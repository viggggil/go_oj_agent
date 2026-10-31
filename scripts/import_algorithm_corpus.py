"""经 Gateway 导入经典题和管理员知识库；不读写任何业务库。

用法：agent/.venv/bin/python scripts/import_algorithm_corpus.py --token-file /secure/admin-token
令牌从权限受控文件读取，不出现在进程参数或输出。已存在题目按 slug 复用，不覆盖正文和测试点。
"""

import argparse
import base64
import hashlib
import json
import time
from pathlib import Path
from uuid import NAMESPACE_URL, uuid4, uuid5

import httpx

from problem_statement import statement_details

ROOT = Path(__file__).resolve().parents[1] / 'data/algorithm-corpus'
PREFIX = '/api/v1/admin/agent/rag'


class Gateway:
    def __init__(self, url, token_file):
        self.client = httpx.Client(base_url=url, timeout=35, trust_env=False,
                                   headers={'Authorization': 'Bearer ' + token_file.read_text().strip()})

    def request(self, method, path, body=None, *, key=None):
        headers = ({'X-Request-ID': str(uuid5(NAMESPACE_URL, 'oj-corpus-v1:' + key))} if key
                   else {'X-Request-ID': str(uuid4())} if method != 'GET' else {})
        response = self.client.request(method, path, json=body, headers=headers)
        if response.status_code != 200:
            # 不输出 JWT 或原始服务错误正文。
            raise RuntimeError(f'{method} {path}: HTTP {response.status_code}')
        return response.json()

    def list_problems(self):
        items = []
        page = 1
        while True:
            batch = self.request('GET', f'/api/v1/problems?page.page={page}&page.page_size=100')
            items.extend(batch['items'])
            if len(items) >= int(batch['page']['total']):
                return {item['slug']: item for item in items}
            page += 1


def run(args):
    api = Gateway(args.gateway, args.token_file)
    try:
        me = api.request('GET', '/api/v1/users/me')['user']
        if not set(me['roles']) & {'admin', 'system_admin'}:
            raise RuntimeError('题目导入要求 admin/system_admin（同时具有题目和知识库管理权限）')
        corpus = json.loads((ROOT / 'manifest.json').read_text())
        problems = api.list_problems()
        base = api.request('POST', PREFIX + '/knowledge-bases', {'name': '经典算法与标准题解'},
                           key='base')['knowledge_base']['id']
        mapping = []
        for item in corpus:
            slug = item['slug']; folder = ROOT / slug
            if slug not in problems:
                tests = []
                for i, case in enumerate(json.loads((folder / 'tests.json').read_text()), 1):
                    tests.append({'case_no': i, 'input_filename': f'{i}.in',
                                  'input_content': base64.b64encode(case['input'].encode()).decode(),
                                  'output_filename': f'{i}.out',
                                  'output_content': base64.b64encode(case['output'].encode()).decode()})
                payload = {'problem': {
                    'title': item['title'], 'slug': slug,
                    'description': (folder / 'statement.md').read_text(),
                    'difficulty': 2, **statement_details(folder),
                    'time_limit_ms': 5000, 'memory_limit_kb': 262144, 'tags': item['tags'],
                }, 'testcases': tests}
                try:
                    problem = api.request('POST', '/api/v1/problems', payload, key='problem:' + slug)['problem']
                except (httpx.TransportError, RuntimeError):
                    # 创建超时/并发 slug 冲突后先查业务真源，不盲目重放非幂等写。
                    reconciled = api.list_problems()
                    if slug not in reconciled:
                        raise
                    problem = reconciled[slug]
                problems[slug] = problem
            problem_id = int(problems[slug]['id'])
            existing_cases = api.request('GET', f'/api/v1/problems/{problem_id}/testcases')['items']
            fingerprints = {(c['input_sha256'], c['output_sha256']) for c in existing_cases
                            if c.get('status') == 1}
            next_number = max((int(c['case_no']) for c in existing_cases), default=0) + 1
            for case in json.loads((folder / 'tests.json').read_text()):
                pair = (hashlib.sha256(case['input'].encode()).hexdigest(),
                        hashlib.sha256(case['output'].encode()).hexdigest())
                if pair in fingerprints:
                    continue
                testcase = {'case_no': next_number, 'input_filename': f'{next_number}.in',
                            'output_filename': f'{next_number}.out',
                            'input_content': base64.b64encode(case['input'].encode()).decode(),
                            'output_content': base64.b64encode(case['output'].encode()).decode()}
                # 测试点写超时不盲重放；下次导入重新读 hash 和 case_no 后收敛。
                api.request('POST', f'/api/v1/problems/{problem_id}/testcases', testcase,
                            key=f'testcase:{slug}:{pair[0]}:{pair[1]}')
                fingerprints.add(pair)
                next_number += 1
            content = (folder / 'editorial.md').read_text()
            doc = api.request('POST', PREFIX + '/documents', {
                'base_id': base, 'title': item['title'] + '：标准题解', 'kind': 'editorial',
                'problem_id': problem_id, 'source_url': item['source'], 'tags': item['tags'],
                'filename': slug + '.md', 'content': content,
            }, key='editorial:' + slug + ':' + hashlib.sha256(content.encode()).hexdigest())['document']
            mapping.append({'slug': slug, 'problem_id': problem_id,
                            'document_id': doc['document_id'], 'version_id': doc['id']})
            print(f'{slug}: 题号 {problem_id}，题解已登记', flush=True)
        deadline = time.monotonic() + args.wait_seconds
        pending = mapping.copy()
        while pending:
            if time.monotonic() > deadline:
                raise RuntimeError(f'索引等待超时，尚有 {len(pending)} 篇；可安全重跑继续')
            for item in pending.copy():
                document_id = item['document_id']
                doc = api.request('GET', PREFIX + '/documents/' + document_id)['document']
                if doc['state'] == 'FAILED':
                    raise RuntimeError(f"索引失败：{item['slug']}（管理员可重试）")
                if doc['state'] != 'READY':
                    continue
                api.request('POST', PREFIX + '/documents/' + document_id + '/enable',
                            {'expected_version': doc['id']}, key='enable:' + doc['id'])
                pending.remove(item)
            if pending:
                time.sleep(2)
        all_problems = api.list_problems()
        output = {'base_id': base, 'total_problems': len(all_problems), 'documents': mapping}
        args.report.write_text(json.dumps(output, ensure_ascii=False, indent=2) + '\n')
        print(f'完成：题库 {len(all_problems)} 题，本语料 {len(mapping)} 篇题解已索引并启用。报告：{args.report}')
    finally:
        api.client.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway', default='http://127.0.0.1:8080')
    parser.add_argument('--token-file', type=Path, required=True)
    parser.add_argument('--wait-seconds', type=int, default=600)
    parser.add_argument('--report', type=Path, default=Path('/tmp/oj-corpus-import.json'))
    run(parser.parse_args())
