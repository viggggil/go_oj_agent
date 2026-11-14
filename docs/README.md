# 文档索引

本文档按当前用途归类项目文档。接口和实现细节以代码、Protobuf 定义及迁移脚本为准。

## 项目概览与核心设计

- [产品功能与当前范围](design/requirements.md)：当前用户角色、产品能力与业务链路。
- [系统架构](design/architecture.md)：服务职责、通信、判题、Agent、存储和可观测性边界。
- [API 与消息契约](design/api.md)：REST、gRPC、SSE、Protobuf 和 RabbitMQ 契约。
- [数据库与存储设计](design/database.md)：MySQL Schema、Redis、MinIO、迁移及一致性约束。
- [身份认证与业务授权](design/authentication-authorization.md)：外部令牌、内部委托和服务授权边界。
- [Go-Kratos 工程规范](kratos.md)：服务目录、依赖方向和代码组织约定。

## Agent

- [Agent 系统总览](agent/design/system-overview.md)：运行边界、身份、配置快照和一次对话流程。
- [工作流与路由](agent/design/workflow-routing.md)：ReAct 节点、工具调用、SSE 与取消语义。
- [上下文与记忆](agent/design/context-memory-design.md)：多轮上下文、摘要和数据边界。
- [RAG 设计](agent/design/rag-design.md)：文档版本、索引、检索和授权。
- [Eval 设计](agent/design/eval-design.md)：数据集、评估运行和质量解释。
- [ReAct 评测报告](agent/react-eval-comparison.md)：固定数据集上的模型评测结果及其限制。

## 可观测性

- [可观测性索引](observability/README.md)
- [系统设计](observability/design.md)
- [Metrics 指标字典](observability/metrics.md)
- [Trace 能力与查询](observability/trace.md)
- [Loki 日志架构](observability/loki.md)

## 部署与运维

- [Compose 部署](../deploy/compose/README.md)
- [比赛排行榜缓存运维](contest-cache-operations.md)
- [压力测试工具](design/load-testing.md)

## 测试结果

- [Prompt、Skill 与运行时评测对比（2026-11-14）](agent/prompt-skill-eval-comparison-2026-11-14.md)，包含脱敏逐题 JSON。
- [云端压力测试报告（2026-11-01）](reports/cloud-load-test-2026-11-01/REPORT.md)，包含原始 JSON 数据。

## 架构决策

- [ADR-0001：Judge Service 同时负责提交与调度](adr/0001-merge-submission-and-scheduler-into-judge-service.md)
- [ADR-0002：Qdrant 与本地中文 Embedding](adr/0002-qdrant-local-chinese-embedding.md)

## 仓库指南

- [文档维护与项目约定](AGENTS.md)
