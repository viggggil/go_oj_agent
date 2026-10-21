"""Build a 50-case Agent Eval dataset for real algorithm-bank scenarios."""

# Prompt/rubric strings intentionally preserve natural Chinese phrasing.
# ruff: noqa: E501

import json
from pathlib import Path

OUTPUT = Path(__file__).resolve().parents[1] / "data/algorithm-knowledge"
PRINCIPAL = "student_a"
cases: list[dict[str, object]] = []


def add_case(
    case_id: str,
    category: str,
    title: str,
    turns: list[tuple[str, str]],
    rubric: list[str],
    *,
    evidence: str | None = None,
    tools: tuple[str, ...] = ("retrieve_knowledge",),
    facts: tuple[str, ...] = (),
    forbidden_claims: tuple[str, ...] = (),
) -> None:
    expectations: dict[str, object] = {"rubric": rubric}
    if evidence:
        expectations["required_evidence"] = [evidence]
    if tools:
        expectations["required_tools"] = list(tools)
    if facts:
        expectations["required_facts"] = list(facts)
    if forbidden_claims:
        expectations["forbidden_claims"] = list(forbidden_claims)
    cases.append(
        {
            "id": case_id,
            "category": category,
            "title": title,
            "turns": [{"role": role, "content": content} for role, content in turns],
            "fixture": {"principal": PRINCIPAL},
            "expectations": expectations,
        }
    )


# Ask about exact problems in the imported 50-problem bank, with source-backed
# explanations, progressive hints, complexity, and boundary conditions.
problem_questions = [
    (
        "pair-sum-index",
        "两数目标和",
        "我想自己写两数目标和。先只给核心思路和一个容易写错的边界，不要直接贴完整代码。",
        [
            "先查补数再登记当前值，避免同一元素被使用两次。",
            "期望 O(n) 时间，重复值仍要用不同下标。",
        ],
    ),
    (
        "maximum-contiguous-sum",
        "最大连续子段",
        "请讲解最大连续子段和。输入全是负数时，为什么答案不能默认是 0？",
        ["求的是非空连续子数组。", "用当前元素或延伸前一段的较大值更新状态。"],
    ),
    (
        "longest-unique",
        "最长无重复子串",
        "最长无重复子串用滑动窗口时，遇到重复字符怎样移动左边界？用 abba 检查边界。",
        ["左边界更新为 max(原左边界, 上次位置加一)。", "abba 的答案长度是 2。"],
    ),
    (
        "binary-search",
        "二分查找首个目标",
        "题库的二分查找首个目标要求返回 1-based 首次位置。数组 [1,2,2,2,5] 查 2 和 4 分别返回什么？",
        ["查 2 返回 2。", "查 4 返回 -1。", "不能读越界的 a[n]。"],
    ),
    (
        "split-array",
        "最小化最大分段和",
        "最小化最大分段和为什么可以二分答案？请解释可行性判断和恰好分成 k 段。",
        [
            "容量越大越容易可行，具有单调性。",
            "贪心统计最少段数，小于等于 k 后可继续拆成恰好 k 段。",
        ],
    ),
    (
        "subarray-sum",
        "和为 K 的子数组数",
        "数组 [1,-1,1] 中和为 1 的非空子数组有几个？请解释前缀和计数为什么先查询再登记。",
        ["答案是 3。", "前缀和频次初值包含 0 一次。", "先查询再登记避免空区间。"],
    ),
    (
        "sliding-maximum",
        "滑动窗口最大值",
        "窗口最大值使用单调队列时，队列应该存值还是下标？解释重复值和过期元素怎么处理。",
        ["存下标。", "删除过期队首，删除被新值支配的队尾。", "每个元素至多入队和出队一次。"],
    ),
    (
        "coin-change",
        "最少硬币数",
        "最少硬币数是完全背包。硬币 [1,3,4] 凑 6，答案是什么？状态不可达应如何表示？",
        ["答案为 2。", "完全背包正序更新。", "不可达状态不能误当成 0。"],
    ),
    (
        "grid-shortest-steps",
        "网格中的最短步数",
        "题库网格最短步数里，起点和终点都可走但相同，答案是 0 还是 1？BFS 怎样避免重复入队？",
        ["答案是 0。", "距离按移动边数计。", "入队时标记访问。"],
    ),
    (
        "nonnegative-single-source-shortest-path",
        "非负权单源最短路",
        "题库的单源最短路允许零权边但不允许负权边。堆 Dijkstra 中为什么要跳过过期堆项？",
        [
            "零权边可以处理。",
            "弹出距离大于当前 dist 的记录时跳过。",
            "复杂度为 O((V+E) log V) 或更保守的等价表达。",
        ],
    ),
    (
        "dependency-topological-order",
        "动态图连通查询",
        "拓扑排序处理中有环时，为什么不能只检查 DFS 是否回边？用 Kahn 算法如何判环？",
        ["处理顶点数小于 n 表示有环。", "只剩下未处理的点不等于这些点都在环上。"],
    ),
    (
        "zero-one-capacity-pack",
        "有限容量装载",
        "0/1 背包每件物品只能用一次，为什么容量要逆序更新？如果改成正序会发生什么？",
        ["逆序防止同一物品在本轮重复使用。", "正序会变成完全背包语义。"],
    ),
]
for slug, title, prompt, rubric in problem_questions:
    add_case(
        f"bank_solve_{slug}",
        "bank_problem_solving",
        f"题库题目讲解：{title}",
        [("user", prompt)],
        rubric,
        evidence=title,
    )


