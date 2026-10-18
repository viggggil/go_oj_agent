# 本地开发 Docker Compose

本目录提供当前认证后端链路所需的最小本地环境：

```text
HTTP Client -> gateway-service -> gRPC -> user-service -> MySQL / Redis
                              -> gRPC -> problem-service -> MySQL / Redis / MinIO
```

MySQL 保存用户、角色、题目、标签和测试点元数据；Redis 保存 Refresh Token
及题目详情缓存；MinIO 的 `problem-data` bucket 保存测试点正文。当前环境
启动 user-service、problem-service、judge-service、contest-service、judge-worker、go-judge、
RabbitMQ、gateway-service 和 Web。Contest 消费判题结果并维护本地 MySQL 排行榜，Consul 默认关闭。
`make infra-up` 默认启动全部应用组件，包括 Agent Service，并开启 Gateway Agent 路由。
启动顺序为 MySQL 健康 → `agent-db-init` 准备数据库 → Agent 健康 → Gateway → Web。

Contest 消费配置位于 `configs/contest.yaml`：exchange=`oj.events`，queue=`contest.submission-judged`，
DLQ=`contest.results.dlq`，prefetch=16。RabbitMQ URL 由 `KRATOS_MESSAGING_URL` 注入，使用 Compose 的
RABBITMQ_USER/RABBITMQ_PASSWORD。MySQL 初始化挂载 Contest `000002_create_result_projection`；
已有数据卷须先显式执行该迁移，`make infra-up` 不会重新执行数据库初始化文件。

MinIO 和 `mc` 不再直接从 Quay 拉取。Compose 使用固定版本的官方 GitHub
Release 二进制构建本地镜像，并在 Docker build 阶段校验 SHA-256；这样 CI 不依赖
Quay 匿名拉取权限，也避免官方 Docker 镜像下线后导致集成环境无法启动。

## 启动

在仓库根目录执行：

```bash
make infra-up
```

服务默认地址：

| 服务 | 地址 | 用途 |
| --- | --- | --- |
| Gateway | `http://127.0.0.1:8080` | 外部 REST API |
| Web | `http://127.0.0.1:5173` | Vue 3 认证前端 |
| user-service | `127.0.0.1:9001` | 内部 gRPC，仅用于本地调试 |
| problem-service | `127.0.0.1:9002` | 内部 gRPC，仅用于本地调试 |
| MySQL | `127.0.0.1:3306` | `oj_user`、`oj_problem` 数据库 |
| Redis | `127.0.0.1:6379` | Refresh Token 和题目详情缓存 |
| MinIO | `http://127.0.0.1:9000` | 测试点对象存储 API |
| MinIO Console | `http://127.0.0.1:9003` | 本地对象存储管理界面 |

检查状态：

```bash
docker compose -f deploy/compose/compose.yaml ps
curl http://127.0.0.1:8080/healthz
```

浏览器访问 `http://127.0.0.1:5173` 可使用注册、登录和个人资料页面。Compose 构建时通过 `WEB_API_BASE_URL` 注入 Gateway 地址；如果修改了 `GATEWAY_HTTP_PORT`，应同步设置 `WEB_API_BASE_URL`，例如 `WEB_API_BASE_URL=http://127.0.0.1:18080 GATEWAY_HTTP_PORT=18080 make infra-up`。

停止服务但保留 MySQL、Redis 和 MinIO 数据：

```bash
make infra-down
```

已有开发数据卷的 Agent 升级由 `agent-db-init` 完成，不需要删除数据卷。
其他服务的历史迁移仍按对应部署说明执行。

### Agent Chat

Agent 默认随 `make infra-up` 启动，不需要设置 `COMPOSE_PROFILES` 或 `--profile agent`。
本地开发使用专用 `oj_agent` 数据库账户。默认 `AGENT_RUNTIME_MODE=auto`：
检测宿主 `~/.config/oj-agent/su8.key`，有 Key 使用真实模型，没有 Key 使用明确标记的 Fake。

```bash
chmod 600 ~/.config/oj-agent/su8.key
make infra-up
```

