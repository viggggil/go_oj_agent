"""从项目公开语料提取题面样例和数据范围，不读取隐藏判题对象。"""

import json
from pathlib import Path


def statement_details(folder: Path) -> dict:
    statement = (folder / 'statement.md').read_text(encoding='utf-8')
    marker = '## 数据范围\n'
    if marker not in statement:
        raise ValueError(f'{folder.name}: 缺少数据范围')
    constraints = statement.split(marker, 1)[1].split('\n## ', 1)[0].strip()
    # tests.json 的前三组是项目编写、已验证的公开题面样例；之后才是生成回归数据。
    samples = json.loads((folder / 'tests.json').read_text(encoding='utf-8'))[:3]
    if not constraints or not 1 <= len(samples) <= 3:
        raise ValueError(f'{folder.name}: 题面信息不完整')
    return {'constraints': constraints, 'samples': samples}
