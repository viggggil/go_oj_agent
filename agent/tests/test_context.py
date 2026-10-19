"""会话摘要和原始轮次装配的边界测试。"""

from datetime import datetime

from app.graphs.context import ContextAssembler
from app.models.memory import ConfirmedFact, ConversationSummary
from app.storage.repository import CompletedTurnRecord, ConversationSummaryRecord


def turn(index: int) -> CompletedTurnRecord:
    return CompletedTurnRecord(
        run_id=f"run-{index}",
        user_message_id=index * 2 - 1,
        assistant_message_id=index * 2,
        user_content=f"问题 {index}",
        assistant_content=f"回答 {index}",
    )


def test_summary_payload_excludes_internal_message_ids() -> None:
    summary = ConversationSummary(
        goal="定位错误",
        confirmed_facts=(
            ConfirmedFact(
                content="用户使用 C++17",
                source_message_id=12,
                source_type="user_statement",
            ),
        ),
    )

    payload = summary.prompt_payload()
    assert payload["confirmed_facts"] == [
        {"content": "用户使用 C++17", "source_type": "user_statement"}
    ]
    assert "source_message_id" not in str(payload)


def test_context_assembler_keeps_only_uncovered_recent_turns() -> None:
    summary = ConversationSummaryRecord(
        conversation_id="conversation",
        user_id=7,
        purpose="chat",
        summary=ConversationSummary(goal="持续排查"),
        covered_until_message_id=8,
        schema_version=1,
        version=2,
        created_at=datetime(2026, 1, 1),
        updated_at=datetime(2026, 1, 1),
    )

    assembled = ContextAssembler(recent_turns=2).assemble(summary, [turn(i) for i in range(1, 7)])

    assert [item.content for item in assembled.history] == [
        "问题 5",
        "回答 5",
        "问题 6",
        "回答 6",
    ]
    assert assembled.summary is summary.summary