凭据目录可通过 `AGENT_MODEL_KEY_DIR` 覆盖。`agent-model-key-init` 只在初始化时读取宿主文件，
为 UID 10001 的 Agent 准备 600 文件；AES-GCM 主密钥保存在独立 `agent-model-keys` 卷中，
首次优先导入宿主 keyring.json 或旧默认 deploy/compose/model-keys/keyring.json，
已有卷重启复用，不能随意删除（否则已有数据库凭据无法解密）。API Key 不进入代码、镜像、环境变量或前端。

开发环境自动准备 su8 Provider（`https://www.su8.codes/v1`，Responses/Bearer，`store=false`）
及三个模型：`deepseek-v4-flash`、`deepseek-v4-pro`、`deepseek-v4.1-flash`。模型 verbosity 配置为
high；su8 当前不支持 `text.verbosity`，请求省略该字段，通过回答风格指令表达详尽程度。
模型参数采用本项目保守限制：32,000 上下文、8,000 输出 Token，不代表供应商公布的容量。

默认真实助手 key 为 `su8_learning_assistant`，演示助手仍为 `learning_assistant`，
可通过 `AGENT_DEFAULT_AGENT_KEY` 指定。旧 Fake 助手、已有资源版本和停用/归档状态完整保留；
准备步骤不会自动恢复资源，也不会撤销或替换已有凭据。已有 Provider 无效时仍启动管理服务，
由管理员通过 CLI 修复。替换宿主 Key 不会自动轮换已有数据库凭据；轮换继续使用
`app.credential_cli` 创建凭据、`app.config_cli` 创建 Provider/模型新版本并重绑 Agent。

管理员可访问 Agent / Prompt / Skill 管理页。新建只填写名称，内部 key 由后端生成；Skill
直接填写任务说明和简短描述，不再选择 Prompt 或重复配置预算。Agent 选择基础 Prompt、Skill、
默认 Skill 和模型；运行限制放在高级设置。测试 Agent 必须管理员可见并设置未来到期时间。
保存后点击“对话”，选择该 Agent/Skill 即可通过 Gateway SSE 调用真实模型。

没有 Key 时复用 `agent/examples/learning-assistant.json` 准备演示目录。显式设置
`AGENT_RUNTIME_MODE=langgraph_fake` 可强制演示；此模式只用于已有演示助手聊天，Agent 管理保存仍需 Responses 模型配置。
需要完全自行配置时设 `AGENT_BOOTSTRAP_DEMO=false` 和 `AGENT_BOOTSTRAP_SU8=false`。
旧环境文件中的 `AGENT_RUNTIME_MODE=langgraph_fake` 会覆盖 auto，使用真实模型需改为 auto/model；
`AGENT_ADMIN_ENABLED=false` 或 `AGENT_CONFIG_MODE=demo` 也会覆盖新默认值。

`AGENT_ENABLED=false` 可显式关闭 Gateway Agent 路由，组件仍正常启动。
若已有 `.env` 设置了这个旧默认值，应改为 `true`。Agent 只在 Compose 网络内监听 `8000`，
不映射宿主机端口；外部客户端始终通过 Gateway `8080` 访问 `POST /api/v1/agent/chat`。
Agent 容器只挂载 `agent-auth-public` 中的 Gateway 公钥，以及单独复制出的
`agent-auth-private` Agent 私钥卷；Gateway 私钥留在 `auth-keys`，不会进入 Agent 容器。
PR4 的业务 Tool 默认关闭。设置 `AGENT_BUSINESS_TOOLS_ENABLED=true` 后，Agent 使用该
独立私钥调用 Problem/Judge 的只读 gRPC RPC；目标服务通过 `agent-public.pem` 和 RPC
allowlist 验证调用。Agent 不会直接挂载业务数据库。
`run_code` 通过 judge-worker 内部接口复用 Go-Judge 沙箱，不会创建正式提交；它默认启用，管理员仍需将工具加入 Agent 和 Skill 的允许列表。
生产部署应将 `AGENT_RUN_CODE_TOKEN` 设置为至少 32 个字符的随机密钥；Compose 会把同一密钥只注入 Agent 与 judge-worker，接口不映射到宿主机。

