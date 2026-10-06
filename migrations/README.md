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