# Verdict debugging scenarios use real bank problems and plausible faulty code.
# The verdict and code are supplied by the user; the Agent must not claim to have
# inspected an actual submission unless it calls the submission tools.
debug_cases = [
    (
        "wa_two_sum",
        "两数目标和",
        "WA",
        "n,t=map(int,input().split()); a=list(map(int,input().split())); d={}\nfor i,x in enumerate(a,1):\n    d[x]=i\n    if t-x in d: print(d[t-x],i); break",
        "重复值时先登记当前下标会把同一元素用两次；应先查补数再写入哈希表。",
        "同一元素不能重复使用",
    ),
    (
        "wa_longest_unique",
        "最长无重复子串",
        "WA",
        "s=input().strip(); last={}; left=0; ans=0\nfor r,c in enumerate(s):\n    if c in last: left=last[c]+1\n    last[c]=r; ans=max(ans,r-left+1)\nprint(ans)",
        "abba 会暴露左边界回退；更新必须取 max(left, last[c]+1)。",
        "左边界不能回退",
    ),
    (
        "wa_binary_search",
        "二分查找首个目标",
        "WA",
        "n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; r=n-1\nwhile l<=r:\n    m=(l+r)//2\n    if a[m]<=t: l=m+1\n    else: r=m-1\nprint(l+1 if r>=0 and a[r]==t else -1)",
        "该边界会落到最后一个小于等于 target 的位置，不是首个相等位置；应找 lower_bound。",
        "首个大于等于 target 的位置",
    ),
    (
        "wa_subarray_sum",
        "和为 K 的子数组数",
        "WA",
        "n,k=map(int,input().split()); a=list(map(int,input().split())); seen={}; s=ans=0\nfor x in a:\n    s+=x; seen[s]=seen.get(s,0)+1; ans+=seen.get(s-k,0)\nprint(ans)",
        "缺少空前缀 0 的初始频次，而且先登记当前前缀可能把空区间计入。",
        "初始前缀和 0 出现一次",
    ),
    (
        "tle_two_sum",
        "两数目标和",
        "TLE",
        "n,t=map(int,input().split()); a=list(map(int,input().split()))\nfor i in range(n):\n    for j in range(i+1,n):\n        if a[i]+a[j]==t: print(i+1,j+1); raise SystemExit",
        "双重循环是 O(n^2)，题目 n 可达 200000；改为哈希补数查询 O(n)。",
        "O(n) 时间",
    ),
    (
        "tle_split_array",
        "最小化最大分段和",
        "TLE",
        "# 当前尝试枚举所有切分点并计算每种分段\n# 数组长度最多 100000，k 也可能很大",
        "枚举切分组合不可行；对最大段和二分，再用线性贪心判断段数，复杂度 O(n log(sum(a)))。",
        "二分答案",
    ),
    (
        "tle_prime_count",
        "区间素数计数",
        "TLE",
        "n=int(input()); ans=0\nfor x in range(2,n+1):\n    prime=True\n    for d in range(2,x):\n        if x%d==0: prime=False; break\n    ans+=prime\nprint(ans)",
        "逐数试除到 x 的平方根仍太慢；使用埃氏筛从 p*p 标记倍数。",
        "埃氏筛",
    ),
    (
        "mle_grid_paths",
        "网格路径数",
        "MLE",
        "n,m=map(int,input().split()); a=[list(map(int,input().split())) for _ in range(n)]; dp=[[0]*m for _ in range(n)]\n# 逐格更新到达方案数",
        "二维 DP 占 O(nm) 内存；每行只依赖上一行和当前行左侧，可压成 O(m) 一维数组。",
        "O(m) 空间",
    ),
    (
        "mle_lcs",
        "最长公共子序列",
        "MLE",
        "a=input().strip(); b=input().strip(); dp=[[0]*(len(b)+1) for _ in range(len(a)+1)]\n# 标准二维 LCS 转移",
        "二维表占 O(nm) 空间；按较短字符串作为列，用一维 DP 保留左上角旧值，降到 O(min(n,m)) 空间。",
        "O(min(n,m)) 空间",
    ),
    (
        "mle_dijkstra",
        "非负权单源最短路",
        "MLE",
        "# 用 n*n 邻接矩阵存图，n=200000，边很少\ngraph=[[0]*n for _ in range(n)]",
        "稀疏图邻接矩阵需要 O(V^2) 空间；改为邻接表和最小堆，空间 O(V+E)。",
        "邻接表",
    ),
    (
        "re_tree_depth",
        "有根树最大深度",
        "RE",
        "def dfs(u,p):\n    return 1+max((dfs(v,u) for v in graph[u] if v!=p), default=0)\n# 输入允许一条长度 200000 的链",
        "递归深度会超过 Python 限制；使用显式栈或队列迭代遍历，不能只增大递归上限掩盖风险。",
        "迭代遍历",
    ),
    (
        "re_daily_temperatures",
        "等待更高温度",
        "RE",
        "while a[stack[-1]] <= x:\n    j=stack.pop()\n    answer[j]=i-j",
        "弹空后访问 stack[-1] 会触发 IndexError；循环条件必须先判断 stack 非空。",
        "while stack and",
    ),
]
for case_id, title, verdict, code, rubric, fact in debug_cases:
    prompt = (
        f"这是我在平台题库的「{title}」提交，判题结果是 {verdict}。下面是最小化后的 Python 代码/实现片段：\n"
        f"```python\n{code}\n```\n"
        "请指出最可能的具体错误或复杂度瓶颈，给出最小修正思路；说明修正后复杂度和一个能暴露问题的边界用例。"
    )
    add_case(
        f"verdict_fix_{case_id}",
        "verdict_debugging",
        f"{verdict} 修复：{title}",
        [("user", prompt)],
        [rubric, "不要声称读取了未提供的真实提交记录；诊断应基于用户贴出的代码和题库题解。"],
        evidence=title,
        facts=(fact,),
    )


