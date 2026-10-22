# Agent 工作流与路由设计

## LangGraph ReAct 图

所有真实模型回答都使用同一张显式图，是否有工具只影响节点中的行为，不切换到另一套运行模式：

```text
START
  ↓
prepare → check_budget → reason
                          ├─ finish → validate_answer → finalize → END
                          └─ action → validate_action → act → observe ─┘
```

### prepare

读取 Run 的配置快照，生成安全前缀、Prompt/Skill 任务说明、会话摘要、最近完整轮次、当前问题和工具 Schema。历史消息按 user/assistant 角色放入模型输入；摘要只作为明确标记的辅助上下文，不拥有系统指令权限。当前问题只追加一次。

### reason

模型以流式 Responses 请求返回文本、结构化思路摘要和 Function Call。无工具时，文本可以实时发送 token；有工具时，模型在工具轮次产生的正文暂存，只有最终 Finish 通过校验后才发送给用户。

### validate_action

程序检查整批调用：工具名是否在快照白名单、JSON 是否严格有效、参数 Schema 是否匹配、call_id 是否重复、相同参数是否重复、调用数量是否超预算。拒绝项会写入脱敏 ToolTrace，不执行外部 RPC。

### act / observe

ToolExecutor 在单次调用上施加超时、结果大小和角色约束。业务 Tool 通过 Gateway 委托身份调用拥有数据的 Go 服务。成功结果成为本轮 Evidence；失败结果只成为 Observation，模型可以在剩余预算内修正或结束。RAG 也是同一工具循环中的只读 Tool。

### check_budget / finalize

每轮重新计算 deadline、模型调用数、输入输出 token、工具调用数、事件数和剩余输出预算。完成回答经过输出验证后，带工具的轮次在 finalize 阶段统一发送暂存正文。RunService 先提交终态，再发送 `done`。

## 路由与工具选择

模型选择“直接回答”还是“调用工具”属于 ReAct 决策，不由自然语言中的伪指令决定。工具集合是以下集合的交集：

```text
Agent allowlist
∩ Skill allowlist
∩ 代码 Registry 中 enabled/read 工具
∩ 当前身份和业务策略允许的工具
```

Skill 提供任务说明和可用工具参考，不让用户手动选择某个 Skill，也不能改变业务授权。没有绑定知识分组时，模型不会获得 `retrieve_knowledge`；绑定后该工具自动加入可用集合，仍受同一预算和权限限制。

## SSE 与取消

对外事件保持稳定的 `progress`、`reasoning_summary`、`token`、`done`、`error` 类型。工具参数、工具原文结果、Prompt 正文和内部思路不通过普通用户 SSE 发送。停止按钮会中止 HTTP/SSE 流、取消底层 Provider 请求，并使 Run 进入 CANCELLED；客户端代次和会话切换会丢弃旧流事件，防止旧 Run 污染新对话。

## 失败处理

模型认证、额度、限流、超时、输入/输出超限、工具拒绝、依赖不可用和无效响应都映射为稳定错误码。默认不自动重放聊天；同一请求不会因为前端刷新而重复执行。评测和摘要可使用各自的有限重试，但不改变已落库的聊天终态。
