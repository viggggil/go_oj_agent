# 统一 ReAct：真实前后 Eval 对比（2026-10-17）

同一份 24 条算法知识/RAG 数据集，改动前后分别真实调用 Agent，再使用相同评审提示词、Skill、JSON schema 和 deepseek-v4-pro 评分。全样本均分从 2.058/4 变为 3.467/4；运行成功从 13/24 变为 21/24。

## 固定条件

- 数据集：算法知识与RAG综合评测 v1，ID `59a2674f-49d9-40c8-850c-0700bfed8295`。
- 内容哈希：`df2f23bd07be2a1fd4352b53bd00687c5f5c29247ddf5448dcc1b14b12ca81a9`。
- 基线 Run：`ff3c85f6-9b78-476d-966e-134c9eb4ddcf`；旧 Runtime 基于提交 `9e8e3e95dbf37aabfd2076e365f29b61f7d77ece`。
- 后测 Run：`6eb79b9c-060f-4994-b8c6-50f33282244d`；被测 Runtime 为 `react_v2`（PR #181）。
- 被测 Agent：su8_learning_assistant，模型 deepseek-v4.1-flash，student_a 为同一普通用户；每侧 repeat=1、concurrency=1。
- Agent、Prompt、Skill、Provider、模型配置版本、知识分组及文档版本、预算逐字段比对全部一致。Runtime 版本和派生 config_hash 的变化属于本次实现差异。
- 共同知识范围为“题解”和“算法知识”。在正式基线之前已绑定两组，之后未变更。
- 目标预算：120 秒、8,000 字符、1,000 事件、8 次工具/模型调用、32,000 输入及 8,000 输出 token；另受相同模型配置约束。
- 不重跑失败目标，不筛选较好回答。两侧原始回答冻结后评分；成功评分后不再重评取高分。

## 评分口径与覆盖

五个维度均为 0–4 整数，逐例平均后再对样本等权平均。全样本分数将目标运行失败计 0；评审不可用单列，不伪装成评分为 0。成功样本均分会受样本组成影响，因此额外比较两侧都成功的相同案例。

旧内置 Reviewer 的 2,000 输出 token/事件预算无法稳定生成 JSON；正式后测也保留了运行开始时固定的 Reviewer 快照，仍有事件超限。因此下表的质量分数均来自统一离线评分，而非混用两侧页面的内置评分。离线评审不调用目标 Agent、工具或知识库，使用相同原有 Reviewer Prompt/Skill/输入投影、deepseek-v4-pro、max_output_tokens=8000。该流程不设原先过小的 Provider 事件计数限制，仍受 120 秒网络超时及 8,000 字符 JSON 限制。

有效质量评分覆盖：基线 13/13 条成功回答，后测 21/21 条成功回答。运行错误单独保留。正式基线原内置评审 13 条不可用；后测原内置评审 2 条完成、19 条不可用。这些历史状态不覆盖，对比质量分统一取本报告及结构化结果。

## 总体结果

| 指标 | 改动前 | 改动后 |
| --- | ---: | ---: |
| 运行成功 / 程序化通过 | 13/24 / 13/24 | 21/24 / 21/24 |
| 全样本均分（失败计 0） | 2.058 | 3.467 |
| 成功样本均分 | 3.800（13 条） | 3.962（21 条） |
| 相同成功案例配对均分 | 3.800（13 条） | 3.954（13 条） |
| 模型评审通过数 / 全部样本 | 12/24 | 21/24 |
| 模型评审 hard_fail | 1 | 0 |
| 目标平均耗时（含失败，不含评审） | 16.55 秒 | 23.26 秒 |
| 目标模型调用总数 | 50 | 56 |
| 检索成功 / 检索调用 | 45/45 | 48/48 |

## 分维度评分

| 维度 | 全样本：前 | 全样本：后 | 配对案例：前 | 配对案例：后 |
| --- | ---: | ---: | ---: | ---: |
| 任务完成度 | 2.125 | 3.500 | 3.923 | 4.000 |
| 技术正确性 | 2.083 | 3.500 | 3.846 | 4.000 |
| 证据支撑 | 1.875 | 3.333 | 3.462 | 3.769 |
| 指令遵循 | 2.125 | 3.500 | 3.923 | 4.000 |
| 表达清晰度 | 2.083 | 3.500 | 3.846 | 4.000 |

## 失败与逐例结果

- 基线运行错误：`{"AGENT_MODEL_INPUT_LIMIT": 8, "AGENT_MODEL_OUTPUT_LIMIT": 3}`。
- 后测运行错误：`{"AGENT_MODEL_OUTPUT_LIMIT": 2, "AGENT_MODEL_INPUT_LIMIT": 1}`。
- 旧摘要只记录去重工具名，无法还原所有真实工具调用次数；检索次数可由两侧已有 knowledge 数组直接比较。新摘要额外保存有归属的 tool_steps，不对旧运行反推轨迹。

