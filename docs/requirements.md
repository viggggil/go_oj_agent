# 产品功能与当前范围

Go OJ Agent 是面向编程学习者与题目管理员的在线判题和 AI 编程辅助平台。本文描述当前仓库提供的产品能力。

## 用户与功能

### 学习者

- 注册、登录、维护个人资料。
- 浏览题库、查看题目说明和样例，按支持的编程语言提交代码。
- 查看判题状态、结果、测试点详情和个人提交记录。
- 参加比赛，查看比赛题目与排行榜。
- 与 Agent 进行流式对话，查看本人会话，并使用已授权的题目、提交、判题和知识检索上下文。

### 管理员

- 管理题目、标签、样例和测试数据。
- 创建和管理比赛、题目绑定及参赛数据。
- 管理 Agent、Prompt、Skill、模型配置、知识库和 Eval 数据。
- 查看运行诊断、评测结果和平台监控信息。

### 运维人员

- 查看服务健康状态、日志、指标和分布式 Trace。
- 监测异步判题队列、Worker 执行与 Agent 运行状态。
- 使用 Docker Compose 部署开发或集成测试环境。

## 核心业务链路

### 判题

```text
Gateway → Judge Service → MySQL Submission + Outbox
                         → RabbitMQ → Judge Worker → Sandbox
                         ← RabbitMQ Result ←─────────┘
```

Judge Service 保存提交和最终结果；Worker 在隔离环境运行用户代码。RabbitMQ 消息采用至少一次投递，重复投递由幂等处理。题目测试集和提交源码保存在 MinIO。

### Agent

Gateway 验证外部身份并签发短期内部委托身份。Agent Service 运行有界 LangGraph ReAct 工作流，支持多轮上下文、只读业务工具、RAG、SSE、取消、Eval 和管理配置。Agent 不直接访问业务数据库；资源权限由对应 Go 服务判定。Agent 配置与运行数据保存在独立数据库，文档原文保存在 MinIO，向量保存在 Qdrant。

## 平台能力

- Go-Kratos 微服务：Gateway、User、Problem、Judge、Judge Worker、Contest。
- Python FastAPI Agent Service，Vue 3 Web 前端。
- gRPC/Protobuf 服务契约，RabbitMQ 异步事件，MySQL 业务存储，Redis 缓存，MinIO 对象存储。
- OpenTelemetry 指标、日志与 Trace；本地观测栈包含 Prometheus、Grafana、Loki 和 Tempo。
- 单元、集成、契约、端到端及 Agent Eval 检查。

具体接口、数据结构、服务边界和部署方式见[文档索引](README.md)。
