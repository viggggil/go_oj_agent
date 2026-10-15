"""将语料参考程序提交到真实判题链路，检查所有题为 AC（仅验证环境显式运行）。"""

import argparse
import json
import time
from pathlib import Path
from uuid import uuid4

from import_algorithm_corpus import Gateway, ROOT


def run(args):
    api = Gateway(args.gateway, args.token_file)
    try:
        problems = api.list_problems()
        pending = {}
        results = []
        for item in json.loads((ROOT / 'manifest.json').read_text()):
            slug = item['slug']
            response = api.request('POST', '/api/v1/submissions', {
                'problem_id': int(problems[slug]['id']), 'language': 'python',
                'source_code': (ROOT / slug / 'solution.py').read_text(),
                'idempotency_key': str(uuid4()),
            }, key='verify:' + str(uuid4()))
            pending[int(response['submission_id'])] = slug
        deadline = time.monotonic() + args.wait_seconds
        while pending:
            if time.monotonic() > deadline:
                raise RuntimeError(f'判题等待超时，尚有 {len(pending)} 题')
            for submission_id, slug in list(pending.items()):
                submission = api.request('GET', f'/api/v1/submissions/{submission_id}')['submission']
                if submission['status'] == 4:
                    verdict = submission['verdict']
                    results.append({'slug': slug, 'submission_id': submission_id, 'verdict': verdict})
                    print(f'{slug}: verdict={verdict}', flush=True)
                    del pending[submission_id]
            if pending:
                time.sleep(2)
        args.report.write_text(json.dumps(results, indent=2) + '\n')
        assert all(item['verdict'] == 1 for item in results), '部分参考程序未通过，详见报告'
        print(f'{len(results)} 道题全部 AC。')
    finally:
        api.client.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway', default='http://127.0.0.1:8080')
    parser.add_argument('--token-file', type=Path, required=True)
    parser.add_argument('--wait-seconds', type=int, default=600)
    parser.add_argument('--report', type=Path, default=Path('/tmp/oj-corpus-submissions.json'))
    run(parser.parse_args())
