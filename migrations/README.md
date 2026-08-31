# 数据库迁移

本目录保存 MVP 阶段的 MySQL migration SQL 文件。

## 目录约定

- `mysql/`：本地或平台级 schema 初始化。
- `user/`：`user-service` 拥有的 `oj_user` 表结构。
- `problem/`：`problem-service` 拥有的 `oj_problem` 表结构。
- `submission/`：`submission-service` 拥有的 `oj_submission` 表结构。

## 当前阶段

当前阶段只提交 SQL migration 文件，不引入 migration runner，也不绑定 ORM。

## 规则

- 每个 `.up.sql` 必须有对应的 `.down.sql`。
- 同一个 schema 内可以使用外键。
- 禁止跨 schema 外键。
- 跨服务字段只保存外部 ID reference。
- `status` / `verdict` 使用 `VARCHAR`，暂不使用 MySQL ENUM。
- 测试数据正文、源码产物、编译日志和大文档正文不直接存入业务事件。

