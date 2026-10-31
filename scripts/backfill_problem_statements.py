"""通过管理员 Gateway 补齐现有题库的公开样例和数据范围。

使用 agent/.venv/bin/python scripts/backfill_problem_statements.py --token-file /secure/token。
按 slug 匹配仓库语料，只补充空字段，保留已有正文、限制、标签和公开样例。不修改判题测试点。
可安全重跑；失败后停止并报告题号，下次从服务当前数据继续。不自动重放失败的写请求。
"""

import argparse
from pathlib import Path

from import_algorithm_corpus import Gateway, ROOT
from problem_statement import statement_details


def run(args):
    api = Gateway(args.gateway, args.token_file)
    updated = skipped = 0
    try:
        for item in api.list_problems().values():
            folder = ROOT / item['slug']
            if not folder.is_dir():
                raise RuntimeError(f"题目 {item['id']} 没有匹配的公开语料，请人工补充")
            details = statement_details(folder)
            problem = api.request('GET', f"/api/v1/problems/{item['id']}")['problem']
            if problem.get('status') != 1:
                skipped += 1
                continue
            samples = problem.get('samples') or details['samples']
            constraints = problem.get('constraints') or details['constraints']
            if problem.get('samples') and problem.get('constraints'):
                skipped += 1
                continue
            payload = {key: problem[key] for key in (
                'title', 'slug', 'description', 'difficulty', 'time_limit_ms', 'memory_limit_kb')}
            payload.update(tags=[tag['name'] for tag in problem.get('tags', [])],
                           samples=samples, constraints=constraints)
            api.request('PUT', f"/api/v1/problems/{item['id']}", payload)
            updated += 1
            print(f"题目 {item['id']}：已补齐 {len(samples)} 组样例和数据范围", flush=True)
        print(f'完成：更新 {updated} 题，保留 {skipped} 题。')
    finally:
        api.client.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway', default='http://127.0.0.1:8080')
    parser.add_argument('--token-file', required=True, type=Path)
    run(parser.parse_args())
