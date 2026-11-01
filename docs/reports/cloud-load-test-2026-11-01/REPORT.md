# 云服务器压力测试报告

测试日期：2026-11-01（Asia/Shanghai）  
目标地址：`http://118.195.132.28:5173/`  
测试入口：Web Nginx 反向代理后的 Gateway `/api/`  
测试工具：[`scripts/load_test.py`](../../../scripts/load_test.py)

## 测试环境

云主机为 4 vCPU、约 7.4 GiB 内存、50 GiB 系统盘。测试期间 Compose 中的 Gateway、Agent、Judge Worker、Judge Service、RabbitMQ、MySQL、Redis 和 Web 均为 healthy；测试结束时 Gateway `/healthz` 返回 200，`judge.task.go` 的 `messages`、`messages_ready` 和 `messages_unacknowledged` 均为 0。

Agent 容器配置为 `AGENT_RUNTIME_MODE=auto`、`AGENT_MAX_CONCURRENT_RUNS=8`。Judge Worker 配置为 `KRATOS_WORKER_CONCURRENCY=2`。Agent 实际选择的模型 Provider 未从运行配置查询确认，因此 SSE 数字只作为当前云环境的黑盒结果。

## 测试数据

云端原本没有题库数据。使用管理员 API 只创建了 1 道压测题和 1 组测试点：

- 题目：A+B，题目 ID `1`。
- 输入：`1 2`。
- 期望输出：`3`。
- Go 提交源码只输出 `1`，用于稳定地产生判题结果并观察异步队列。

没有迁移本地其他题目、用户或业务数据。压测程序为各个虚拟用户创建临时账号；这些账号仍保留在云端数据库中，如需清理可按 `loadtest_` 前缀删除。

## 普通 API

请求为认证后的 `GET /api/v1/problems?page.page=1&page.page_size=20`，每档持续 2 分钟。

| 并发用户 | 请求数 | 吞吐 | 错误率 | P50 | P95 | P99 | 最大延迟 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 11,126 | 92.72 req/s | 0% | 106.39 ms | 115.94 ms | 133.26 ms | 234.91 ms |
| 30 | 32,800 | 273.33 req/s | 0% | 107.53 ms | 122.04 ms | 186.76 ms | 482.41 ms |
| 50 | 53,474 | 445.62 req/s | 0% | 109.26 ms | 128.88 ms | 171.00 ms | 1,118.84 ms |

在本次云主机配置和该只读 API 下，50 并发仍未出现 HTTP 错误；延迟中位数约 106–109 ms，P95 约 116–129 ms。

## Judge

每档持续 2 分钟，分别以 0.5、1、2 次提交/秒发送。提交接口响应成功后，程序轮询提交终态，并通过 RabbitMQ Management API 每秒记录 `judge.task.go` 的队列深度。每档最多等待 300 秒排空。

| 提交速率 | 提交请求 | 提交错误率 | 完成数 | 排空窗口结束仍待处理 | 提交 P50/P95 | 完成耗时 P50/P95 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0.5/s | 60 | 0% | 60 | 0 | 200.62 / 248.21 ms | 81.99 / 114.06 s |
| 1/s | 120 | 0% | 120 | 0 | 169.58 / 292.73 ms | 162.64 / 284.91 s |
| 2/s | 240 | 0% | 183 | 57 | 173.23 / 238.10 ms | 284.48 / 319.46 s |

观测到的 `judge.task.go` 队列最大深度分别约为 27、81、196。2/s 档结束后，压测程序的 300 秒排空窗口内仍有 57 条提交未进入终态；随后继续观察，云端队列恢复到 0，说明消息最终仍被 Worker 消费，但当前 Worker 吞吐低于 2 次提交/秒。提交 API 本身没有因为队列积压返回错误，异步延迟和积压是主要瓶颈。

## Agent SSE

每档持续 2 分钟，发送同一个短问题，持续读取 SSE 直到 `done`，记录首个 token 延迟（TTFT）、完成延迟、SSE token 事件数和错误原因。

| 并发 Run | 请求数 | 成功数 | 错误数 | 错误率 | 吞吐 | TTFT P50/P95 | 完成 P50/P95 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 847 | 847 | 0 | 0% | 7.06 req/s | 129.97 / 144.12 ms | 139.60 / 153.94 ms |
| 3 | 2,429 | 2,409 | 20 | 0.823% | 20.24 req/s | 133.58 / 158.67 ms | 144.40 / 173.51 ms |
| 5 | 3,650 | 3,588 | 62 | 1.699% | 30.42 req/s | 146.23 / 196.44 ms | 158.59 / 216.48 ms |

3 并发档包含 19 次 HTTP 503 和 1 次流提前结束；5 并发档包含 62 次 HTTP 503。成功流均收到终止事件。TTFT 随并发上升，5 并发的 P95 比单并发增加约 52 ms；错误率也随并发增加。

## 结论与限制

这次云端测试证明了：Gateway 普通读 API 在 50 并发下保持 0% 错误；Judge 提交入口具备异步接收能力，但单 Worker 配置在 2 次提交/秒下形成明显积压；Agent SSE 在 1 并发稳定，3 和 5 并发出现容量相关 503，需要结合 Agent 并发限制、模型 Provider 响应和容器资源继续定位。

本次 Judge 只使用一组极小测试点，结果不能代表复杂题、编译型大程序或多测试点题目的执行吞吐。本次 Agent 测试使用短消息，统计的是 SSE token 事件数量，不是 Provider 账单中的输入/输出 token 数。测试过程中没有修改云端服务配置、扩容 Worker 或开放 RabbitMQ 管理端口。

原始 JSON 结果：

- [`oj-cloud-api.json`](./oj-cloud-api.json)
- [`oj-cloud-judge.json`](./oj-cloud-judge.json)
- [`oj-cloud-agent.json`](./oj-cloud-agent.json)