# Ask for related questions in the actual bank. Retrieval is required so the
# response is grounded in available editorials, while the rubric rejects claims
# that every matching problem has been found when pagination is incomplete.
related_questions = [
    (
        "related_range",
        "单点加区间和",
        "我正在学区间查询，题库里有哪些题和树状数组相关？请区分点修改区间和与静态 RMQ，不要把所有区间问题都说成同一种数据结构。",
        "树状数组适合点增量和前缀/区间和；静态 RMQ 可用 ST 表。",
    ),
    (
        "related_shortest_path",
        "非负权单源最短路",
        "想练最短路，题库中 BFS、Dijkstra、Floyd 各对应什么题？按图权重和查询类型推荐，并提醒负边限制。",
        "无权图可用 BFS，非负权单源可用 Dijkstra，多源点对可用 Floyd；Dijkstra 不适用于一般负权边。",
    ),
    (
        "related_dp",
        "数字三角形",
        "请从题库推荐几道由浅入深的动态规划题，至少涉及线性 DP、背包和网格/三角形路径，并说每题练什么。",
        "推荐应来自题库已有题目，解释各自状态或转移差异。",
    ),
    (
        "related_strings",
        "字符串匹配位置",
        "题库里有哪些字符串题适合练哈希、Trie、KMP、编辑距离或 LCS？请按核心技能分组，别编造题目。",
        "推荐的题名应是题库中存在的题；KMP 解决模式匹配，Trie 适合前缀集合查询。",
    ),
    (
        "related_graph",
        "任务依赖排序",
        "我学完 BFS 后想继续图算法，题库有哪些图题可以练连通性、拓扑排序、最短路和生成树？",
        "应覆盖题库实际有的动态连通、拓扑、最短路或 MST 题型，并指出适用场景。",
    ),
    (
        "related_tree",
        "最近公共祖先",
        "请列出题库里的树相关练习，从遍历/树深度到树直径、最近公共祖先，说明进阶顺序。",
        "推荐必须使用题库真实题名；LCA 用于树上两点公共祖先查询。",
    ),
    (
        "related_monotonic",
        "等待更高温度",
        "想练单调结构，题库中哪些题分别适合练下一个更大元素、窗口最值和区间边界？解释栈与队列差别。",
        "等待更高温度使用单调栈，滑动窗口最大值使用单调队列。",
    ),
    (
        "related_binary_greedy",
        "最小化最大分段和",
        "我喜欢二分答案和贪心，题库里有哪些相近题？说明哪些是二分可行性，哪些是排序/交换贪心，避免只按题名猜。",
        "最小化最大分段和是二分答案；活动选择或区间合并是排序贪心类问题。",
    ),
]
for case_id, anchor, prompt, rubric in related_questions:
    add_case(
        f"related_bank_{case_id}",
        "bank_related_problems",
        f"题库相关题推荐：{case_id.removeprefix('related_')}",
        [("user", prompt)],
        [rubric, "只推荐经检索或题库列表确认的题目；如果无法确认完整列表，应明确说明范围有限。"],
        evidence=anchor,
    )