`agent-db-init` 在 MySQL 健康后检查 Agent `000001`～`000005` 的表、增量列和约束，
按顺序应用尚未执行的迁移；完整的已执行迁移会跳过，保留已有会话、配置和凭据。
它复用 `migrations/agent/` 的原始 SQL，并幂等执行 `agent-user.sql` 创建仅有 `oj_agent`
表读写权限的账户，不覆盖已有账户密码。若发现迁移只完成了一部分，会明确失败并要求
保留数据、检查失败记录后修复，不盲目重跑 DDL。管理凭据只进入初始化容器。
`AGENT_DATABASE_URL` 可覆盖默认 DSN，但必须仍使用 `mysql+asyncmy` 和 `oj_agent` Schema；
外部数据库由部署者迁移，本地初始化容器只操作 Compose 中的 MySQL。

PR3/PR4 的 Fake runtime 只用于开发和集成测试；生产环境禁止 `AGENT_RUNTIME_MODE=fake`
或 `langgraph_fake`。只读 Tools 不需要真实模型 API Key；真实 Responses Provider、配置管理和
前端页面已提供。停止全部服务并保留数据时使用：

```bash
make infra-down
```

## 配置

Gateway Agent 路由、管理 API 和数据库配置目录默认开启。Agent 的 `000001`～`000005`
migration 在启动时自动准备。设置 `AGENT_ADMIN_ENABLED=false` 可单独关闭管理功能。
默认 `AGENT_ADMIN_PROVIDER_KEY=su8`；开发环境自动准备固定 Provider 和三个模型，
其他部署或凭据轮换按 `agent/README.md` 使用本机 CLI，HTTP 不接收 API Key。Chat 关闭时管理仍可独立使用；
普通聊天目录无需开启管理开关。现有开发 Fake 配置不会自动变成真实模型配置。

Compose 会自动读取仓库根目录或 `--env-file` 指定的环境文件。可用变量记录在 `deploy/compose/.env.example`。所有默认密码和 JWT 密钥仅供本机开发，不能用于共享、测试平台或生产环境。

`deploy/compose/configs/` 中的配置通过 Kratos 配置源使用 `${KEY}` 占位符读取容器环境变量。它们是 Compose 专用配置，不替代服务自身的本地默认配置。

使用指定环境文件：

```bash
docker compose \
  --env-file deploy/compose/.env.example \
  -f deploy/compose/compose.yaml \
  up --build --detach
```

`ACCESS_TOKEN_TTL=5s` 可用于后续真实过期集成测试。Gateway 和 user-service 必须使用相同的 `AUTH_ACCESS_TOKEN_KEY`、issuer 与 audience。

## 数据初始化

MySQL 官方镜像仅在数据目录为空时执行 `/docker-entrypoint-initdb.d`。Compose 按以下顺序挂载现有 migration：

1. 创建项目 schemas。
2. 创建 `oj_user.users`。
3. 创建 `oj_user.roles`。
4. 创建 `oj_user.user_roles`。
5. 写入 `user` 和 `admin` 默认角色。
6. 创建 `oj_problem.problems`、`tags`、`problem_tags` 和 `testcases`。

`minio-init` 在 MinIO 健康后幂等创建 `problem-data` bucket。

Compose 不复制另一套数据库结构，schema 的唯一来源仍是 `migrations/`。

MySQL 空数据卷初始化包含 Agent `000001`～`000005` 和开发账户 SQL。
`agent-db-init` 同时兼容这条已初始化路径，以及尚未包含 Agent 或只完成历史完整版本的旧数据卷。
Agent 使用独立公钥卷和 Gateway Chat 路由。不要为迁移已有数据库而删除当前开发数据卷。

## 排障

查看服务日志：

```bash
docker compose -f deploy/compose/compose.yaml logs user-service gateway-service
```

Agent 启动失败时，先检查数据库准备和应用日志：

```bash
docker compose -f deploy/compose/compose.yaml logs agent-db-init agent-service
```

若报 `failed to fetch oauth token` 或 Docker Hub 连接超时，Docker Desktop 的代理与
WSL 中 Buildx 的代理都需要可用。在当前 WSL 终端设置 `HTTP_PROXY` / `HTTPS_PROXY` 后
再执行 `make infra-up`；代理地址应使用本机代理软件实际监听的 HTTP/mixed 端口。

