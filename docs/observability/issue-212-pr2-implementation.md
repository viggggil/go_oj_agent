# Issue #212 PR2：固定 Metrics 查询与服务接入状态

## 交付内容

- 扩展 Gateway 固定 PromQL 查询，增加 API 延迟 P50/P95/P99、Agent Run 成功率和 Agent TTFT P95；缺少 Histogram 或样本时保持 `no_data`。
- 增加 `GET /api/v1/admin/observability/services`，返回固定服务清单的采集/探测接入状态和稳定说明。
- 平台管理员可查看 Gateway、User、Problem、Judge、Contest、Agent、Judge Worker、RAG Worker；`agent_admin` 只查看 Agent 和 RAG Worker。普通用户没有访问权限。
- 服务状态接口不接受服务地址，不把任意指标样本推断成业务健康；User、Problem、Judge、Contest、Judge Worker 使用固定 TCP 探测，Agent 使用固定 `/readyz`，RAG Worker 因没有独立探针返回 `not_configured`。
- 更新 API 文档和固定指标实现记录。

## 语义边界

当前服务状态 API 是“观测接入状态”，不是完整的业务健康探测。Gateway 当前请求可达时为 `up`；TCP 探测只说明端口可连接，Agent `/readyz` 才表示健康端点响应；RAG Worker 因没有独立探针为 `not_configured`。探测失败返回受控 `down` 和稳定错误码，不使用前端传入 URL。

## 验证

- Gateway observability 相关 Go 契约测试
- `go test ./services/gateway/internal/server ./services/gateway/internal/client`
- `make fmt-check`
- `make vet`
- `make build`
- Web build、单测和既有监控 E2E（包含服务状态 API mock）

