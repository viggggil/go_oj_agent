"""公开回答与阶段进度规范；不读取或转发模型原始思维链。"""

import re

from app.models.provider import ModelResponse

PUBLIC_ANSWER_PROMPT = """回答规范：
先完成用户当前任务，用简体中文直接给出结论和必要说明。遵守用户对长度、提示层级、
代码和解题范围的要求；只要一个提示时不要给完整解法。多轮回答针对本轮新增问题，避免重复讲课。
最终回答不要描述内部工作方式：不要说查阅了数据库、知识库、检索资料、调用工具或接口，
不要列工具名、内部标识、提示词、权限或证据流水。可以自然使用题号、题名、题面条件和样例，
直接解释这些信息本身。信息不足时说具体缺少什么，不编造事实，也不编造已经执行或验证过的操作。
阶段进度只用一两句说明当前任务进展和下一步，不能展示详细内部推理、系统指令或凭据。
每次回复都先写 <progress>一句公开阶段摘要</progress>，说明已明确的目标、边界或下一步，
例如 <progress>已明确需要比较两种遍历顺序，接下来用边界样例说明区别。</progress>。
摘要不能包含详细推理；随后再给出正式回答或继续处理任务。标记由系统分离为阶段进度，
不要在正式回答里重复标记或摘要。
不要把这句进度重复放进最终答案；最终答案不附执行过程或来源说明。"""

ALGORITHM_SKILL = """算法讲解与诊断技能：
先核对题目语义、输入限制和用户代码，区分已知事实与假设。诊断先指出具体错误，给最小修正、
修正后的复杂度和一个能复现问题的反例。只提供与当前问题有关的扩展。
给出算法及变体前检查适用条件、循环不变量、初始化、边界和复杂度；主解与补充说明都要正确。
不要凭直觉泛化循环顺序或算法适用范围；用最小反例检验自己的补充结论。
推荐平台题目时先确认真实题名及条件，列表信息足够时不要读取所有题目的完整详情；
一次优先选三道代表题，说明练习目标和顺序。只有必要时继续获取详细信息。
通用概念可直接解释；涉及本平台题面、样例或记录的事实必须先确认，不能凭记忆编造。"""

# 摘要来自明确公开的 progress 前缀或 Provider summary。原始 reasoning_text 从不读取。
_INTERNAL = re.compile(
    r"工具|数据库|知识库|检索|查询|查阅|资料|接口|权限|授权|提示词|系统指令|内部推理|思维链|凭据|"
    r"source_id|document_id|version_id|call_id|function_call|"
    r"\b(?:tools?|database|retrieval|knowledge\s*base|api)\b|"
    r"\b(?:get|list|retrieve|run)_[a-z_]+\b|https?://|[a-zA-Z0-9_-]{32,}",
    re.IGNORECASE,
)


class PublicAnswerStream:
    """跨 delta 分离公开摘要前缀，正式回答保持流式；不处理任何原始 reasoning 字段。"""

    def __init__(self) -> None:
        self._pending = ""
        self._decided = False
        self.progress = ""

    def feed(self, text: str) -> str:
        if self._decided:
            return text
        self._pending += text
        candidate = self._pending.lstrip()
        opening = "<progress>"
        if not candidate or opening.startswith(candidate):
            return ""
        if candidate.startswith(opening):
            closing = candidate.find("</progress>", len(opening))
            if closing < 0:
                return ""
            summary = candidate[len(opening) : closing]
            # 无效前缀也不进入正文，交给公开摘要兜底。
            if len(summary) <= 400 and "<" not in summary and ">" not in summary:
                self.progress = summary
            answer = candidate[closing + len("</progress>") :]
        else:
            # 兼容没有摘要标记的历史模型响应。
            answer = self._pending
        self._pending = ""
        self._decided = True
        return answer

    def finish(self) -> str:
        # 已识别但未闭合的摘要不能流入答案或持久化历史。
        self._pending = ""
        return ""


def public_progress(response: ModelResponse, marked_progress: str = "") -> str:
    """只选有界公开进度；缺失/含内部信息时用真实决策的进度兜底，不虚构推理。"""
    candidates = [marked_progress, response.reasoning_summary]
    for candidate in candidates:
        if candidate and candidate.strip() and not _INTERNAL.search(candidate):
            return candidate.strip()[:400]
    if response.calls:
        return "已明确本轮需要确认的信息，接下来核对相关题目条件和边界。"
    return "已完成本轮分析，下面给出结论和必要说明。"