若 MinIO 镜像构建失败，请检查 runner 是否能访问 `github.com` 和公开的
`alpine:3.23` 基础镜像；构建阶段会下载并校验固定版本的 MinIO server 与 `mc`。

若宿主机端口已被占用，可通过环境变量覆盖，例如：

```bash
GATEWAY_HTTP_PORT=18080 MYSQL_PORT=13306 make infra-up
```


## RAG PR1 组件与语料

默认 `make infra-up` 同时启动 `qdrant`、`embedding`、`rag-worker`，无 profile。
Qdrant 使用独立持久卷 `qdrant-data`，主机端口默认仅绑定 127.0.0.1:6333；Embedding 和 worker 不开放主机端口。
Embedding 构建阶段安装 CPU PyTorch 并从 Hugging Face 下载固定 revision 的 BGE-small-zh-v1.5，
首次构建需联网和数百 MiB 下载空间，随后使用镜像缓存。构建失败可重跑 `docker compose -f deploy/compose/compose.yaml build embedding`；
构建日志会指出依赖或模型下载失败。运行阶段完全离线，不会因未缓存模型静默下载。共享/离线部署提前在联网机器构建并传输镜像。

Embedding 限制 2 CPU / 2 GiB，默认限制底层计算 2 线程，模型健康后 worker 启动。
模型就绪健康返回 revision 和 dimensions；Qdrant 检查 /readyz；worker 查询 MySQL 持久任务。
Agent 不因索引依赖暂时故障停止已有聊天，管理检索返回明确 503、索引任务有限重试后显示 FAILED。
生产部署应配置独立 bucket 凭据及服务网络隔离，勿使用开发默认 MinIO root 凭据。

新增环境项：`AGENT_RAG_ENABLED`、`AGENT_RAG_EMBEDDING_URL`、`AGENT_RAG_QDRANT_URL`、
`AGENT_RAG_MINIO_ENDPOINT/ACCESS_KEY/SECRET_KEY/BUCKET/SECURE`。Compose 默认 bucket 为 `agent-knowledge`。
旧数据卷由 `agent-db-init` 自动增加 000004 的五张表，保留题目、聊天及凭据。

```bash
docker compose -f deploy/compose/compose.yaml ps
docker compose -f deploy/compose/compose.yaml logs --tail=80 embedding rag-worker
# 查询索引任务状态使用管理员文档列表/详情 API，FAILED 后 POST documents/{id}/retry。
# 孤立对象先预览；仅使用知识库 UUID，不是管理员名称。
docker compose -f deploy/compose/compose.yaml exec rag-worker python -m app.rag.gc BASE_UUID
# 确认预览后添加 --apply；每次最多 500 对象，可按输出 start-after 继续。
```

50 题及题解通过 `scripts/import_algorithm_corpus.py` 显式导入；不在启动时自动写入业务题库。
管理员令牌文件和导入/判题报告不提交仓库。操作说明见 `data/algorithm-corpus/README.md`。
PR1 未提供管理页面和聊天检索工具，分别由后续 PR2 / PR3 完成。

## RAG PR3 升级

先部署新增 Contest CheckKnowledgeAccess RPC 和 Problem/Contest 的 Agent 调用白名单，再部署 Agent。现有容器不会因绑定挂载配置文件内容改变自动重启，更新 configs/problem.yaml、configs/contest.yaml 后执行 `docker compose -f deploy/compose/compose.yaml restart problem-service contest-service` 加载白名单。干净启动由 make infra-up 完成。Agent 地址为 AGENT_CONTEST_SERVICE_ENDPOINT=contest-service:9005；不授予 Agent 其他 Contest RPC，不向公网增加授权入口。旧业务服务未升级时检索失败关闭，不能以普通管理搜索绕过。

管理员启用题解文档并在 Agent 表单绑定“题解”组后，可以正常聊天，由模型按需检索；RAG 页支持按 Run 查询来源版本。未关联题号的资料按分组授权使用，题解应填写真实题号，以便执行比赛与题目检查。现有 Agent/Contest 数据库迁移须按原说明先应用，PR3 没有新表/列。
