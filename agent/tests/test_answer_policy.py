"""公开阶段进度必须有用、简短且不泄漏内部执行细节。"""

from app.answer_policy import (
    ALGORITHM_SKILL,
    PUBLIC_ANSWER_PROMPT,
    PublicAnswerStream,
    public_progress,
)
from app.models.provider import FunctionCall, ModelResponse


def test_final_answer_prompt_hides_internal_tools_and_respects_requested_scope() -> None:
    assert "不要说查阅了数据库" in PUBLIC_ANSWER_PROMPT
    assert "只要一个提示时不要给完整解法" in PUBLIC_ANSWER_PROMPT
    assert "用最小反例检验自己的补充结论" in ALGORITHM_SKILL


def test_progress_uses_only_safe_summary_or_an_explicit_public_marker() -> None:
    assert (
        public_progress(ModelResponse(reasoning_summary="先界定问题条件。")) == "先界定问题条件。"
    )
    assert public_progress(ModelResponse(reasoning_summary="我会检索知识库和数据库。")) == (
        "已完成本轮分析，下面给出结论和必要说明。"
    )
    tool_response = ModelResponse(
        calls=(FunctionCall(call_id="call_1", name="get_problem", arguments="{}"),)
    )
    assert public_progress(tool_response, "先梳理题目目标和边界。") == ("先梳理题目目标和边界。")
    assert public_progress(tool_response, "调用 get_problem 检查。") == (
        "已明确本轮需要确认的信息，接下来核对相关题目条件和边界。"
    )


def test_public_progress_prefix_is_separated_across_arbitrary_delta_boundaries() -> None:
    stream = PublicAnswerStream()
    wire = " \n<progress>已明确题目边界，下面解释递推。</progress>\n答案与代码。"
    answer = "".join(stream.feed(char) for char in wire) + stream.finish()
    assert stream.progress == "已明确题目边界，下面解释递推。"
    assert answer == "\n答案与代码。"
    assert public_progress(ModelResponse(), stream.progress) == stream.progress


def test_legacy_answers_stream_and_incomplete_progress_does_not_leak() -> None:
    stream = PublicAnswerStream()
    assert stream.feed("答案") == "答案"
    assert stream.feed("正文") == "正文"
    incomplete = PublicAnswerStream()
    assert incomplete.feed("<progress>未完成的摘要") == ""
    assert incomplete.finish() == ""


def test_internal_progress_is_replaced_but_legitimate_answer_is_unchanged() -> None:
    stream = PublicAnswerStream()
    assert stream.feed("<progress>I used database tools.</progress>答案") == "答案"
    assert public_progress(ModelResponse(), stream.progress) == (
        "已完成本轮分析，下面给出结论和必要说明。"
    )
