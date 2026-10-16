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


def test_parse_eval_jsonl_rejects_casefold_collisions() -> None:
    first = (
        '{"id":"Case_A","category":"basic","title":"题目",'
        '"turns":[{"role":"user","content":"解释二分"}]}'
    )
    second = first.replace("Case_A", "case_a")
    with pytest.raises(ValueError, match="conflicts ignoring case"):
        parse_jsonl(f"{first}\n{second}")


def test_parse_eval_jsonl_reports_invalid_line() -> None:
    valid = (
        '{"id":"case_1","category":"basic","title":"题目",'
        '"turns":[{"role":"user","content":"解释二分"}]}'
    )
    with pytest.raises(ValueError, match="line 2"):
        parse_jsonl(f"{valid}\n{{}}")


def test_parse_eval_jsonl_rejects_non_finite_json_numbers() -> None:
    with pytest.raises(ValueError, match="invalid"):
        parse_jsonl(
            '{"id":"case_1","category":"basic","title":"题目",'
            '"fixture":{"problem_id":NaN},'
            '"turns":[{"role":"user","content":"解释二分"}]}'
        )
