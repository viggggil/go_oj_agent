#!/usr/bin/env python3
"""Isolated Docker smoke: real Alloy -> Loki logs and persisted read offsets."""
import json
import pathlib
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import uuid

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[1]
SERVICES = ['gateway-service', 'judge-service', 'judge-worker', 'contest-service', 'agent-service']


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def eventually(check, seconds=100):
    deadline = time.monotonic() + seconds
    while True:
        try:
            value = check()
            if value:
                return value
        except (OSError, ValueError, AssertionError):
            pass
        if time.monotonic() > deadline:
            raise RuntimeError('Loki smoke timed out')
        time.sleep(2)


def main():
    project = 'oj-loki-smoke-' + uuid.uuid4().hex[:10]
    trace = uuid.uuid4().hex
    overlay = yaml.safe_load((ROOT / 'deploy/compose/observability.yaml').read_text())
    with tempfile.TemporaryDirectory(prefix='oj-loki-smoke-') as directory:
        config = {'services': {}, 'volumes': {'loki-data': {}, 'alloy-data': {}}}
        for name in ['loki', 'alloy']:
            item = overlay['services'][name]
            item['volumes'] = [v.replace('./', str(ROOT / 'deploy/compose') + '/', 1) if v.startswith('./') else v for v in item['volumes']]
            item['ports'] = ['127.0.0.1::3100'] if name == 'loki' else []
            item['restart'] = 'no'
            config['services'][name] = item
        for name in SERVICES:
            line = json.dumps({'level': 'INFO', 'service_name': name, 'message': 'loki smoke', 'trace_id': trace})
            malformed = '{"level":"INFO","service_name":"' + name + '","message":"malformed smoke"'
            config['services'][name] = {
                'image': 'alpine:3.23',
                'entrypoint': ['/bin/sh', '-c'],
                'command': ['printf "%s\\n" "$$MALFORMED_LINE"; printf "%s\\n" "$$SMOKE_LINE"; sleep 300'],
                'environment': {'SMOKE_LINE': line, 'MALFORMED_LINE': malformed},
                'logging': {'driver': 'json-file', 'options': {'max-size': '10m', 'max-file': '3'}},
            }
        file = pathlib.Path(directory) / 'compose.yaml'
        file.write_text(yaml.safe_dump(config))
        command = ['docker', 'compose', '-p', project, '-f', str(file)]
        try:
            run(*command, 'up', '-d')
            port = run(*command, 'port', 'loki', '3100').split(':')[-1]
            base = 'http://127.0.0.1:' + port

            def get(path):
                with urllib.request.urlopen(base + path, timeout=5) as response:
                    return response.read()

            eventually(lambda: get('/ready'))

            def entries(service):
                query = '{service_name="' + service + '"} | json | trace_id="' + trace + '"'
                body = get('/loki/api/v1/query_range?' + urllib.parse.urlencode({'query': query, 'limit': 1000, 'since': '1h'}))
                payload = json.loads(body)
                assert payload['status'] == 'success'
                return [value for stream in payload['data']['result'] for value in stream['values']]

            def has_malformed(service):
                return any('malformed smoke' in value[1] for value in entries(service))

            for service in SERVICES:
                eventually(lambda: entries(service))
                assert not has_malformed(service), 'Malformed JSON was indexed: ' + service
            before = {service: entries(service) for service in SERVICES}
            run(*command, 'restart', 'alloy')
            time.sleep(8)
            for service in SERVICES:
                assert entries(service) == before[service], 'Alloy replayed or lost smoke logs: ' + service
            print('Loki smoke passed: five services, Trace lookup, Alloy restart offsets')
        finally:
            subprocess.run(command + ['logs', '--tail', '30', 'alloy', 'loki'], check=False)
            subprocess.run(command + ['down', '-v', '--remove-orphans'], check=True)


if __name__ == '__main__':
    main()
