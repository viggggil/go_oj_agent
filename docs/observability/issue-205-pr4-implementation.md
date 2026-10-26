# Issue #205 PR4：Agent Chat 与后台任务 Metrics

## 实现范围

- 保留既有 `agent.run.*` 计数、在途数、总耗时和首事件耗时。
- 增加模型 attempt/retry、耗时和在途指标；每次 Provider attempt 独立记录 `success|error|timeout|cancelled`。
- 增加 Tool 和知识检索的调用数、耗时和在途指标；仅将受控 Tool 名和 Agent purpose 用作维度。
- 为 Memory、Eval/Case/Review、RAG ingestion 增加任务结果、耗时和在途指标；使用受控 `purpose` 与 `status`。
- Memory 与 RAG ingestion 在决定安排重试后记录 `status=retrying`。RAG 任务只在原有 finish 事务成功确认仍持有租约后报告 retry/completed；失租单独记 `lease_lost`。
- 所有指标记录使用安全属性策略，未知模型/工具归一为 `other`。指标 API 调用失败仅写受控调试错误码并被忽略，不改变 Chat、Provider retry、任务状态、lease、数据库持久化或取消行为。
- 同步更新指标字典、Agent README 和系统架构说明。

## 可靠性与隐私边界

Metrics 是业务旁路数据。模型 attempt 指标在当前 attempt 退出时记录，不改变已有 attempt 限额、错误转换或退避；Tool/RAG 指标在调用返回或抛错后记录，不改变 Tool 授权、超时或结果验证。后台任务最终状态沿用既有持久化结果：Eval/Review 仍在原 Store 保存后报告终态；RAG ingestion 只有 lease-fenced `finish` 成功后才报告持久化状态，失败时继续沿用现有重领和指数退避。

Metrics 属性不包含 Run/用户/会话/任务/文档/题目 ID、对话或 Prompt、Tool 参数/结果、RAG 文本、数据集样本、Provider 原始错误或凭据。模型名和 Tool 名经过受控枚举；未知值折叠为 `other`。

## 验证

- `./.venv/bin/pytest tests -m 'not integration' -q`：237 passed，55 deselected。
- `./.venv/bin/ruff check app tests`、`./.venv/bin/ruff format --check --exclude app/grpcgen app tests`、`./.venv/bin/mypy app tests`：通过。
- `git diff --check`：通过。
- 首次全量单测出现一个 `/api/v1/agent/agents` 管理路由测试 401；该测试单独通过，随后再次全量执行 237 passed。CI 仍需确认。
- Async reliability review：RELIABLE FOR REVIEWED SCENARIOS。Metrics 不改模型重试判断、Memory 退避、RAG claim/heartbeat/lease fencing/finish 状态或重领行为；RAG retry 只在 finish 已持久化 PENDING 后记录一次，失租单独计数。
- 独立代码审查：APPROVE。剩余验证边界是 RAG retry 计数测试用 Fake Store 验证 worker 控制流，没有新增 MySQL 事务集成覆盖；本 PR 未改 Store 的持久化实现。
