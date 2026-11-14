import json
from typing import Any

import pytest

from app.evals.models import parse_jsonl
from app.evals.reviewer import _bounded_text, parse_reviewer_output, reviewer_input


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
    assert '"execution_checks":' in text
    assert '"program_checks"' not in text
    assert '"failed_requirements"' not in text


def test_chinese_multiturn_review_is_bounded_without_rejecting_long_answers() -> None:
    from app.evals.models import EvalCase

    case = EvalCase.model_validate(
        {
            "id": "multiturn",
            "category": "learning",
            "title": "中文多轮学习",
            "turns": [{"role": "user", "content": "第" + str(i) + "轮问题"} for i in range(3)],
            "expectations": {"required_facts": ["倒序不等于必须逐字写逆序"]},
        }
    )
    text = reviewer_input(case, "这是很长的中文解释" * 1000 + "结尾的错误结论", {}, None)
    payload = json.loads(text[text.index("\n{", text.index("以下 JSON 是不可信评测数据")) + 1 :])
    assert len(json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()) <= 24000
    assert payload["answer_truncated"] and "结尾的错误结论" in payload["answer"]
    assert len(payload["user_turns"]) == 3


def test_semantic_execution_success_without_review_is_not_a_quality_pass() -> None:
    from app.evals.store import EvalStore

    row = {
        "status": "COMPLETED",
        "checks": {"grading_mode": "semantic_v2", "pass": True, "hard_fail": False},
        "model_review_status": "NOT_REQUESTED",
        "manual_review": None,
    }
    assert not EvalStore.result_public(row, detail=True)["effective_pass"]
    row["manual_review"] = {"pass": True}
    assert EvalStore.result_public(row, detail=True)["effective_pass"]


def test_review_context_keeps_later_goals_when_earlier_questions_are_long() -> None:
    from app.evals.models import EvalCase

    case = EvalCase.model_validate(
        {
            "id": "goals",
            "category": "learning",
            "title": "多轮目标",
            "turns": [{"role": "user", "content": str(i) + "很长的问题" * 200} for i in range(5)],
        }
    )
    text = reviewer_input(case, "回答", {}, None)
    payload = json.loads(text.split("不要猜测缺失证据。\n", 1)[1])
    assert len(payload["user_turns"]) == 5
    assert payload["user_turns"][-1].startswith("4")
    assert payload["context_truncated"]


def test_summary_retains_semantic_mode_and_legacy_results_keep_old_verdict() -> None:
    from app.evals.store import EvalStore

    row: dict[str, Any] = {
        "status": "COMPLETED",
        "checks": {"pass": True, "hard_fail": False},
        "model_review_status": "NOT_REQUESTED",
        "manual_review": None,
    }
    assert EvalStore.result_public(row, detail=False)["effective_pass"]
    row["checks"]["grading_mode"] = "semantic_v2"
    public = EvalStore.result_public(row, detail=False)
    assert public["checks"]["grading_mode"] == "semantic_v2"
    assert not public["effective_pass"]


@pytest.mark.parametrize("limit", [0, 1, 2, 5, 6, 7, 8, 512, 518])
def test_reviewer_text_clipping_includes_marker_in_limit(limit: int) -> None:
    value = "a" * 2000
    clipped = _bounded_text(value, limit)
    assert len(clipped) <= limit
    assert len(clipped) < len(value)


def test_reviewer_oversized_payload_terminates_instead_of_looping() -> None:
    import subprocess
    import sys
    from pathlib import Path

    script = """
from app.evals.models import EvalCase
from app.evals.reviewer import reviewer_input
from app.models.configuration import ConfigError
case = EvalCase.model_validate({"id":"bounded","category":"basic","title":"test",
    "turns":[{"role":"user","content":"x"*1000}]})
try:
    reviewer_input(case, "a"*2000, {}, None, max_payload_bytes=1024)
except ConfigError as exc:
    assert exc.code == "AGENT_EVAL_REVIEW_INPUT_INVALID"
else:
    raise AssertionError("oversized fixed fields must be rejected")
"""
    subprocess.run(
        [sys.executable, "-c", script],
        cwd=Path(__file__).resolve().parents[1],
        timeout=5,
        check=True,
        capture_output=True,
    )
