# ADR-0002：Qdrant 与本地中文 Embedding

- 状态：已接受（Issue #171，用户确认）
- 日期：2026-10-15

## 决定

开发和部署统一 Qdrant，替代原先开发 Chroma / 部署 Qdrant 的双路线。
Embedding 固定采用 `BAAI/bge-small-zh-v1.5`，revision `7999e1d3359715c523056ef9478215996d62a620`，
512 维、Cosine、归一化，查询添加模型建议的中文检索指令。su8 继续负责 Responses 回答，不承担向量化。
CPU 向量化独立服务，镜像构建时下载固定版本，正常运行不需要外网；初版不引入重排服务。

MySQL 是 Agent 自有知识库、不可变文档版本、索引任务与发布状态的真源，原文存独立 MinIO bucket，
Qdrant 可从原文重建。索引任务使用 MySQL 租约 worker，不新增 MQ；后续如改成可靠消息通知，必须使用 Outbox。

## PR1 的发布单元

文档版本 UUID 同时标识其完整向量 generation。新版本各批向量写完后，在事务中把文档 active_version
切到新版本。知识库是启用文档集合，每次检索先读取已发布版本清单、Qdrant 前置过滤，返回后复核 MySQL。
PR3 再给 Run 固定版本清单并检查即时撤销；PR1 不提前把管理员检索开放给普通用户或 LangGraph。

## 权限与页面边界

管理员在后续 Agent / RAG 页上传和查看题解、讲义、平台文档。普通用户不能直接读原文，题目页面不增加题解功能。
材料用于模型回答不等于禁止模型复述，比赛和题目授权由业务 Go 服务决定，Prompt 不是安全边界。

## 代价与后续

新增 Qdrant、Embedding 与 worker 的部署和资源成本；首次构建需要下载 CPU PyTorch 与约 92 MiB 权重。
Embedding 容器限制 2 CPU / 2 GiB，单次至多 8 条、每条含特殊 token 不超过 512。
先以小语料建立召回/延迟基线；模型或维度升级必须另建不兼容集合、重建并切换，不能混写。
原文和旧版本不主动物理删除，先保证追溯与后续 Run 引用；孤立对象有受控 GC。旧向量清理随 Run 保留策略后续实现。
