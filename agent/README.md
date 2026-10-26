# Agent 服务

Agent 服务为在线判题平台提供面向编程学习的对话、知识检索和评测能力。它负责 Agent 配置与运行编排，不拥有用户、题目、提交或比赛等业务数据；访问这些数据时通过 Gateway 委托身份调用相应 Go 服务，由数据所属服务进行最终授权。

## 架构

```text
浏览器
  │ REST / SSE + 用户 Access JWT
  ▼
Gateway ──验证身份并签发短期、路径绑定的委托 JWT──▶ Agent Service
                                                       ├─ 配置管理与运行快照
                                                       ├─ 对话、消息、SSE 与取消
                                                       ├─ LangGraph ReAct Runtime
                                                       ├─ 只读业务工具与代码运行工具
                                                       ├─ RAG 检索与文档索引
                                                       ├─ Eval Runner 与模型评审
                                                       └─ 会话上下文与摘要 Worker
                                                            │ gRPC / 内部服务
                                                            ▼
                                                 题目、提交、判题、比赛等服务
```

Agent 的 MySQL Schema 独立于其他业务服务。MySQL 保存配置、运行、消息、知识库元数据、评测数据和摘要；MinIO 保存知识文档原文；Qdrant 保存已发布知识文档的向量；本地 Embedding 服务使用 `BAAI/bge-small-zh-v1.5`。OJ 的 Redis 与 RabbitMQ 服务不参与 Agent 对话状态和摘要队列。

Agent、Prompt、Skill、模型配置及已发布知识版本在对话开始时形成不可变运行快照。管理员更新配置只影响后续运行。Provider 密钥由服务端加密保存，普通用户接口和浏览器不暴露密钥或知识文档管理内容。

## 对话运行

所有真实模型回答都通过同一张 LangGraph ReAct 图执行，包含上下文准备、预算检查、模型推理、动作校验、工具执行、观察和最终回答校验。模型可根据当前问题选择直接回答或调用可用工具；工具集合受 Agent 与 Skill 配置、代码注册表、运行预算和用户授权共同限制。Skill 为模型提供任务说明和工具参考，不授予权限。

会话历史按用户与会话隔离，只使用已完整成功的问答。模型可通过内部 `retrieve_knowledge` 工具按需检索 Agent 已绑定分组中的已发布资料。工具调用经过参数校验、调用去重、权限检查、超时及结果大小限制；业务工具最终仍由对应业务服务鉴权。

聊天使用 SSE 发送进度、结构化思路摘要、回答片段、完成或错误事件。停止操作会取消运行；只有持久化终态成功后才发送完成事件。工具原始结果、隐藏思路和管理配置不会通过普通用户聊天事件返回。

## 知识库（RAG）

管理员在 Agent 知识库管理页面按分组上传 Markdown 或文本资料。文档原文保存于 MinIO，元数据和版本保存在 MySQL；索引 Worker 负责切分、向量化并写入 Qdrant。只有索引成功且管理员启用的文档版本会参与检索。Agent 绑定知识分组，运行启动时固定可访问分组和发布版本；检索时再次核验文档状态以及题目和比赛访问权限。

用户只能在模型回答中看到整理后的信息，不能直接浏览知识分组、文档原文或内部检索片段。管理员可在管理页面查看文档、索引状态、版本和运行证据。

## Eval

管理员上传 JSONL 数据集，启动评测时选择目标 Agent。Runner 为每个 Case 建立隔离会话并调用普通运行服务，保存回答、工具摘要、知识证据和程序检查结果。程序门禁通过的回答由固定的 `eval_reviewer` Agent 自动评审；该评审 Agent 不启用工具和知识库。管理员也可人工评分，或对不可用的模型评审按结果重试。

评测完成状态表示运行已结束，质量结果需结合程序检查、模型评分和人工复核判断。Case、运行配置及评审配置均保存快照，避免后续配置修改改变历史评测含义。

## 上下文与长期记忆

新 Run 使用同一用户、同一会话里最近的完整成功轮次和可用摘要。失败、取消、未完成运行、工具原始结果和残缺消息不会进入对话历史。后台有界 Memory Worker 在成功运行后增量整理较早问答，保留最近轮次原文，并用版本和消息游标防止覆盖或重复归档。当前生成器以抽取和压缩历史对话为主，不调用 LLM 推断事实。摘要是辅助上下文，不是系统指令；摘要任务失败不会改变已完成的回答。

## 代码布局

```text
agent/app/api/          聊天、历史、健康检查与配置管理 API
agent/app/graphs/       LangGraph Runtime 与 ReAct 工作流
agent/app/tools/        工具注册、授权和执行
agent/app/rag/          知识库管理、检索与索引协调
agent/app/evals/        数据集、评测运行、评分与评审
agent/app/memory/       上下文装配、增量摘要与后台 Worker
agent/app/storage/      MySQL 存储与仓储实现
agent/embedding/        本地文本向量化服务
agent/tests/            Agent 单元与集成测试
```

## 运行和配置

仓库根目录的 Makefile 是开发命令的来源。常用目标如下：

```bash
make infra-up          # 启动本地依赖和应用服务
make infra-down        # 停止 Compose 服务
make agent-dev         # 本地启动 Agent 开发服务
make agent-test-unit   # Agent 非集成测试
make agent-test-integration
make agent-check       # Agent 格式、静态检查和类型检查
```

Compose 部署说明、端口、环境变量和模型凭据配置见 [`deploy/compose/README.md`](../deploy/compose/README.md)。Agent 环境变量示例见 [`.env.example`](.env.example)。服务提供 `/healthz` 存活探针和 `/readyz` 数据库就绪探针。

Agent Metrics 通过 OpenTelemetry OTLP 发往可选 Collector；设置 `OTEL_EXPORTER_OTLP_ENDPOINT` 后由 Prometheus 抓取 Collector。当前包含 Run 终态与耗时、模型 attempt/retry、Tool 与知识检索调用，以及 Memory、Eval/Review、RAG ingestion 后台任务结果、耗时和在途数量。指标只使用受控 purpose/model/tool/status/outcome 标签；Prompt、对话、Run/用户/会话 ID、工具参数和结果、RAG 文本及原始异常不会成为指标属性。未配置 Collector 或指标记录失败时，Agent 业务处理继续运行。详细名称与计数口径见 [`Metrics 字典`](../docs/observability/metrics.md)。

## 设计文档

- [系统设计总览](../docs/agent/design/system-overview.md)
- [工作流与路由](../docs/agent/design/workflow-routing.md)
- [知识库与 RAG](../docs/agent/design/rag-design.md)
- [Eval](../docs/agent/design/eval-design.md)
- [上下文与长期记忆](../docs/agent/design/context-memory-design.md)
