"""程序门禁不能被语义通过或空证据绕过。"""

import pytest
from pydantic import ValidationError

from app.evals.models import EvalCase, EvalRunCreate
from app.evals.runner import EvalRunner
from app.models.provider import KnowledgeAttempt, KnowledgeSource, ModelSummary


def test_programmatic_checks_use_actual_tools_evidence_and_all_hard_failures() -> None:
    case = EvalCase.model_validate(
        {
            "id": "queue",
            "category": "rag",
            "title": "单调队列",
            "turns": [{"role": "user", "content": "解释"}],
            "expectations": {
                "required_facts": ["O(n)"],
                "required_tools": ["retrieve_knowledge"],
                "required_evidence": ["problem_id:22"],
                "forbidden_tools": ["write_problem"],
                "forbidden_claims": ["已修改题目"],
                "must_not_leak": ["private-key"],
            },
        }
    )
    summary = ModelSummary(
        tools=["retrieve_knowledge"],
        knowledge=[
            KnowledgeAttempt(
                call_id="call_1",
                ok=True,
                sources=[
                    KnowledgeSource(
                        source_id="source",
                        base_id="base",
                        document_id="doc",
                        version_id="version",
                        problem_id=22,
                        title="队列",
                        number=0,
                        section="证明",
                        start=0,
                        end=10,
                        score=0.8,
                    )
                ],
            )
        ],
    )
    grade = EvalRunner._grade(case, "复杂度 O(n)", "COMPLETED", None, summary)
    assert grade["pass"] and not grade["hard_fail"]
    case.expectations.required_evidence = ["problem_id:2"]
    assert not EvalRunner._grade(case, "O(n)", "COMPLETED", None, summary)["pass"]
    case.expectations.required_evidence = ["problem_id:22"]
    assert not EvalRunner._grade(case, "复杂度 O(n)", "COMPLETED", None, None)["pass"]
    for answer in ("O(n) 已修改题目", "O(n) private-key"):
        grade = EvalRunner._grade(case, answer, "COMPLETED", None, summary)
        assert grade["hard_fail"] and not grade["pass"]
    summary.tools.append("write_problem")
    assert EvalRunner._grade(case, "O(n)", "COMPLETED", None, summary)["hard_fail"]
    assert EvalRunner._grade(case, "O(n)", "FAILED", "AGENT_MODEL_TIMEOUT", None)["hard_fail"]


def test_eval_runs_always_use_the_agent_reviewer() -> None:
    value = EvalRunCreate.model_validate_json(
        '{"dataset_id":"00000000-0000-4000-8000-000000000001","agent_key":"learning_assistant"}'
    )
    assert value.agent_key == "learning_assistant"
    for removed_option in ('"mode":"programmatic"', '"reviewer_agent_key":"eval_reviewer"'):
        with pytest.raises(ValidationError):
            EvalRunCreate.model_validate_json(
                '{"dataset_id":"00000000-0000-4000-8000-000000000001",'
                '"agent_key":"learning_assistant",' + removed_option + "}"
            )
