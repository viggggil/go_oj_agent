# Agent Service

## 当前已实现：PR1 服务骨架

当前分支只交付可独立启动、可健康检查和可部署的 FastAPI 服务骨架。它还没有 Agent Runtime、Chat/SSE、Tool、RAG、会话表或真实模型调用；启动服务不需要模型 API Key。后续功能会在独立 PR 中逐步接入，下面第 1 节起的内容是目标设计，不能视为当前已上线能力。

本阶段提供两个健康端点：

| Endpoint | 成功 | 未就绪 | 作用 |
| --- | --- | --- | --- |
| `GET /healthz` | `200 {"service":"agent-service","status":"ok"}` | 不依赖数据库 | liveness，进程仍能处理请求 |
| `GET /readyz` | `200`，`checks.database=ready` | `503`，状态为 `not_configured` 或 `unavailable` | readiness，检查 Agent 自有 MySQL Schema |

readiness 只执行 `SELECT 1`，不会建表，也不会访问 User、Problem、Submission 或 Contest 的数据库。数据库 URL 必须使用 `mysql+asyncmy` 且 Schema 必须是 `oj_agent`；生产环境必须配置该 URL。配置错误会在启动时以字段名报告，响应和 JSON 日志不会输出 DSN、密码或驱动异常文本。数据库不可用时 liveness 仍保持正常，服务退出时会在有界超时内释放连接池。

本地开发：

```bash
cp agent/.env.example agent/.env
make agent-init
make agent-dev
```

没有数据库时可以访问 `http://127.0.0.1:8000/healthz`；`/readyz` 会返回 503，这是预期行为。常用检查命令为：

```bash
make agent-check       # ruff format 检查、ruff lint、mypy
make agent-test-unit   # 不依赖外部服务的测试
make agent-test-integration  # 独立 Docker Compose + MySQL + 容器健康检查
```

Docker 镜像的构建上下文是 `agent/`，运行用户为非 root 的 UID 10001，容器健康检查读取 `AGENT_PORT`（Compose 默认 8000）访问 `/healthz`。集成测试使用独立 Compose project、网络和数据卷，结束后自动清理，不复用开发环境。

PR1 的代码和测试目录如下：

```text
agent/
├── app/                 # 配置、结构化日志、数据库探针、健康路由、生命周期
├── tests/               # 单元测试和真实 MySQL/容器集成测试
├── Dockerfile
├── pyproject.toml
└── uv.lock
```

Agent Service 是面向编程学习场景的 Python 服务。它负责理解用户目标、选择受控 Tool、执行有限的 ReAct 或 Plan-and-Solve 流程、生成带证据的回答，并通过 SSE 向客户端返回进度。

以下章节描述完整 Agent 的目标设计，随着后续 PR 实现逐项更新状态。

提交诊断、算法解释、题目理解、分级 Hint、历史复盘、题目推荐和学习计划运行在同一个 Agent Runtime 上。它们通过 Skill 声明目标、可用 Tool、Prompt、输出结构和预算。Agent 不直接访问 User、Problem、Submission 或 Contest 数据库，所有业务数据都通过 Python gRPC Client 调用拥有数据的 Go Service，并由目标 Go Service 执行最终授权。

## 1. 设计目标

- 用户可以询问算法、题目、提交、历史记录和学习计划。
- Agent 根据目标选择必要的 Tool，不固定预取所有上下文。
- 管理员可以在前端有限调整 Prompt、Skill、Tool 可用性和知识库。
- 运行时配置不写死在 Python 代码中，但 Tool 的实现、输入输出契约和安全边界由代码控制。
- Prompt、题面、源码、编译输出和 RAG 文档都视为不可信数据。
- 每次运行追溯 Prompt、Skill、Tool Catalog、模型和知识库索引版本。
- 通过离线 Eval、在线指标、Trace 和人工抽检评估回答质量。

## 2. 运行时分层

~~~text
HTTP / SSE
  → API Layer
  → Session Resolver
  → Intent + Entity Parser
  → Policy Resolver
  → Published Prompt / Skill / Tool Config
  → Agent Runtime
       ├─ Direct Answer
       ├─ ReAct Tool Loop
       └─ Plan-and-Solve
  → Tool Executor
  → Typed gRPC Clients / RAG
  → Evidence / Safety / Quality Check
  → SSE Stream + Persistence
~~~

Context Resolver 只提取 submission_id、problem_id、语言、时间范围等实体，不因识别到实体就自动读取资源。是否调用 get_submission 或 get_problem 由 Agent 在允许的 Tool 集合中决定。

运行时分为 API Layer、Agent Runtime、Tool Executor 和 Control Plane。Control Plane 管理已发布的 Prompt、Skill、Tool 策略、知识库和 Eval 配置。

