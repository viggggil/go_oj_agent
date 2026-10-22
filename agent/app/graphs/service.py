"""运行时与持久化的边界：先接受请求，持久化终态后再发 done。"""

import asyncio
import logging
from collections.abc import AsyncIterator
from dataclasses import dataclass
from datetime import datetime, timedelta
from time import monotonic
from typing import Any, Protocol
from uuid import UUID

from opentelemetry import trace

from app.core.runtime_config import ConfigSnapshot, ConfigSnapshotReader
from app.graphs.context import ContextAssembler
from app.graphs.runtime import AgentRuntime
from app.memory.worker import MemoryTrigger
from app.models.provider import MODEL_ERROR_CODES, ModelFailure
from app.models.runtime import (
    AgentState,
    ChatRequest,
    Principal,
    RunStatus,
    StreamEvent,
)
from app.storage.repository import (
    CompletedTurnRecord,
    ConversationSummaryRecord,
    RunRecord,
    utc_now,
)

logger = logging.getLogger(__name__)


@dataclass
class _ExportTracker:
    submitted: bool = False


_PUBLIC_RUNTIME_ERROR_CODES = frozenset(
    {
        "AGENT_DEADLINE_EXCEEDED",
        "AGENT_RUNTIME_INCOMPLETE",
        "AGENT_EVENT_LIMIT",
        "AGENT_RUNTIME_INVALID_EVENT",
        "AGENT_OUTPUT_LIMIT",
        "AGENT_RUNTIME_FAILED",
    }
)


class RuntimeStore(Protocol):
    async def create_run_with_user_message(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID | None,
        request_id: str,
        content: str,
        config_source: str,
        config_snapshot: dict[str, Any],
        agent_key: str | None = "demo",
        deadline_at: datetime,
    ) -> RunRecord: ...

    async def finish_run(
        self,
        *,
        user_id: int,
        run_id: str | UUID,
        status: RunStatus,
        answer: str | None = None,
        error_code: str | None = None,
        model_summary: dict[str, Any] | None = None,
    ) -> RunRecord: ...

    async def interrupt_running_runs(self) -> int: ...

    async def expire_running_runs(self) -> int: ...

    async def list_completed_turn_records(
        self,
        user_id: int,
        conversation_id: str | UUID,
        *,
        after_message_id: int | None = None,
        limit: int = 20,
    ) -> list[CompletedTurnRecord]: ...

    async def get_conversation_summary(
        self, user_id: int, conversation_id: str | UUID
    ) -> ConversationSummaryRecord | None: ...


class RuntimeFailure(RuntimeError):
    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class AcceptedRun:
    """调用方必须消费事件到结束或显式 aclose，确保取消写入数据库。"""

    def __init__(
        self,
        state: AgentState,
        events: AsyncIterator[StreamEvent],
        service: "RunService",
        trace_id: str | None,
        export_tracker: _ExportTracker,
    ) -> None:
        self.state = state
        self.events = events
        self._service = service
        self._trace_id = trace_id
        self._export_tracker = export_tracker

    async def __aenter__(self) -> "AcceptedRun":
        return self

    async def __aexit__(self, *_args: object) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        close = getattr(self.events, "aclose", None)
        if close is not None:
            await close()
        # 建流前调用方取消时，未启动的 async generator 也需要释放 Run。
        if self.state.status == "RUNNING":
            await self._service._terminate(self.state, "CANCELLED", "AGENT_CANCELLED")
            self._service._export_terminal(
                self.state, self._trace_id, "CANCELLED", "AGENT_CANCELLED", self._export_tracker
            )


