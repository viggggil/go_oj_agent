# 压力测试

`scripts/load_test.py` 是针对已经启动的本地 Compose 环境的黑盒压测程序，
只依赖 Python 标准库。它会为每个虚拟用户注册临时账号，测试结束后账号仍保留在
本地数据库；生产环境不要直接使用默认密码或默认地址。

先确认 Gateway、Judge Worker、RabbitMQ 和 Agent 已启动，并准备一个可判题的题目 ID：

```bash
make infra-up
curl http://127.0.0.1:8080/healthz
```

普通 API 每档 2 分钟：

```bash
python3 scripts/load_test.py api \
  --concurrencies 10,30,50 --duration 120 \
  --output load-test-api.json
```

Judge 每档 2 分钟，按 0.5、1、2 次提交/秒发送，结束后默认再排空 180 秒：

```bash
python3 scripts/load_test.py judge \
  --problem-id 1 --rates 0.5,1,2 --duration 120 --drain-timeout 180 \
  --output load-test-judge.json
```

脚本会轮询每个提交直到终态，并从 RabbitMQ Management API 记录
`judge.task.<language>`、`judge.results` 和 `judge.dlq` 的 ready/unacked 深度。
如果 RabbitMQ Management API 不可用，可以加 `--no-rabbit`，提交和排空测试仍会运行。

Agent SSE 每档 2 分钟；结果包含 HTTP 错误率、并发运行数、首个 token 延迟（TTFT）、
完成延迟和 token 事件数。HTTP 401/503 会按错误原因单独统计，不会混入完成流的延迟分位数：

```bash
python3 scripts/load_test.py agent \
  --concurrencies 1,3,5 --duration 120 \
  --output load-test-agent.json
```

所有结果会同时打印摘要并写入 JSON，便于在不同版本间比较。默认题目源码是一个
输出 `1` 的 Go 程序；可以用 `--language python` 或 `--language cpp` 切换。

本地只验证压测工具本身：

```bash
python3 -m unittest discover -s scripts -p 'test_load_test.py'
```