| 案例 | 改动前状态 / 均分 | 改动后状态 / 均分 | 差值 |
| --- | --- | --- | ---: |
| algo_bfs_weighted | COMPLETED / 3.80 | COMPLETED / 4.00 | +0.20 |
| algo_binary_duplicates | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_difference_range | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_greedy_coin | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_hint_only | COMPLETED / 3.60 | COMPLETED / 3.80 | +0.20 |
| algo_injection_user | COMPLETED / 3.80 | COMPLETED / 4.00 | +0.20 |
| algo_knapsack_exact | COMPLETED / 3.40 | COMPLETED / 4.00 | +0.60 |
| algo_multiturn_dp | COMPLETED / 3.20 | COMPLETED / 4.00 | +0.80 |
| algo_no_business_access | AGENT_MODEL_OUTPUT_LIMIT / 0 | AGENT_MODEL_OUTPUT_LIMIT / 0 | +0.00 |
| algo_rag_bfs | AGENT_MODEL_OUTPUT_LIMIT / 0 | COMPLETED / 3.80 | +3.80 |
| algo_rag_binary_search | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_rag_dijkstra | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_rag_dynamic_programming | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_rag_fenwick_tree | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_rag_greedy | AGENT_MODEL_INPUT_LIMIT / 0 | AGENT_MODEL_INPUT_LIMIT / 0 | +0.00 |
| algo_rag_kmp | COMPLETED / 3.60 | COMPLETED / 3.80 | +0.20 |
| algo_rag_monotonic_structures | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_rag_prefix_sum | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_rag_sliding_window | AGENT_MODEL_INPUT_LIMIT / 0 | AGENT_MODEL_OUTPUT_LIMIT / 0 | +0.00 |
| algo_rag_topological_sort | COMPLETED / 4.00 | COMPLETED / 4.00 | +0.00 |
| algo_rag_union_find | AGENT_MODEL_OUTPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_stack_equal | COMPLETED / 4.00 | COMPLETED / 3.80 | -0.20 |
| algo_topo_multiple | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |
| algo_window_abba | AGENT_MODEL_INPUT_LIMIT / 0 | COMPLETED / 4.00 | +4.00 |

## 典型变化与剩余问题

- 多轮 0/1 背包：从 3.20 提高到 4.00。新回答保留“单件物品最多一次”，没有旧回答在“恰好装满容量 4”说明中的自相矛盾。
- 贪心找零、动态规划、单调结构、前缀和、并查集等原预算失败案例恢复为成功回答；其中 RAG BFS 得分 3.80，其余新增成功案例为 4.00。
- 单调栈相等元素：从 4.00 降到 3.80。算法结论正确，但评审仅看到标题/章节，无法核实“资料恰好以 [2,2,3] 为例”的引用，证据维度降为 3。
- 仍失败的三条：algo_no_business_access、algo_rag_sliding_window 为输出限制；algo_rag_greedy 为输入限制，形成四次检索后，下一轮请求的保守估算超过累计输入预算。未提高被测预算或重新抽样替换这些结果。

## 解释与限制

预算修复减少了 UTF-8 字节上界被误作实际 token 累计消耗的情况。请求前仍使用保守估算，Provider 可信 usage 返回后按实际消耗累计，缺失 usage 时继续保守处理。这是本轮稳定性变化的重要原因；本次还同时包含图状态、续接、重复调用保护和公开进度改动，没有做消融实验，不能把所有变化归因于某一个节点。

这是每侧单次抽样，模型及中转站输出存在波动。均分变化仅描述本次测量，不代表统计显著改进。模型评审读取的证据是脱敏标题/章节等投影，不包含完整私有知识正文，引用真实性可能被误判；hard_fail 标签应结合具体 reason 人工复核，不能直接等同安全漏洞。

成功率、全样本分数、配对分数和耗时应一起看。更多案例完成会增加后续模型调用和耗时，不能只用全样本均分宣称算法能力提升。后测部分已冻结回答的离线评分与剩余目标案例并发运行，两侧目标 concurrency 仍为 1，但 Provider 负载并非严格一致；耗时只作本轮观察，不作性能提升或回退的因果结论。原始答案与评分保存在本机 /tmp/oj-eval-baseline、/tmp/oj-eval-after；提交的 [逐例结构化结果](react-eval-comparison.json) 包含答案哈希、状态、分数、评审理由和调用指标，未包含凭据、原始推理或 Provider 续接数据。

## 验证与代码审查

Python 静态检查与 193 项非集成、35 项基础集成、17 项真实模型协议集成通过；Gateway 全包测试通过；Web 11 项测试、格式/构建、15 项管理、5 项聊天浏览器回归及 2 项真实 Gateway/数据库浏览器闭环通过。独立审查发现并修复失败历史轨迹丢失、Eval 多轮轨迹遗漏与 Reviewer 部署预算夹限，复核结论 APPROVE。详见 [重构说明](react-refactor.md)。
