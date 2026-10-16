"""受控批量 Eval Runner；每个 Case 复用普通 Agent Runtime 的授权和预算。"""

import asyncio
import hashlib
import json
import logging
from time import monotonic
from typing import Any
from uuid import uuid4

from app.core.runtime_config import ConfigSnapshot, ConfigSnapshotReader, DatabaseConfigReader
from app.evals.models import EvalCase, EvalRunCreate
from app.evals.store import EvalStore
from app.graphs.service import RunService
from app.models.configuration import ConfigError
from app.models.provider import ModelSummary
from app.models.runtime import ChatContext, ChatRequest, Principal

logger = logging.getLogger(__name__)


class EvalRunner:
    def __init__(
        self,
        store: EvalStore,
        service: RunService,
        reader: ConfigSnapshotReader,
        principal_aliases: dict[str, int] | None = None,
        max_pending_runs: int = 20,
        max_concurrent_cases: int = 8,
    ) -> None:
        self.store = store
        self.service = service
        self.reader = reader
        self.principal_aliases = principal_aliases or {}
        self.max_pending_runs = max_pending_runs
        self._create_lock = asyncio.Lock()
        self._case_slots = asyncio.Semaphore(max_concurrent_cases)
        self._tasks: dict[str, asyncio.Task[None]] = {}
        self._cancel_events: dict[str, asyncio.Event] = {}

    async def initialize(self) -> int:
        return await self.store.recover_orphaned_runs()

    async def close(self) -> None:
        tasks = list(self._tasks.values())
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        self._tasks.clear()

    async def create(self, body: EvalRunCreate, principal: Principal) -> dict[str, Any]:
        fingerprint = hashlib.sha256(
            json.dumps(body.model_dump(mode="json"), sort_keys=True).encode()
        ).hexdigest()
        # 与普通 Runtime 一样只支持单实例；锁保护进程内的准入和任务注册。
        async with self._create_lock:
            existing = await self.store.find_request(principal, fingerprint)
            if existing is not None:
                return {"run": existing, "duplicate": True}
            if len(self._tasks) >= self.max_pending_runs:
                raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503)
            request = ChatRequest(agent_key=body.agent_key, message="Eval")
            if isinstance(self.reader, DatabaseConfigReader):
                snapshot = await self.reader.read(request, principal, render_prompts=False)
            else:
                snapshot = await self.reader.read(request, principal)
            result = await self.store.create_run(
                body, principal, snapshot.model_dump(mode="json"), principal.request_id, fingerprint
            )
            if not result["duplicate"]:
                run_id = result["run"]["id"]
                self._cancel_events[run_id] = asyncio.Event()
                self._tasks[run_id] = asyncio.create_task(
                    self._run(run_id, body, snapshot, principal)
                )
            return result

    async def cancel(self, run_id: str, expected_version: int, request_id: str) -> dict[str, Any]:
        result = await self.store.request_cancel(run_id, expected_version, request_id)
        event = self._cancel_events.get(run_id)
        if event is not None:
            event.set()
        return result

    @staticmethod
    def _request(agent_key: str, message: str, fixture: dict[str, Any]) -> ChatRequest:
        context = {
            key: fixture[key]
            for key in ("problem_id", "submission_id", "language")
            if key in fixture and fixture[key] is not None
        }
        return ChatRequest(
            agent_key=agent_key,
            message=message,
            context=ChatContext.model_validate(context),
        )

    async def _run(
        self,
        run_id: str,
        body: EvalRunCreate,
        snapshot: ConfigSnapshot,
        principal: Principal,
    ) -> None:
        try:
            await self.store.mark_running(run_id)
            cases = await self.store.cases_for_run(run_id)
            semaphore = asyncio.Semaphore(body.concurrency)

            async def one(case_value: dict[str, Any], attempt: int) -> None:
                async with semaphore, self._case_slots:
                    case = EvalCase.model_validate(case_value["case"])
                    await self._run_case(
                        run_id,
                        case,
                        attempt,
                        snapshot,
                        principal,
                        self._cancel_events[run_id],
                    )

            tasks = [
                asyncio.create_task(one(case, attempt))
                for case in cases
                for attempt in range(1, body.repeat_count + 1)
            ]
            outcomes = await asyncio.gather(*tasks, return_exceptions=True)
            if any(isinstance(outcome, Exception) for outcome in outcomes):
                for outcome in outcomes:
                    if isinstance(outcome, Exception):
                        logger.error(
                            "Eval case persistence failed",
                            extra={
                                "run_id": run_id,
                                "error_type": type(outcome).__name__,
                            },
                        )
                await self.store.finish_run(run_id, "FAILED")
                return
            await self.store.finish_run(
                run_id, "CANCELLED" if await self.store.cancel_requested(run_id) else "COMPLETED"
            )
        except asyncio.CancelledError:
            await self.store.finish_run(run_id, "CANCELLED")
            raise
        except Exception as exc:
            logger.error(
                "Eval run failed",
                extra={
                    "run_id": run_id,
                    "error_type": type(exc).__name__,
                },
            )
            await self.store.finish_run(run_id, "FAILED")
        finally:
            self._tasks.pop(run_id, None)
            self._cancel_events.pop(run_id, None)

    async def _run_case(
        self,
        run_id: str,
        case: EvalCase,
        attempt: int,
        snapshot: ConfigSnapshot,
        principal: Principal,
        cancel_event: asyncio.Event,
    ) -> None:
        started = monotonic()
        answer: list[str] = []
        status = "COMPLETED"
        error_code: str | None = None
        summary: ModelSummary | None = None
        try:
            if cancel_event.is_set():
                raise ConfigError("AGENT_CANCELLED")
            conversation_id = None
            user_turns = [turn for turn in case.turns if turn.role == "user"]
            if not user_turns:
                raise ConfigError("AGENT_EVAL_CASE_INVALID")
            if case.agent_key is not None and case.agent_key != snapshot.agent_key:
                raise ConfigError("AGENT_EVAL_CASE_INVALID")
            alias = case.fixture.get("principal", "")
            if not isinstance(alias, str) or alias not in self.principal_aliases:
                raise ConfigError("AGENT_EVAL_PRINCIPAL_INVALID")
            case_principal = Principal(
                user_id=self.principal_aliases[alias],
                roles=frozenset({"user"}),
                request_id=str(uuid4()),
            )
            summary = ModelSummary()
            for user_turn in user_turns:
                if cancel_event.is_set():
                    raise ConfigError("AGENT_CANCELLED")
                request = self._request(snapshot.agent_key, user_turn.content, case.fixture)
                request = request.model_copy(update={"conversation_id": conversation_id})
                case_principal = case_principal.model_copy(update={"request_id": str(uuid4())})
                # 使用固定版本，但按 Case 的上下文与普通用户身份重新渲染、验证可见性。
                turn_snapshot = snapshot
                if isinstance(self.reader, DatabaseConfigReader):
                    turn_snapshot = await self.reader.read(request, case_principal, pinned=snapshot)
                accepted = await self.service.accept(
                    request, case_principal, snapshot=turn_snapshot
                )
                current_answer: list[str] = []
                async with accepted:
                    while True:
                        next_event = asyncio.ensure_future(anext(accepted.events))
                        cancelled = asyncio.create_task(cancel_event.wait())
                        try:
                            done, _ = await asyncio.wait(
                                {next_event, cancelled}, return_when=asyncio.FIRST_COMPLETED
                            )
                            if cancelled in done and cancel_event.is_set():
                                status = "CANCELLED"
                                error_code = "AGENT_CANCELLED"
                                break
                            try:
                                event = next_event.result()
                            except StopAsyncIteration:
                                break
                            if event.type == "token":
                                current_answer.append(str(event.data["text"]))
                            elif event.type == "error":
                                status = "FAILED"
                                error_code = str(event.data["code"])
                        finally:
                            # 包括进程关闭在内，先停止 anext，再由 AcceptedRun 关闭流。
                            for task in (next_event, cancelled):
                                if not task.done():
                                    task.cancel()
                            await asyncio.gather(next_event, cancelled, return_exceptions=True)
                conversation_id = accepted.state.conversation_id
                answer.append("".join(current_answer))
                if accepted.state.model_summary is not None:
                    summary.attempts.extend(accepted.state.model_summary.attempts)
                    summary.tools.extend(
                        value
                        for value in accepted.state.model_summary.tools
                        if value not in summary.tools
                    )
                    summary.knowledge.extend(accepted.state.model_summary.knowledge)
                    summary.attempts = summary.attempts[-16:]
                    summary.tools = summary.tools[-32:]
                    summary.knowledge = summary.knowledge[-32:]
                if status != "COMPLETED":
                    break
        except asyncio.CancelledError:
            raise
        except ConfigError as exc:
            status = "CANCELLED" if exc.code == "AGENT_CANCELLED" else "FAILED"
            error_code = exc.code
        except Exception:
            status = "FAILED"
            error_code = "AGENT_EVAL_CASE_FAILED"
        text = "\n\n".join(answer)
        checks = self._grade(case, text, status, error_code, summary)
        await self.store.add_result(
            run_id,
            {
                "id": str(uuid4()),
                "run_id": run_id,
                "case_id": case.id,
                "attempt": attempt,
                "status": status,
                "answer": text[:32_000] or None,
                "checks": checks,
                "model_summary": summary.model_dump(mode="json") if summary is not None else None,
                "error_code": error_code,
                "latency_ms": int((monotonic() - started) * 1000),
                "created_at": self.store.clock(),
                "finished_at": self.store.clock(),
            },
        )

    @staticmethod
    def _grade(
        case: EvalCase,
        answer: str,
        status: str,
        error_code: str | None,
        summary: ModelSummary | None,
    ) -> dict[str, Any]:
        folded = answer.casefold()
        expectations = case.expectations
        required_facts = {fact: fact.casefold() in folded for fact in expectations.required_facts}
        forbidden_claims = {
            claim: claim.casefold() in folded for claim in expectations.forbidden_claims
        }
        leaks = {value: value.casefold() in folded for value in expectations.must_not_leak}
        knowledge = summary.knowledge if summary is not None else []
        tools = set(summary.tools) if summary is not None else set()
        required_tools = {tool: tool in tools for tool in expectations.required_tools}
        forbidden_tools = {tool: tool in tools for tool in expectations.forbidden_tools}
        sources = [source for attempt in knowledge if attempt.ok for source in attempt.sources]

        def has_evidence(selector: str) -> bool:
            # 结构化选择器按字段精确匹配，避免 problem_id:2 命中题号 22。
            for field in ("problem_id", "document_id", "version_id"):
                prefix = field + ":"
                if selector.startswith(prefix):
                    expected = selector[len(prefix) :]
                    return any(str(getattr(source, field)) == expected for source in sources)
            return any(
                selector.casefold() in f"{source.title} {source.section}".casefold()
                for source in sources
            )

        required_evidence = {
            evidence: has_evidence(evidence) for evidence in expectations.required_evidence
        }
        hard_fail = (
            status != "COMPLETED"
            or any(forbidden_claims.values())
            or any(leaks.values())
            or any(forbidden_tools.values())
        )
        passed = (
            not hard_fail
            and all(required_facts.values())
            and all(required_tools.values())
            and all(required_evidence.values())
            and not any(forbidden_tools.values())
            and not any(forbidden_claims.values())
            and not any(leaks.values())
        )
        return {
            "pass": passed,
            "hard_fail": hard_fail,
            "required_facts": required_facts,
            "forbidden_claims": forbidden_claims,
            "must_not_leak": leaks,
            "required_tools": required_tools,
            "forbidden_tools": forbidden_tools,
            "required_evidence": required_evidence,
            "error_code": error_code,
        }
