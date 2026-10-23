# Issue 193：Agent Chat 管理诊断闭环

本次切片把聊天响应中的 `X-Agent-Run-ID`、Agent 数据库 `agent_runs.trace_id` 与管理员诊断页面连起来。

## 查询边界

`GET /api/v1/admin/agent/observability/runs` 支持分页和状态筛选，详情路径返回单个 Run。Gateway 和 Agent 都要求管理员角色（`admin`、`system_admin` 或 `agent_admin`），并使用既有短期委托。

响应只包含 Run ID、Trace ID、状态、时间、耗时、受控错误码以及工具步骤/知识检索数量。不会返回消息、Prompt、配置快照、工具参数或结果、RAG 文本、原始异常和凭据。历史记录没有本地 Trace 时显示为“无本地 Trace”。

Vue Agent 工作区的“运行诊断”页面以低频轮询显示列表和详情；页面隐藏时暂停轮询。Tempo/LangSmith 跳转地址在后续部署配置受控目标后再增加，当前不会接受或代理用户提供的 URL。

## 验证

- Agent API、Gateway 路径白名单和 JSON 响应契约限制查询边界。
- 前端构建验证类型和路由注册。
- 启动 Compose 后检查 Agent/Gateway 健康端点。
