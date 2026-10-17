# 统一 ReAct 工作流与前后评测

## 验收范围

真实模型只运行一个 ReAct Graph，支持零工具回答和动态多轮只读工具调用；不再提供 Direct/ReAct 配置选择。新增公开进度、可选 Provider 摘要和脱敏工具时间线，刷新历史对话可恢复工具摘要。保持身份、权限、预算、取消、背压，以及最终答案事务成功后才发送 done 的约束。使用现有 24 条算法知识与 RAG 测试集，对重构前后的回答分别进行相同模型评审。

## 改动前代码评审

1. `model_runtime.py` 由 execution_mode 决定是否创建 ToolSession，存在真实 Direct/ReAct 分支。配置、目录查询、Skill 表单及 Reviewer 都依赖此字段。
2. 图只有 prepare/check_budget/generate/tools/validate_answer，模型轮次、调用 ID、预算和 Provider 输入隐式积累在 ToolSession。
3. 工具目录与 ToolExecutor 已有两层授权，执行时继续使用可信 Principal 和业务 gRPC；这些边界应保留。
4. tools 摘要去重，无法表示同一工具多次调用、耗时、失败或拒绝。SSE 只有 thinking/token/done/error。
5. 工具调用轮次在 Provider 完成前可能先产生文本 delta；若直接发送，后续才发现混合文本和 Function Call 时，用户已经看到未确认的回答。
6. token 预算将 UTF-8 字节上界与 Provider 实际 usage 取较大值，中文正文与多轮检索容易被误判超限。应在请求前保守估算，在返回可信 usage 后按实际消耗累计；缺少 usage 时保持保守估算。
7. 真实基线发现评审模型原 2,000 输出 token 可能在公开评分 JSON 产生前耗尽，Provider 推理 delta 也可能超过原 1,000 事件上限。评审失败必须单列，不应计为模型给出的零分。

## 实现顺序与状态所有权

1. 保留运行旧镜像，固定被测配置、知识库版本、普通用户身份和数据集，保存完整基线回答。
2. Agent 新增 ReActState；显式节点为 prepare → check_budget → reason → validate_action → act → observe → check_budget，Finish 经过 validate_answer → finalize。异常、取消和耗尽由现有 RunService 收敛到失败/取消终态。
3. Provider 的加密续接数据和工具结果输入仅保存在请求局部 ReActContext；不启用 checkpoint，不进入审计与 SSE。禁止 LangSmith 外传。
4. ToolExecutor 继续负责真实执行。整批动作先校验权限、JSON、Schema、重复调用及预算；失败观察可进入下一轮，成功观察才记为证据。保存到现有 ModelSummary JSON 的 tool_steps 只包含安全元数据，无数据库 DDL。
5. Python → Gateway → Web 同步扩展 SSE。保留 thinking 的读取兼容。进度、摘要和工具事件不参与正文拼接。新字段由两端严格校验，call_id 关联、单调序号及终态约束继续生效。
6. 新 Skill 管理写入不再接受 execution_mode。历史不可变配置、运行与 Eval 快照仍可解析；不改写旧版本或旧哈希。新 Run 标记 react_v2，统一使用 ReAct Graph。Reviewer 继续无工具、无知识、单模型决策和严格 JSON。
7. 运行必要测试、构建与独立审查，再部署新镜像，复测同一数据集并对比逐 Case 结果。

## 故障与兼容

- 越权、未知工具、坏参数与重复调用在执行前终止，不能依靠模型指令授权。
- 工具失败进入失败 Observation，不伪造成功 Evidence；真实错误只保存稳定错误码。
- Provider 不支持官方公开摘要时不伪造摘要；仍显示真实工作流进度与工具事件。
- 有工具时缓冲每轮文本，确认 Finish 后输出；无工具时唯一合法决策为 Finish，保留实时 token 流和背压。错误/取消后不会持久化成功答案。
- Gateway/前端先升级到接受新事件，再切换 Agent 镜像；旧 Agent 的 thinking 仍可读取。
- 历史消息接口仅在会话归属校验后返回安全工具字段，不返回原始参数、结果或证据内部标识。

## 测试与比较方法

复用现有 Provider mock、权限与取消回归，只增加关键的多轮、重复/坏调用、混合文本隔离、Provider 摘要和 SSE 时间线契约覆盖。运行 Python 检查与非集成测试、Agent MySQL 集成测试、Gateway 测试、Web 测试与构建。

数据集固定为“算法知识与RAG综合评测 v1”（24 条，内容哈希 df2f23bd07be2a1fd4352b53bd00687c5f5c29247ddf5448dcc1b14b12ca81a9）。被测 Agent 固定为 su8_learning_assistant，模型不变，绑定同一“题解”和“算法知识”组，student_a 固定为同一普通用户。每侧 repeat=1、concurrency=1。保存答案后，使用同一个 deepseek-v4-pro、原有评审 Prompt/Skill/JSON schema 和统一的 8,000 token 上限分别评分；评分期间不重跑被测回答、不授予评审工具或知识库权限。原 2,000 上限导致的 UNAVAILABLE 保留在原始运行中，不伪装成有效评分。

同时报告程序化通过数、硬失败/运行错误、评审覆盖数、五个维度均分、全样本分数（运行失败记 0，评审不可用单列）、双方均成功样本的配对分数、检索与工具次数、耗时。一次抽样可说明本轮现象，不能证明统计显著提升。

真实运行数据与逐题评分保存在本机 /tmp/oj-eval-baseline 和 /tmp/oj-eval-after，报告只引用脱敏指标，不提交 Token、API key、原始凭据或内部续接数据。

Compose 默认 Reviewer 使用 8,000 输出 token、10,000 事件；部署事件天花板同步为 10,000。普通学习 Agent 自身的 1,000 事件预算仍取最小值生效。只自动升级审计确认未被管理员修改的 seed，生成新版本而不修改旧数据。本轮正式后测已固定旧 Reviewer 事件快照，因此对比分数统一取保存回答的离线评分；部署修复另行用真实评审验证。

## 改动后独立审查

独立审查先发现两项 Medium 问题：失败 Run 没有成功 assistant 消息时，刷新页面会丢失工具轨迹；EvalRunner 聚合多轮结果时遗漏 tool_steps。已分别修复为失败 user 记录返回白名单轨迹并由页面挂到失败回答上、按有界列表聚合且记录可信 run_id；新增真实 MySQL/Gateway 回归验证失败与非 owner 场景，以及跨两轮重复 call_id 的归属。

真实评测又发现 Reviewer 推理事件可超过预算。独立复核指出只提高 Agent 配置仍会被部署上限截断，已同步 Compose 天花板，并在集成测试读取实际 Reviewer Run 快照，断言有效 max_events=10000。最终独立审查结论为 APPROVE，未保留阻断项。

验证覆盖零工具、单工具与多轮、重复与非法调用、工具失败 Observation、混合正文隔离、官方摘要/opaque 续接、超时/取消/背压、SSE 错配与异常终态、历史权限与安全投影。Python 193 项非集成测试、35 项基础集成和 17 项真实模型协议集成通过；Gateway 全包、Web 11 项测试/构建以及管理 15 项、聊天 5 项及 2 项真实 Gateway/数据库浏览器闭环通过。独立 RAG 索引集成套件未重复执行；本次正式 Eval 直接使用运行中的 BGE/Qdrant 检索链路。