## 3. 目录结构

~~~text
agent/
├── app/
│   ├── api/
│   │   ├── chat.py
│   │   ├── admin_prompts.py
│   │   ├── admin_tools.py
│   │   ├── admin_skills.py
│   │   ├── admin_knowledge.py
│   │   ├── admin_evals.py
│   │   └── schemas.py
│   ├── agents/
│   │   ├── orchestrator.py
│   │   ├── intent.py
│   │   ├── policy.py
│   │   └── response.py
│   ├── graphs/
│   │   ├── runtime.py
│   │   ├── react.py
│   │   ├── planner.py
│   │   ├── reflection.py
│   │   └── nodes.py
│   ├── tools/             # Registry、Executor、各业务 Tool
│   ├── clients/           # User / Problem / Judge gRPC Client
│   ├── rag/               # 摄取、检索、引用和可见性过滤
│   ├── control_plane/     # 配置版本、发布、校验、审计
│   ├── evals/             # 数据集、Runner、Graders
│   ├── models/            # State、Event、Persistence
│   └── core/              # Config、Budget、Trace、脱敏
├── evals/
└── tests/
~~~

代码中的 Registry 只注册 Tool 实现和不可绕过的安全元数据。Prompt 文本、Skill 的 Tool 绑定、模型参数、预算和知识库内容从已发布配置读取。

## 4. Tool 设计

每个 Tool 使用 typed input/output，并声明：

~~~text
ToolSpec:
  name, description, input_schema, output_schema, rpc_method
  read_scope: public / current_user / admin
  allowed_roles
  side_effect: read / write
  sensitivity: public / private / source / internal
  timeout_seconds, cost, requires_confirmation
~~~

第一批只读 Tool：

~~~text
get_current_user
get_problem
list_problems
get_submission
get_submission_source
get_judge_result
list_submissions
retrieve_knowledge
~~~

后续 Tool：

~~~text
get_learning_profile
search_problems
recommend_problem
create_submission
rejudge_submission
~~~

create_submission 和 rejudge_submission 默认关闭。recommend_problem 可以先由只读 Tool 组合完成。

Tool Executor 在执行前检查 Tool allowlist、用户角色、资源范围、JSON Schema、deadline、调用次数、token 预算、写操作确认和重复参数。目标 Go Service 仍然是最终授权方。

统一 ToolResult 包含：

~~~text
tool_name, ok, data, error_code, source, fetched_at
evidence[], redacted_fields[]
~~~

## 5. Skill 设计

Skill 是可配置的任务能力，不是可执行任意 Python 的插件。示例：

~~~yaml
key: problem_hint
name: 题目分级提示
execution_mode: react
allowed_tools:
  - get_problem
  - retrieve_knowledge
prompt_key: skill.problem_hint
output_schema: HintAnswer
max_tool_calls: 4
max_steps: 6
requires_confirmation: false
~~~

管理员可以编辑描述、Prompt 版本、Tool allowlist、预算、输出结构和启用状态，但不能编辑 rpc_method、目标服务、权限校验实现或 Python 代码。

运行时解析顺序：

~~~text
请求上下文
→ 可用 Skill
→ Skill 已发布版本
→ Tool Catalog 已发布策略
→ Prompt 已发布版本
→ 模型和预算配置
~~~

一次 Agent Run 开始时固定配置快照，不在回答中途切换配置。首批 Skill：

~~~text
general_coding_question
algorithm_explain
problem_understanding
progressive_hint
submission_diagnosis
history_review
problem_recommendation
learning_plan
~~~

意图不明确或缺少计划所需的时间、语言、目标等信息时，先向用户提问。

## 6. Prompt 和管理员控制

Prompt 由不可编辑的安全前缀和可管理模板组成：

~~~text
安全前缀（代码固定）
→ 身份、权限和不可信数据说明
→ 已发布 Skill Prompt
→ 用户问题
→ Tool Observation 和证据
~~~

管理员可以创建草稿、编辑变量和内容、查看 Diff、运行样例 Eval、发布、回滚、归档和查看审计。不能通过 Prompt 授予新 RPC 权限、绕过业务授权、开启写 Tool、删除安全前缀或注入系统命令。

发布前检查变量白名单、长度、敏感信息、Tool 名称和禁止指令。生产运行只读取 published 版本，草稿不影响在线请求。

## 7. RAG 和知识库

原始文档存 MinIO，切分和索引结果存向量库。管理员页面支持文档上传、元数据编辑、标签、版本、切分预览、索引、发布、归档、回滚和检索测试。

知识库内容不能修改安全策略或 Tool 权限。检索结果携带文档版本和引用位置，回答区分知识库说明与本次业务数据确认。