# Multi-turn tutor sessions exercise continuity: the model-generated assistant
# answer, not the fixture reference answer, becomes the next request's history.
learning_dialogues = [
    (
        "two_sum",
        "两数目标和",
        [
            "我想学习两数目标和。先问我一个小问题引导我想到哈希表，不要直接给完整代码。",
            "我觉得可以先把每个数放进表里，再查 target-x。这样对 [3,3]、target=6 会有什么问题？",
            "现在请基于刚才的讨论给出简短伪代码，并总结不变量、复杂度和重复值处理。",
        ],
        ["先查后存", "两个不同下标", "O(n)"],
    ),
    (
        "sliding_max",
        "滑动窗口最大值",
        [
            "我在学滑动窗口最大值，请先让我理解为什么需要双端队列，先给提示不要直接给代码。",
            "如果队尾下标对应的值比新元素小，新元素为什么可以把它删掉？",
            "再用窗口长度 3、数组 [1,3,-1,-3,5] 走一遍，并总结过期判断条件。",
        ],
        ["被新元素支配", "过期", "3 3 5"],
    ),
    (
        "binary_search",
        "二分查找首个目标",
        [
            "请带我学习 lower_bound 二分。先告诉我循环不变量，不要贴代码。",
            "数组 [2,2,2,4] 查 2 时，如果我在相等时令 l=mid+1，会得到什么错误？",
            "请给一个左闭右开写法的伪代码，并说明空数组为什么安全。",
        ],
        ["左闭右开", "首个大于等于", "空数组"],
    ),
    (
        "dp_knapsack",
        "有限容量装载",
        [
            "我刚开始学 0/1 背包。用一句话解释 dp[j] 的含义，再给一个小提示。",
            "有一件重量 2、价值 3 的物品，容量 4。为什么正序会得到 6？",
            "请对比 0/1 与完全背包的遍历方向，并给出各自的物品使用次数语义。",
        ],
        ["每件物品至多一次", "逆序", "完全背包正序"],
    ),
    (
        "bfs",
        "网格中的最短步数",
        [
            "请循序渐进教我用 BFS 求网格最短步数。先解释为什么按层扩展，不要完整代码。",
            "访问标记应该在节点入队还是出队时做？为什么？",
            "如果起点等于终点，距离是多少？请最后总结 BFS 的适用边权条件。",
        ],
        ["入队时标记", "距离为 0", "无权图"],
    ),
    (
        "prefix_sum",
        "和为 K 的子数组数",
        [
            "我想学用前缀和统计和为 K 的子数组。先问我应该保存什么历史信息。",
            "对于 [1,-1,1] 和 K=1，为什么滑动窗口不可靠？",
            "请总结哈希表初值、查询与更新顺序，并给出时间复杂度。",
        ],
        ["前缀和频次", "负数", "先查询再更新"],
    ),
    (
        "dijkstra",
        "非负权单源最短路",
        [
            "带我理解 Dijkstra。先解释贪心选择依赖什么条件。",
            "如果边权可以为 0 呢？如果出现一条负边又为什么不行？",
            "请给出堆优化时过期状态判断和复杂度。",
        ],
        ["非负边权", "零权边", "过期"],
    ),
    (
        "union_find",
        "动态图连通查询",
        [
            "我在学并查集，请先用连通分量解释 find 和 union，不要展开代码。",
            "路径压缩和按大小合并分别改善什么？",
            "请说明并查集不能直接解决的两个问题，并总结摊还复杂度。",
        ],
        ["路径压缩", "按大小合并", "不能支持在线删边"],
    ),
    (
        "topological",
        "任务依赖排序",
        [
            "请引导我理解 Kahn 拓扑排序，先讲入度队列代表什么。",
            "如果最后队列空了但还有顶点没有输出，说明什么？",
            "请总结环判定条件，并提醒未处理顶点不一定全在环上。",
        ],
        ["入度为 0", "处理顶点数小于 n", "未处理顶点不一定在环上"],
    ),
    (
        "fenwick",
        "单点加区间和",
        [
            "请用直观方式教我树状数组的 lowbit，先给我一个提示。",
            "更新位置 i 时，为什么循环加上 lowbit(i)？查询前缀时为什么减去它？",
            "请说明 1-based 下标要求、区间和公式和复杂度。",
        ],
        ["lowbit", "1-based", "O(log n)"],
    ),
]
for slug, problem_title, dialogue, facts in learning_dialogues:
    turns: list[tuple[str, str]] = []
    for index, prompt in enumerate(dialogue):
        if index:
            turns.append(
                ("assistant", "（上一轮由被测 Agent 实际生成，本参考内容不会注入运行历史）")
            )
        turns.append(("user", prompt))
    add_case(
        f"multi_turn_learn_{slug}",
        "multi_turn_learning",
        f"多轮学习：{problem_title}",
        turns,
        ["根据前文逐步加深解释，后续回答应承接用户刚才讨论的条件。"],
        evidence=problem_title,
        facts=tuple(facts),
    )


