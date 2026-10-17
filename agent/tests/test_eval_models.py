import pytest

from app.evals.models import parse_jsonl
from app.evals.reviewer import parse_reviewer_output, reviewer_input


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


def test_model_review_requires_single_strict_json_and_matching_case() -> None:
    value = {
        "case_id": "case_1",
        "pass": True,
        "scores": {
            "task_completion": 3,
            "technical_correctness": 3,
            "evidence_support": 3,
            "instruction_following": 3,
            "clarity": 3,
        },
        "hard_fail": False,
        "violations": [],
        "missing_facts": [],
        "reason": "回答完成任务。",
    }
    import json

    assert parse_reviewer_output(json.dumps(value, ensure_ascii=False), "case_1")["pass"]
    with pytest.raises(ValueError):
        parse_reviewer_output(json.dumps({**value, "extra": 1}), "case_1")
    with pytest.raises(ValueError):
        parse_reviewer_output(json.dumps(value) + json.dumps(value), "case_1")
    with pytest.raises(ValueError):
        parse_reviewer_output(json.dumps({**value, "case_id": "other"}), "case_1")


def test_reviewer_input_preserves_positive_and_forbidden_check_semantics() -> None:
    case = parse_jsonl(
        '{"id":"case_1","category":"basic","title":"题目",'
        '"turns":[{"role":"user","content":"解释二分"}]}'
    )[0]
    text = reviewer_input(
        case,
        "回答",
        {
            "pass": True,
            "hard_fail": False,
            "required_tools": {"get_submission": True},
            "forbidden_claims": {"看到了隐藏测试点": False},
            "must_not_leak": {"source_id": False},
        },
        None,
    )
    assert '"failed_requirements":[]' in text
    assert '"violations":[]' in text
