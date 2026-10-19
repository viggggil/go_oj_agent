# 数据库迁移

本目录保存 MVP 阶段的 MySQL migration SQL 文件。

## 目录约定

- `mysql/`：本地或平台级 schema 初始化。
- `user/`：`user-service` 拥有的 `oj_user` 表结构。
- `problem/`：`problem-service` 拥有的 `oj_problem` 表结构。
- `submission/`：`judge-service` 拥有的 `oj_submission` 表结构。
- `contest/`：`contest-service` 拥有的 `oj_contest` 表结构。
- `agent/`：`agent-service` 拥有的 `oj_agent` 会话、消息和最小 Run 生命周期。

Agent `000001_create_agent_runtime` 为 PR2 新增三张表，用生成列唯一索引约束同会话
最多一个 RUNNING。数据仅归 Agent 所有，user_id 不建立跨库外键。Run 的会话外键为
RESTRICT；下行迁移按 messages、runs、conversations 顺序删除，删除全部历史数据。
新数据库在 Compose 初始化时执行 up SQL；已有数据库需部署者显式应用迁移，应用不
自动执行 SQL，也不增加 Alembic 等第二套迁移体系。

Contest `000002_create_result_projection` 添加消费幂等、提交事实（含作废记录）和题目结果表，
时间使用 DATETIME(6) 保留事件版本精度。重建索引为 `(contest_id,user_id,problem_id,submitted_at,submission_id)`。
外键仅约束本地比赛参与者/题目，不跨服务建外键。回滚依次删除结果、事实和消费记录表。
Compose 新建数据库自动应用此迁移；已有数据库须显式执行 up SQL，服务启动不会自动迁移。

## 当前阶段

当前阶段只提交 SQL migration 文件，不引入 migration runner，也不绑定 ORM。

## 规则

- 每个 `.up.sql` 必须有对应的 `.down.sql`。
- 同一个 schema 内可以使用外键。
- 禁止跨 schema 外键。
- 跨服务字段只保存外部 ID reference。
- `status` / `verdict` 使用 `VARCHAR`，暂不使用 MySQL ENUM。
- 测试数据正文、源码产物、编译日志和大文档正文不直接存入业务事件。

### Agent PR6

先应用 `agent/000003_create_model_provider.up.sql`，再部署启用 model Runtime。空库初始化和已有 PR5 数据均使用这一步；Docker init SQL 只在首次创建数据卷执行，已有卷需要通过正常迁移流程应用。迁移扩展 provider kind、新增加密凭据/引用/审计和 Run 模型摘要，不保存主密钥。

Down 首先拒绝仍有 Provider 配置的库，避免孤立 Model 引用。清理新配置/引用后才可回滚；会删除凭据与模型摘要，不删除业务会话/消息/Run。保留真实运行证据时不要执行 down，旧应用应保留 expand Schema 并关闭真实模型。

### Agent Eval 数据集

`agent/000005_create_eval` 增加管理员评测数据集元数据和 Case 投影表。原始 JSONL
保存在 Agent 专用 MinIO，数据库只保存不可变对象键、内容哈希和严格校验后的 JSON 投影。
内容哈希唯一约束支持重复上传去重；评测运行、程序化结果和人工评审表由后续迁移增加。

### Agent Eval Runner

`agent/000006_create_eval_runs` 增加批量评测运行和 Case 结果表。运行创建时固定不含凭据明文的内部配置
快照（HTTP 只返回有界版本元数据），Runner 通过普通 Agent Runtime 执行受控新会话；结果只保存回答、程序化检查、延迟、
错误码和有界模型摘要。运行支持排队、取消、重复执行和有界并发，人工/评审 Agent 审计字段
在后续迁移增加。

此迁移为增量建表，先执行迁移再启用新版 Runner；旧代码可忽略新表，新代码不兼容缺表部署。
`down` 删除运行及其全部 Case 结果，不能恢复评测历史；数据集/原始 JSONL 和普通聊天记录保留。
同时增加 `agent_conversations.purpose`（chat/eval），默认 chat 保留历史兼容性，并增加用途分页索引。
回滚会先删除 Eval 的普通 Runtime 会话/消息/Run，再删除用途列，避免评测内容进入普通历史。
因此新版/旧 schema 不兼容，迁移先于应用升级；回滚代码也应在应用停止后配合迁移执行。
仅单实例启动时将遗留 QUEUED/RUNNING 标为 FAILED，尚无跨实例租约或自动续跑。

### Agent Eval 人工评审

`agent/000007_create_eval_reviews` 增加结果 `review_version`（默认 0）/`manual_review` 与追加式审计表。
审计 `(result_id,version)`、request UUID 均唯一，操作者、旧值、新值、原因和当前评分在同一事务保存。
迁移先于 Agent/Gateway/Web 部署；Compose 升级入口检测新表和两列，遇到部分结构拒绝继续。
已有运行/结果保留，旧应用可忽略新列。down 删除人工评分和审计，不删除数据集、运行结果、程序检查或普通聊天；审计数据无法由 down 恢复。

### Agent Eval 模型评审

`agent/000008_create_eval_model_reviews` 为运行保存评审 Agent 快照，为结果保存模型评审状态、
严格 JSON 评分和错误码，并以追加表记录每次尝试。迁移必须在新版 Agent/Gateway/Web 部署前执行；
旧应用可以忽略新增列和表。`down` 会清空运行中的评审绑定并删除模型评审结果，保留运行、回答和程序检查。

### Agent 会话持久化摘要

`agent/000009_create_conversation_summaries` 为每个 Agent 会话增加一行结构化摘要，保存摘要
版本、已覆盖的最后一条完整 assistant 消息和用途隔离字段。摘要游标由应用校验只能指向
COMPLETED Run 的完整问答边界；乐观版本号用于后续异步摘要 Worker 的并发更新。该迁移不
删除或改写已有消息，旧应用可以忽略新表；部署应先应用迁移再启用读取摘要的新 Agent。