# Longer reasoning and code-review cases mix constraints, counterexamples,
# alternative algorithms, and explicit uncertainty handling.
complex_cases = [
    (
        "window_negative",
        "最短达标子数组",
        "我把正整数最短达标子数组的双指针代码直接用在 [1,-1,3]、target=3 上，结果漏掉了最短答案。请手算反例并推荐适用的算法。",
        ["答案长度为 1。", "负数会破坏窗口和的单调性。", "可考虑前缀和加单调队列。"],
    ),
    (
        "dijkstra_negative",
        "非负权单源最短路",
        "Dijkstra 跑完后我再扫描一次负边并松弛就能修复所有错误吗？请构造题库题型之外的最小反例思路，并推荐适合负边的算法。",
        ["一次补扫不能保证传播所有后续改进。", "可使用 Bellman-Ford；DAG 可拓扑序松弛。"],
    ),
    (
        "knapsack_exact",
        "有限容量装载",
        "把 0/1 背包改成必须恰好装满时，dp[0]=0、其他容量初始化为 0 有什么问题？如何区分不可达状态？",
        ["其他容量初始化为负无穷或不可达哨兵。", "必须避免从不可达状态转移。"],
    ),
    (
        "topology_lex",
        "任务依赖排序",
        "题目要求字典序最小拓扑序，我用普通 FIFO 队列。如何修改数据结构？复杂度会怎样变化？",
        ["使用最小堆维护当前入度为 0 的点。", "复杂度可写 O((V+E) log V)。"],
    ),
    (
        "fenwick_negative",
        "单点加区间和",
        "树状数组维护的值可能为负数。lowbit 与负值有关吗？区间 [l,r] 的和怎么由两个前缀得到？",
        ["lowbit 作用于正的下标，不要求数组值非负。", "区间和为 prefix(r)-prefix(l-1)。"],
    ),
    (
        "segment_lazy",
        "区间加与区间和",
        "区间加、区间求和有 2e5 个操作。每次更新都遍历区间会超时，应该用什么结构？懒标记要表达什么？",
        ["使用带懒标记的线段树。", "懒标记表示整段尚未下推的增量。", "操作复杂度 O(log n)。"],
    ),
    (
        "merge_touching",
        "合并重叠区间",
        "闭区间 [1,2] 和 [2,3] 在题库的区间合并规则下要合并吗？如果改成半开区间会有什么变化？",
        ["闭区间端点相接也合并。", "当前实现是闭区间语义，不能混用半开区间判断。"],
    ),
    (
        "kmp_overlap",
        "字符串匹配位置",
        "文本 aaaaaa、模式 aaa 有重叠匹配。KMP 找完一次后怎样继续找下一次？前缀函数回退到什么位置？",
        ["匹配后回退到模式最长真前后缀长度。", "不会丢失重叠匹配。"],
    ),
]
for case_id, title, prompt, rubric in complex_cases:
    add_case(
        f"complex_reasoning_{case_id}",
        "algorithm_reasoning_and_review",
        f"边界与算法选择：{title}",
        [("user", prompt)],
        rubric,
        evidence=title,
    )


