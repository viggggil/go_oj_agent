# Observability 文档

这些文档描述当前代码和默认本地部署的能力，不记录功能逐步开发或 PR 实施过程。

- [系统设计](design.md)：信号链路、服务职责、管理 API、权限和现有限制。
- [Metrics 指标字典](metrics.md)：指标名称、类型、单位、标签与计数语义。
- [Loki 日志架构与运行约束](loki.md)：日志字段、采集范围、查询、保留、轮转和安全边界。
- [HTTP API 契约](../api.md)：监控、日志和 Trace 查询接口参数与响应约束。
- [本地 Compose 配置](../../deploy/compose/README.md)：启动、停止和开发环境配置。
