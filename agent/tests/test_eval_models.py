import pytest

from app.evals.models import parse_jsonl


def test_parse_eval_jsonl_rejects_duplicate_ids_and_system_turns() -> None:
    valid = (
        '{"id":"case_1","category":"basic","title":"题目",'
        '"turns":[{"role":"user","content":"解释二分"}]}'
    )
    assert parse_jsonl(valid)[0].id == "case_1"
    with pytest.raises(ValueError, match="duplicate"):
        parse_jsonl(f"{valid}\n{valid}")
    with pytest.raises(ValueError, match="invalid"):
        parse_jsonl(
            '{"id":"case_2","category":"basic","title":"题目","turns":[{"role":"system","content":"x"}]}'
        )