if len(cases) != 50:
    raise RuntimeError(f"Expected exactly 50 cases, got {len(cases)}")
if len({str(item["id"]) for item in cases}) != len(cases):
    raise RuntimeError("Case IDs must be unique")
manifest_path = Path(__file__).resolve().parents[1] / "data/algorithm-corpus/manifest.json"
problem_titles = {item["title"] for item in json.loads(manifest_path.read_text(encoding="utf-8"))}
unmatched_evidence = sorted(
    {
        evidence
        for item in cases
        for evidence in item.get("expectations", {}).get("required_evidence", [])
        if not any(evidence in title for title in problem_titles)
    }
)
if unmatched_evidence:
    raise RuntimeError(f"Evidence titles are not in the problem manifest: {unmatched_evidence}")

OUTPUT.mkdir(parents=True, exist_ok=True)
jsonl_path = OUTPUT / "eval-complex.jsonl"
jsonl_path.write_text(
    "".join(json.dumps(item, ensure_ascii=False, separators=(",", ":")) + "\n" for item in cases),
    encoding="utf-8",
)
(OUTPUT / "eval-complex.dataset.json").write_text(
    json.dumps(
        {
            "name": "算法题库复杂场景评测 v1",
            "description": (
                "50 条真实题库场景：题目讲解与提示、WA/TLE/MLE/RE 代码诊断修正、"
                "相关题推荐、多轮算法辅导及边界推理。判题诊断使用题库题目与明确提供的代码样例，"
                "不绑定易变的真实提交 ID。"
            ),
            "content_file": "eval-complex.jsonl",
            "case_count": 50,
            "categories": {
                "bank_problem_solving": 12,
                "verdict_debugging": 12,
                "bank_related_problems": 8,
                "multi_turn_learning": 10,
                "algorithm_reasoning_and_review": 8,
            },
        },
        ensure_ascii=False,
        indent=2,
    )
    + "\n",
    encoding="utf-8",
)
print(f"Wrote {len(cases)} cases to {jsonl_path}")
