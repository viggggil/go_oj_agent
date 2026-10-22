# Agent 系统设计总览

## 目标与边界

Agent 服务负责对话编排、配置快照、模型调用、只读工具、知识检索、评测和会话上下文。它不直接读取用户、题目、提交或比赛数据库；需要业务数据时通过 Gateway 委托后的 gRPC Tool 访问对应业务服务，由业务服务完成最终授权。

系统的持久化边界是独立的 `oj_agent` 数据库。MySQL 保存配置、Run、消息、知识库元数据、评测数据和摘要；MinIO 保存知识文档原文；Qdrant 保存已发布文档的向量和切片元数据。Redis 与 RabbitMQ 服务于 OJ 的其他业务链路，不参与 Agent 对话状态或摘要队列；Agent 的摘要 Worker 使用进程内有界队列，在成功 Run 后异步处理。

## 服务边界

```text
浏览器
  │ REST / SSE，外部 Access JWT
  ▼
Gateway
  │ 验证用户身份，签发短期 Agent 委托 JWT
  ▼
Agent Service
  ├─ Control Plane：Agent / Prompt / Skill / Tool 元数据
  ├─ Run Service：会话、Run 生命周期、SSE 事件和终态事务
  ├─ LangGraph Runtime：ReAct 图、模型和工具循环
  ├─ RAG：文档版本、索引 Worker、Qdrant 检索
  ├─ Eval：数据集、Runner、程序检查、模型评审
  └─ Memory：历史上下文、增量摘要和后台 Worker
       │ gRPC Tool（带内部委托身份）
       ▼
题目、提交、判题、比赛等业务服务
```

Gateway 不复制业务数据库，也不决定业务资源权限。Agent 只信任委托 JWT 中的身份、角色、请求编号和路径绑定；请求体里的用户标识、角色或权限字段不能提升权限。

## 配置与版本

Agent、Prompt、Skill、Model Profile、Provider 和知识版本都是控制面资源。配置修改生成新的不可变版本，当前指针和审计记录在事务内更新。Run 创建时固定 Agent、Prompt、Skill、Tool 策略、模型、Provider、知识分组和已发布版本的快照；Run 运行期间不跟随后台配置变化。

Provider 凭据由 Agent 自有凭据仓储加密保存，密钥由部署侧 keyring 提供。浏览器和普通用户接口看不到 Provider 凭据、Prompt 安全前缀或知识文档管理接口。

## 一次回答的数据流

1. Gateway 验证 Access JWT，生成绑定具体 Agent 路径的短期委托 JWT。
2. Agent 根据用户身份和 Agent key 读取配置，创建会话与 RUNNING Run，并保存用户消息。
3. ContextAssembler 读取同一用户、同一会话中最近的完整成功轮次和可用摘要。
4. ReAct 图准备安全前缀、配置文本、上下文、工具 Schema 和当前问题。
5. 模型决定直接完成或调用只读工具；工具调用经过 Schema、权限、预算和重复调用检查。
6. 模型完成回答后，RunService 在数据库事务中保存 assistant 消息、终态和有界摘要。
7. 只有终态提交成功后才发送 `done`；停止、取消、超时和依赖故障都发送稳定错误码。
8. 成功轮次通知 Memory Worker，后台归档较早轮次，不阻塞当前回答。

## 安全与可靠性原则

- LLM、Prompt 和 Skill 不是授权边界，业务服务仍做最终权限判断。
- 工具只能来自代码注册表；模型不能动态创建工具、执行任意 Python 或访问数据库。
- Provider 请求固定目标 origin，禁止把凭据发送到未经允许的地址。
- 所有模型、工具、RAG 和 Eval 调用都有时间、事件、输入、输出和调用次数上限。
- 流式客户端必须消费到终态或显式关闭；Run 终态只能写入一次。
- 任何索引、评测、摘要故障都不能把已完成的聊天改写为成功或伪造证据。
