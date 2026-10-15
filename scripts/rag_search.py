"""管理员 CLI 检索测试；普通用户无权使用，查询仅发送到 Gateway。"""

import argparse
import json
from pathlib import Path

from import_algorithm_corpus import Gateway, PREFIX

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway', default='http://127.0.0.1:8080')
    parser.add_argument('--token-file', type=Path, required=True)
    parser.add_argument('--base-id', action='append', required=True)
    parser.add_argument('--query', required=True)
    parser.add_argument('--problem-id', type=int)
    parser.add_argument('--top-k', type=int, default=5)
    args = parser.parse_args()
    api = Gateway(args.gateway, args.token_file)
    try:
        print(json.dumps(api.request('POST', PREFIX + '/search', {
            'base_ids': args.base_id, 'query': args.query,
            'problem_id': args.problem_id, 'top_k': args.top_k,
        }), ensure_ascii=False, indent=2))
    finally:
        api.client.close()
