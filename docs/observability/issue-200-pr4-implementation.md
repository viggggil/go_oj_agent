# Issue #200 PR4：Judge Trace 管理员排障入口

PR4 将已经接通的 Judge 同步、Outbox、Worker、结果消费者和 Contest 投影 Trace 暴露给平台管理员。入口由 Gateway 提供，服务端只读取 Judge Service 的 `GetSubmission` RPC 做资源校验，再使用部署时固定的 `JUDGE_OBSERVABILITY_GRAFANA_URL` 构造 Grafana/Tempo Explore 链接。

## 接口与权限

`GET /api/v1/admin/observability/judge/submissions/{submission_id}` 只接受 `admin` 或 `system_admin`。`agent_admin` 不能访问 Judge Trace。Gateway 不接受 Grafana、Tempo、TraceQL 或重定向地址作为请求参数，也不读取 Judge 数据库。

响应包含 `submission_id`、`tempo_url`、`link_available`、`trace_available` 和 `status`。`SEARCHABLE` 只表示固定检索链接已生成；`trace_available` 为 `false`，因为采样、Collector 故障和 Tempo 保留策略可能导致实际 Trace 缺失。未配置或地址无效时返回 `NOT_CONFIGURED`，不伪造健康状态。

TraceQL 只按受控的 `submission.id` 属性检索，submission id 不作为指标标签。前端的提交详情和“监控 → 判题链路”页面仅向平台管理员展示入口；普通用户不会看到其他提交的 Trace 链接。

## 运维与失败行为

- Gateway 到 Judge 的资源校验失败时沿用 Judge 的认证、无权和不存在语义。
- Grafana 地址必须是服务端配置的 HTTP(S) URL；不允许用户 URL 代理或开放重定向。
- Trace 不存在、未采样、已过期或 Collector/Tempo 不可用时，页面显示“尚不可用/未采样”，业务判题状态不受影响。
- 观测链接不包含源码、测试输入输出、Prompt、Tool 参数、用户请求正文或凭据。

本地 Compose 可设置 `JUDGE_OBSERVABILITY_GRAFANA_URL`（默认 `http://localhost:3000`）。启用观测 overlay 后，在 Grafana Explore 中确认 Tempo 数据源和保留窗口，再以提交编号验证完整链路。
