# Issue 193：Agent Chat 管理诊断闭环

本次切片把聊天响应中的 `X-Agent-Run-ID`、Agent 数据库 `agent_runs.trace_id` 与管理员诊断页面连起来。

## 查询边界

`GET /api/v1/admin/agent/observability/runs` 支持分页和状态筛选，详情路径返回单个 Run。Gateway 和 Agent 都要求管理员角色（`admin`、`system_admin` 或 `agent_admin`），并使用既有短期委托。

响应只包含 Run ID、Trace ID、状态、时间、耗时、受控错误码以及工具步骤/知识检索数量。不会返回消息、Prompt、配置快照、工具参数或结果、RAG 文本、原始异常和凭据。历史记录没有本地 Trace 时显示为“无本地 Trace”。

Vue 顶层“监控”工作区的“Agent 运行诊断”页面以低频轮询显示概览、列表和详情；支持分页、状态筛选、stale 状态和从 Chat 直达当前 Run。运行诊断不再属于 Agent 管理工作区。LangSmith 导出状态随 Run 持久化，只有服务端确认上传成功且配置的项目 UUID 同时作为实际上传目标时才提供链接。Tempo/LangSmith 地址由服务端配置构造，不接受或代理用户提供的 URL；页面失焦时暂停刷新。

## 验证

- Agent API、Gateway 路径白名单和 JSON 响应契约限制查询边界。
- 前端构建验证类型和路由注册。
- 启动 Compose 后检查 Agent/Gateway 健康端点。

## 监控工作区导航

Agent 运行诊断已迁出 Agent 管理侧栏，统一放入顶栏一级“监控”入口。管理员可从监控工作区进入 Agent Run 诊断与判题服务拓扑；`agent_admin` 仅能进入 Agent 诊断，判题视图仍要求 `admin` 或 `system_admin`。旧 `/agent/observability` 前端地址重定向到新的监控路由。

判题视图当前展示 Gateway → Judge Service → RabbitMQ → Judge Worker/Sandbox → Contest 结果回写的服务拓扑，并提供提交记录入口。判题 Outbox、队列积压、Worker/Sandbox 的实时指标和 Trace 传播仍未接入，页面明确显示“指标接入中”，不会以静态拓扑冒充运行健康状态。