class RunService:
    def __init__(
        self,
        store: RuntimeStore,
        runtime: AgentRuntime,
        config_reader: ConfigSnapshotReader,
        *,
        cleanup_timeout_seconds: float = 5,
        context_assembler: ContextAssembler | None = None,
        memory_trigger: MemoryTrigger | None = None,
        langsmith_exporter: Any | None = None,
    ) -> None:
        self._store = store
        self._runtime = runtime
        self._reader = config_reader
        self._cleanup_timeout = cleanup_timeout_seconds
        self._context_assembler = context_assembler or ContextAssembler()
        self._memory_trigger = memory_trigger
        self._langsmith_exporter = langsmith_exporter

    def with_store(self, store: RuntimeStore) -> "RunService":
        """共享 Runtime 和配置解析器，使用独立用途的持久化边界。"""
        return RunService(
            store,
            self._runtime,
            self._reader,
            cleanup_timeout_seconds=self._cleanup_timeout,
            context_assembler=self._context_assembler,
            # Evaluation stores have their own purpose and must not enqueue
            # ordinary chat memory jobs.
            memory_trigger=None,
            # Eval and review runs never enter the production Chat export.
            langsmith_exporter=None,
        )

    @property
    def config_reader(self) -> ConfigSnapshotReader:
        return self._reader

    async def initialize(self) -> int:
        """仅允许单实例，在开始接受运行前中断旧进程遗留的 RUNNING。"""
        return await self._store.interrupt_running_runs()

    async def accept(
        self,
        request: ChatRequest,
        principal: Principal,
        *,
        snapshot: ConfigSnapshot | None = None,
    ) -> AcceptedRun:
        span_context = trace.get_current_span().get_span_context()
        trace_id = (
            span_context.trace_id.to_bytes(16, "big").hex() if span_context.is_valid else None
        )
        snapshot = snapshot or await self._reader.read(request, principal)
        # 上次终态持久化失败的 Run 会在 deadline 之后释放活动关联。
        await self._store.expire_running_runs()
        now = utc_now()
        record = await self._store.create_run_with_user_message(
            user_id=principal.user_id,
            conversation_id=request.conversation_id,
            request_id=principal.request_id,
            content=request.message,
            config_source=snapshot.source,
            config_snapshot=snapshot.model_dump(mode="json"),
            agent_key=snapshot.agent_key,
            deadline_at=now + timedelta(seconds=snapshot.budget.max_run_seconds),
        )
        state = AgentState(
            user_id=principal.user_id,
            principal=principal,
            conversation_id=UUID(record.conversation_id),
            run_id=UUID(record.id),
            user_message=request.message,
            agent_key=snapshot.agent_key,
            skill_key=snapshot.skill_key,
            context=request.context,
            allowed_tools=snapshot.allowed_tools,
            config_snapshot=snapshot.model_dump(mode="json"),
            started_at=record.started_at,
            deadline_at=record.deadline_at,
        )
        try:
            summary = await self._store.get_conversation_summary(
                principal.user_id, record.conversation_id
            )
            turns = await self._store.list_completed_turn_records(
                principal.user_id,
                record.conversation_id,
                after_message_id=summary.covered_until_message_id if summary else None,
                limit=24,
            )
            assembled = self._context_assembler.assemble(summary, turns)
            state.history = assembled.history
            state.memory_summary = assembled.summary
        except asyncio.CancelledError:
            await self._terminate(state, "CANCELLED", "AGENT_CANCELLED")
            raise
        except Exception:
            await self._terminate(state, "FAILED", "AGENT_HISTORY_UNAVAILABLE")
            raise
        tracker = _ExportTracker()
        return AcceptedRun(state, self._events(state, trace_id, tracker), self, trace_id, tracker)

    async def _events(
        self, state: AgentState, trace_id: str | None, tracker: _ExportTracker
    ) -> AsyncIterator[StreamEvent]:
        budget = state.config_snapshot["budget"]
        remaining = max(0.0, (state.deadline_at - utc_now()).total_seconds())
        end = monotonic() + remaining
        iterator: AsyncIterator[StreamEvent] | None = None
        answer = ""
        sequence = 0
        terminal = False
        runtime_state = state.model_copy(deep=True)
        try:
            iterator = self._runtime.run(runtime_state)
            while True:
                remaining = end - monotonic()
                if remaining <= 0:
                    raise RuntimeFailure("AGENT_DEADLINE_EXCEEDED")
                try:
                    event = await asyncio.wait_for(anext(iterator), timeout=remaining)
                except StopAsyncIteration:
                    raise RuntimeFailure("AGENT_RUNTIME_INCOMPLETE") from None
                except TimeoutError:
                    raise RuntimeFailure("AGENT_DEADLINE_EXCEEDED") from None
                sequence += 1
                if sequence > budget["max_events"]:
                    raise RuntimeFailure("AGENT_EVENT_LIMIT")
                if event.run_id != state.run_id or event.conversation_id != state.conversation_id:
                    raise RuntimeFailure("AGENT_RUNTIME_INVALID_EVENT")
                if event.type == "token":
                    text = event.data.get("text")
                    if not isinstance(text, str):
                        raise RuntimeFailure("AGENT_RUNTIME_INVALID_EVENT")
                    answer += text
                    if len(answer) > budget["max_output_chars"]:
                        raise RuntimeFailure("AGENT_OUTPUT_LIMIT")
                elif event.type == "error":
                    # Runtime 不能自行决定对外错误文本，避免将 Provider 异常泄露。
                    raise RuntimeFailure("AGENT_RUNTIME_FAILED")
                elif event.type == "done":
                    if not answer.strip():
                        raise RuntimeFailure("AGENT_RUNTIME_INCOMPLETE")
                    state.model_summary = runtime_state.model_summary
                    await asyncio.wait_for(
                        self._store.finish_run(
                            user_id=state.user_id,
                            run_id=state.run_id,
                            status="COMPLETED",
                            answer=answer,
                            model_summary=(
                                runtime_state.model_summary.model_dump(mode="json")
                                if runtime_state.model_summary is not None
                                else None
                            ),
                        ),
                        timeout=max(0, end - monotonic()),
                    )
                    if self._memory_trigger is not None:
                        try:
                            self._memory_trigger.notify_completed(
                                user_id=state.user_id,
                                conversation_id=str(state.conversation_id),
                            )
                        except Exception as exc:
                            # Memory is best-effort and must never turn a
                            # durably completed chat run into an error.
                            logger.warning(
                                "Conversation memory enqueue failed",
                                extra={"error_type": type(exc).__name__},
                            )
                    terminal = True
                    state.status = "COMPLETED"
                    self._export_terminal(state, trace_id, "COMPLETED", None, tracker)
                    logger.info(
                        "Agent run completed",
                        extra={
                            "run_id": str(state.run_id),
                            "request_id": state.principal.request_id,
                        },
                    )
                    yield StreamEvent.done(state, sequence)
                    return
                yield event.model_copy(update={"sequence": sequence})
        except (asyncio.CancelledError, GeneratorExit):
            state.model_summary = runtime_state.model_summary
            await self._terminate(state, "CANCELLED", "AGENT_CANCELLED")
            terminal = True
            self._export_terminal(state, trace_id, "CANCELLED", "AGENT_CANCELLED", tracker)
            raise
        except Exception as exc:
            state.model_summary = runtime_state.model_summary
            # 注入的 Runtime 即使误用内部异常类，也不能输出任意错误文本。
            logger.warning(
                "Agent runtime failed",
                extra={"error_type": type(exc).__name__},
            )
            code = (
                exc.code
                if isinstance(exc, RuntimeFailure) and exc.code in _PUBLIC_RUNTIME_ERROR_CODES
                else "AGENT_RUN_FAILED"
            )
            if isinstance(exc, ModelFailure) and exc.code in MODEL_ERROR_CODES:
                code = exc.code
            await self._terminate(state, "FAILED", code)
            terminal = True
            self._export_terminal(state, trace_id, "FAILED", code, tracker)
            yield StreamEvent.error(state, code, sequence + 1)
        finally:
            if not terminal:
                await self._terminate(state, "CANCELLED", "AGENT_CANCELLED")
            # Async generator 的取消/关闭必须传到 Runtime，不能遗留生成任务。
            close = getattr(iterator, "aclose", None)
            if close is not None:
                try:
                    await asyncio.wait_for(close(), timeout=self._cleanup_timeout)
                except Exception:
                    logger.warning(
                        "Runtime close failed", extra={"error_code": "AGENT_RUNTIME_CLOSE_FAILED"}
                    )

    async def _terminate(self, state: AgentState, status: RunStatus, code: str) -> None:
        if state.status != "RUNNING":
            return
        if state.model_summary is not None:
            for attempt in state.model_summary.attempts:
                if attempt.finish_reason is None:
                    attempt.finish_reason = "cancelled" if status == "CANCELLED" else "failed"
                    attempt.error_code = code
        task = asyncio.create_task(
            self._store.finish_run(
                user_id=state.user_id,
                run_id=state.run_id,
                status=status,
                error_code=code,
                model_summary=(
                    state.model_summary.model_dump(mode="json")
                    if state.model_summary is not None
                    else None
                ),
            )
        )
        try:
            await asyncio.wait_for(asyncio.shield(task), timeout=self._cleanup_timeout)
            state.status = status
        except (Exception, asyncio.CancelledError):
            task.cancel()
            # 保存失败保持 RUNNING，下一次请求按 deadline 清理；只记录稳定错误码。
            await asyncio.gather(task, return_exceptions=True)
            logger.error(
                "Run finalization failed", extra={"error_code": "AGENT_FINALIZATION_FAILED"}
            )

    def _export_terminal(
        self,
        state: AgentState,
        trace_id: str | None,
        status: str,
        error_code: str | None,
        tracker: _ExportTracker,
    ) -> None:
        if tracker.submitted or self._langsmith_exporter is None or state.status == "RUNNING":
            return
        tracker.submitted = True
        try:
            self._langsmith_exporter.submit(
                run_id=state.run_id,
                started_at=state.started_at,
                status=status,
                error_code=error_code,
                model_summary=state.model_summary,
                trace_id=trace_id,
                config_version=state.config_snapshot.get("model_profile_id"),
            )
        except Exception as exc:
            # Export bugs must never change the already committed Run outcome.
            logger.warning(
                "LangSmith export enqueue failed",
                extra={
                    "error_code": "LANGSMITH_ENQUEUE_FAILED",
                    "error_type": type(exc).__name__,
                },
            )