## 8. Agent Runtime 和状态

~~~text
user_id, conversation_id, run_id, user_message
intent, goal, entities, constraints, skill_key, allowed_tools
config_snapshot, plan, observations, evidence, missing_information
tool_call_count, remaining_tokens, deadline, reflection_count
answer_draft, final_answer
~~~

执行模式包括 direct、react、plan_execute 和 reflection。所有模式都有 deadline、最大 Tool 次数、最大计划步数和最大 Reflection 次数。

## 9. SSE、观测和质量评估

用户侧事件：

~~~text
thinking, tool_call, tool_result, plan_update, token, warning, done, error
~~~

运行事件记录 run_id、conversation_id、user_id、trace_id、intent、Skill/Prompt/Tool Catalog 版本、模型、Tool 延迟和状态、Token 使用、答案状态和 Eval 标签。完整源码、凭据和原始敏感 Prompt 默认不直接发送浏览器，持久化按隐私策略脱敏。

Eval 分为：

- 确定性评估：意图、Skill、Tool、参数、权限、状态转换、预算和输出 Schema；
- 模型评估：事实正确性、相关性、解释完整性、Hint 等级、引用一致性和不确定性；
- 人工抽检：按 Skill、模型和配置版本抽样，失败样例回流数据集。

推荐指标：

~~~text
tool_selection_pass_rate
tool_argument_pass_rate
permission_denied_correct_rate
evidence_grounded_rate
citation_valid_rate
answer_relevance_score
answer_correctness_score
hint_level_accuracy
clarification_rate
unsafe_action_block_rate
fallback_rate
~~~

不以逐字匹配作为主要指标。线上观测和离线 Eval 使用同一套版本标识。

## 10. 管理员前端

管理员入口使用 system_admin 或明确授权的 agent_admin 角色，页面包括：

1. Prompt 管理：列表、版本、编辑、Diff、样例运行、发布、回滚和审计。
2. Tool Catalog：查看描述、输入输出 Schema、来源 RPC、敏感级别、允许角色、启用状态和调用统计。RPC 方法和最终授权代码只读展示。
3. Skill 管理：编辑元数据、选择允许的 Tool、绑定 Prompt、设置预算、样例运行、发布和回滚。
4. 知识库管理：文档上传、编辑、标签、版本、切分预览、索引、发布和归档。
5. Eval 与质量：运行数据集、查看通过率、工具选择错误、引用错误、拒答错误、版本对比和失败样例。
6. 运行观测：请求量、延迟、Token、Tool 错误、RAG 延迟、Skill 分布、Trace 和脱敏运行详情。

管理页面只能修改控制面允许的字段。发布、回滚、归档和启用写 Tool 需要二次确认并写审计日志，生产环境可以配置双人审批。

## 11. 数据和版本

Agent 自有 Schema 保存会话、Prompt、Skill、知识库元数据、Eval、运行事件和审计记录，不保存其他服务业务镜像：

~~~text
agent_conversations
agent_messages
agent_prompts
agent_prompt_versions
agent_skills
agent_skill_versions
agent_skill_tools
agent_knowledge_documents
agent_knowledge_versions
agent_eval_cases
agent_eval_runs
agent_eval_results
agent_runs
agent_run_events
agent_admin_audits
~~~

所有可编辑配置使用草稿和发布版本，发布版本不可变。一次 Agent Run 固定配置快照，回滚通过重新发布旧版本实现，不原地修改历史版本。

## 12. 开发顺序

1. 通过 ADR 确定控制面数据模型、版本状态、权限和 API；
2. 完成 FastAPI、SSE、会话、运行状态和 Tool Registry；
3. 接入只读 gRPC Tool 和 Agent Service 内部身份；
4. 实现 ReAct、Plan-and-Solve、Reflection 和预算控制；
5. 实现 Prompt、Skill、Tool Catalog、知识库和 Eval 管理 API；
6. 开发管理员页面、审计、发布和回滚；
7. 先上线算法解释、题目 Hint、提交诊断，再实现复盘、推荐和学习计划；
8. 接入离线 Eval、人工抽检、Trace、指标和告警；
9. 最后评估需要确认的写 Tool，普通学习流程默认不启用。

## 13. 安全不变量

- Agent、Prompt、Skill 和知识库不能替代目标 Go Service 的授权。
- 管理员页面不能修改 RPC 实现、服务身份或安全前缀。
- Prompt、题面、源码、编译输出和检索内容都是不可信输入。
- 运行时只使用已发布配置，草稿不会影响生产请求。
- 配置发布必须可审计、可回滚、可复现。
- 写 Tool 默认关闭，启用需要角色、确认和审计。
- Tool 次数、计划步数、Token 和时间必须有上限。
