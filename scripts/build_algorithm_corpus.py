"""生成可审阅的经典算法语料；题面、解答和测试均为本项目改编，不复制第三方题解。"""

import json
import subprocess
import sys
import textwrap
from pathlib import Path

from corpus_inputs import generated_inputs

ROOT = Path(__file__).resolve().parents[1] / "data/algorithm-corpus"
ENTRIES = []


def add(slug, title, source, tags, task, fmt, limits, idea, proof, complexity, code, cases):
    ENTRIES.append(dict(slug=slug, title=title, source=source, tags=tags.split(','), task=task,
                        fmt=fmt, limits=limits, idea=idea, proof=proof, complexity=complexity,
                        code=textwrap.dedent(code).strip() + '\n', cases=cases))


def lc(slug):
    return 'https://leetcode.cn/problems/' + slug + '/'


def lg(number):
    return 'https://www.luogu.com.cn/problem/' + number


add('two-sum','两数之和（唯一解）',lc('two-sum'),'数组,哈希表',
    '给定整数数组和目标值，输出两个不同元素的下标，使其和为目标值。保证恰有一组解，下标从 1 开始且升序输出。',
    '输入 n target，下一行 n 个整数。输出两个下标。','2≤n≤200000，元素及 target 绝对值≤10^9。',
    '从左到右扫描，用哈希表保存已访问值的下标。对 x 查找 target-x，命中后立即返回。先查询再插入，避免重复使用当前元素。',
    '处理第 j 项时哈希表包含所有更早元素。唯一答案的第二个下标到来时，其补数必在表中，因此算法必找到且不会使用同一位置。','期望 O(n) 时间，O(n) 空间。',
    '''n,t=map(int,input().split()); a=list(map(int,input().split())); seen={}
for j,x in enumerate(a,1):
    if t-x in seen:
        print(seen[t-x],j); break
    seen[x]=j
''',[('4 9\n2 7 11 15\n','1 2\n'),('2 6\n3 3\n','1 2\n'),('3 0\n-2 5 2\n','1 3\n')])
add('max-subarray','最大连续子数组和',lc('maximum-subarray'),'数组,动态规划',
    '求非空连续子数组的最大元素和。','输入 n，再输入 n 个整数；输出最大和。','1≤n≤200000，|a_i|≤10^9。',
    '维护以当前位置结尾的最大和 cur=max(x,cur+x)，同时更新全局答案。必须从首元素初始化。',
    '以 i 结尾的最优区间要么仅含 a_i，要么延伸以 i-1 结尾的最优区间，递推穷尽两种情况。对所有终点取最大得到全局最优。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); cur=ans=a[0]
for x in a[1:]:
    cur=max(x,cur+x); ans=max(ans,cur)
print(ans)
''',[('9\n-2 1 -3 4 -1 2 1 -5 4\n','6\n'),('3\n-5 -2 -8\n','-2\n'),('1\n0\n','0\n')])
add('move-zeroes','稳定移动零',lc('move-zeroes'),'数组,双指针',
    '把所有零移到数组末尾，非零元素保持原顺序。','输入 n 和 n 个整数；输出变换后的数组。','1≤n≤200000，|a_i|≤10^9。',
    '写指针 w 指向下一个非零元素存放处，扫描非零元素依次写入，最后补零。',
    '每次写入后前 w 项恰是已扫描前缀中的全部非零元素，且顺序相同。扫描完成后剩余位置只能填零。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); w=0
for x in a:
    if x!=0: a[w]=x; w+=1
for i in range(w,n): a[i]=0
print(*a)
''',[('5\n0 1 0 3 12\n','1 3 12 0 0\n'),('3\n0 0 0\n','0 0 0\n'),('1\n-1\n','-1\n')])
add('sorted-squares','有序数组的平方',lc('squares-of-a-sorted-array'),'数组,双指针',
    '给定非递减数组，输出各元素平方后的非递减序列。','输入 n 和有序数组；输出平方序列。','1≤n≤200000，|a_i|≤10^9。',
    '最大绝对值一定在当前两端。比较左右端绝对值，把较大平方从答案尾部向前填入。',
    '有序区间内所有元素的绝对值不超过两端绝对值最大值，因此每次选出的都是剩余最大平方，逆序填入后整体有序。','O(n) 时间，O(n) 答案空间。',
    '''n=int(input()); a=list(map(int,input().split())); b=[0]*n; l=0; r=n-1
for i in range(n-1,-1,-1):
    if abs(a[l])>abs(a[r]): b[i]=a[l]*a[l]; l+=1
    else: b[i]=a[r]*a[r]; r-=1
print(*b)
''',[('5\n-4 -1 0 3 10\n','0 1 9 16 100\n'),('2\n-3 -2\n','4 9\n'),('1\n0\n','0\n')])
add('container-water','盛水容器',lc('container-with-most-water'),'双指针,贪心',
    '选择两条竖线，容量等于下标距离乘以较低高度，求最大容量。','输入 n 和 n 个高度；输出最大容量。','2≤n≤200000，0≤h_i≤10^9。',
    '双指针从两端开始，记录面积后移动较矮的一端。',
    '固定较矮端并缩短宽度，较低高度不可能变大，所以不会得到更优解；可以安全舍弃较矮端。逐步排除后必保留某个最优解。','O(n) 时间，O(1) 空间。',
    '''n=int(input()); a=list(map(int,input().split())); l=0; r=n-1; ans=0
while l<r:
    ans=max(ans,(r-l)*min(a[l],a[r]))
    if a[l]<a[r]: l+=1
    else: r-=1
print(ans)
''',[('9\n1 8 6 2 5 4 8 3 7\n','49\n'),('2\n0 9\n','0\n'),('3\n5 5 5\n','10\n')])
add('longest-unique','最长无重复子串',lc('longest-substring-without-repeating-characters'),'字符串,滑动窗口',
    '求字符串中不含重复字符的最长连续子串长度。','输入一行非空小写英文字母字符串；输出长度。','1≤长度≤200000。',
    '记录每个字符最近出现的位置。右端加入字符后，左端跳至上次同字符之后，但不能向左回退。',
    '更新后窗口中当前字符唯一，其余字符仍唯一；左端是满足条件的最左位置，因此对每个右端得到了最长合法窗口。','O(n) 时间，O(26) 空间。',
    '''s=input().strip(); last={}; l=0; ans=0
for r,c in enumerate(s):
    l=max(l,last.get(c,-1)+1); last[c]=r; ans=max(ans,r-l+1)
print(ans)
''',[('abcabcbb\n','3\n'),('bbbbb\n','1\n'),('abba\n','2\n')])
add('min-window-sum','最短达标子数组',lc('minimum-size-subarray-sum'),'数组,滑动窗口',
    '正整数数组中，求和至少 target 的最短非空连续子数组长度。不存在输出 0。','输入 n target，再输入数组；输出最短长度。','1≤n≤200000，1≤a_i≤10^6，1≤target≤10^15。',
    '扩展右端累加，和达标时持续收缩左端并更新最短长度。正数保证窗口和单调。',
    '每个右端对应的合法起点是一段前缀，收缩到刚好不合法前已检查最短合法窗口。每个元素最多进入和离开一次。','O(n) 时间，O(1) 额外空间。',
    '''n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; total=0; ans=n+1
for r,x in enumerate(a):
    total+=x
    while total>=t:
        ans=min(ans,r-l+1); total-=a[l]; l+=1
print(0 if ans==n+1 else ans)
''',[('6 7\n2 3 1 2 4 3\n','2\n'),('3 100\n1 2 3\n','0\n'),('1 5\n5\n','1\n')])
add('binary-search','二分查找首个目标',lc('find-first-and-last-position-of-element-in-sorted-array'),'数组,二分查找',
    '在非递减数组中求 target 首次出现的位置（1 开始），不存在输出 -1。','输入 n target 和数组；输出位置。','1≤n≤200000，|a_i|、|target|≤10^9。',
    '在半开区间 [l,r) 查找首个大于等于 target 的位置，再检查是否相等。',
    '循环保持 l 左侧都小于 target，r 及右侧都不小于 target。区间为空时 l 就是唯一分界点。','O(log n) 时间，O(1) 空间。',
    '''n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; r=n
while l<r:
    m=(l+r)//2
    if a[m]<t: l=m+1
    else: r=m
print(l+1 if l<n and a[l]==t else -1)
''',[('5 2\n1 2 2 3 4\n','2\n'),('2 9\n1 2\n','-1\n'),('1 -2\n-2\n','1\n')])
add('integer-sqrt','整数平方根',lc('sqrtx'),'数学,二分查找',
    '给定非负整数 x，求平方不超过 x 的最大整数。','输入 x；输出整数平方根。','0≤x≤10^18。',
    '二分第一个平方大于 x 的整数，再减一。其他语言可用除法比较避免乘法溢出。',
    '平方函数在非负整数上单调，二分保持答案分界位于区间内，最终定位首个不满足平方上界的位置。','O(log x) 时间，O(1) 空间。',
    '''x=int(input()); l=0; r=x+1
while l<r:
    m=(l+r)//2
    if m*m<=x: l=m+1
    else: r=m
print(l-1)
''',[('8\n','2\n'),('0\n','0\n'),('1000000000000000000\n','1000000000\n')])
add('split-array','最小化最大分段和',lc('split-array-largest-sum'),'二分答案,贪心',
    '把正整数数组分成恰好 k 个非空连续段，使最大段和最小。','输入 n k 和数组；输出最小可能的最大段和。','1≤k≤n≤100000，1≤a_i≤10^9。',
    '二分容量 C，贪心尽量填满每段，统计所需段数。若段数≤k，可拆分成恰好 k 段。',
    '贪心第一段覆盖不短于任何可行方案第一段，归纳可知其段数最少。可行容量具有单调性，因此二分首个可行值正确。','O(n log(sum a)) 时间，O(1) 额外空间。',
    '''n,k=map(int,input().split()); a=list(map(int,input().split())); l=max(a); r=sum(a)
while l<r:
    m=(l+r)//2; cnt=1; s=0
    for x in a:
        if s+x>m: cnt+=1; s=0
        s+=x
    if cnt<=k: r=m
    else: l=m+1
print(l)
''',[('5 2\n7 2 5 10 8\n','18\n'),('3 3\n1 2 3\n','3\n'),('1 1\n9\n','9\n')])
add('range-sum','静态区间求和',lc('range-sum-query-immutable'),'前缀和,数组',
    '回答若干闭区间 [l,r] 的元素和，下标从 1 开始。','输入 n q 和数组，随后 q 行 l r；每行输出一个区间和。','1≤n,q≤200000，|a_i|≤10^9。',
    '令 p[0]=0，p[i]=p[i-1]+a_i，区间和等于 p[r]-p[l-1]。',
    '两个前缀相减时前 l-1 项抵消，仅剩区间项。初始化 p[0] 覆盖从首项开始的查询。','O(n+q) 时间，O(n) 空间。',
    '''import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); p=[0]
for _ in range(n): p.append(p[-1]+next(it))
for _ in range(q):
    l=next(it); r=next(it); print(p[r]-p[l-1])
''',[('5 3\n1 -2 3 4 5\n1 5\n2 2\n3 4\n','11\n-2\n7\n'),('1 1\n0\n1 1\n','0\n'),('2 1\n1000000000 1000000000\n1 2\n','2000000000\n')])
add('subarray-sum','和为 K 的子数组数',lc('subarray-sum-equals-k'),'前缀和,哈希表',
    '统计元素和等于 k 的非空连续子数组数，数组可以有负数。','输入 n k 和数组；输出数量。','1≤n≤200000，|a_i|≤10^6，|k|≤10^12。',
    '扫描前缀和 s，累加此前前缀 s-k 的出现次数，再登记 s。初始前缀 0 出现一次。',
    '区间和为 k 等价于两端前缀差为 k，查找历史前缀恰好枚举每个以当前项结尾的合法区间，且不会计入空区间。','期望 O(n) 时间，O(n) 空间。',
    '''n,k=map(int,input().split()); a=list(map(int,input().split())); count={0:1}; s=ans=0
for x in a:
    s+=x; ans+=count.get(s-k,0); count[s]=count.get(s,0)+1
print(ans)
''',[('3 2\n1 1 1\n','2\n'),('3 0\n0 0 0\n','6\n'),('3 0\n1 -1 1\n','2\n')])
add('valid-parentheses','括号匹配',lc('valid-parentheses'),'栈,字符串',
    '判断仅含 ()[]{} 的非空字符串是否正确嵌套且完全匹配。','输入括号串；合法输出 YES，否则 NO。','1≤长度≤200000。',
    '左括号入栈，右括号必须与栈顶类型相同并出栈，最后栈须为空。',
    '栈始终是已扫描部分尚未匹配的左括号，最后开启的括号必须最先关闭，故每次检查正是合法嵌套的必要条件，也足以保证完整匹配。','O(n) 时间，O(n) 空间。',
    '''s=input().strip(); st=[]; pairs={')':'(',']':'[','}':'{'}; ok=True
for c in s:
    if c in '([{': st.append(c)
    elif not st or st.pop()!=pairs[c]: ok=False; break
print('YES' if ok and not st else 'NO')
''',[('([]{})\n','YES\n'),('([)]\n','NO\n'),('((\n','NO\n')])
add('daily-temperatures','等待更高温度',lc('daily-temperatures'),'单调栈,数组',
    '对每天输出距离下一次严格更高温度的天数，没有则为 0。','输入 n 和温度数组；输出 n 个等待天数。','1≤n≤200000，0≤温度≤100。',
    '维护温度非递增的下标栈。遇到更高温度，弹出所有较低温度位置并计算距离。',
    '栈中位置此前都没有更高温度，当前项第一次满足条件，故弹栈时得到最近的更高位置；未弹出的不存在答案。','O(n) 时间，O(n) 空间。',
    '''n=int(input()); a=list(map(int,input().split())); st=[]; ans=[0]*n
for i,x in enumerate(a):
    while st and a[st[-1]]<x:
        j=st.pop(); ans[j]=i-j
    st.append(i)
print(*ans)
''',[('8\n73 74 75 71 69 72 76 73\n','1 1 4 2 1 1 0 0\n'),('3\n5 5 5\n','0 0 0\n'),('1\n0\n','0\n')])
add('sliding-maximum','滑动窗口最大值',lc('sliding-window-maximum'),'单调队列,滑动窗口',
    '输出所有长度为 k 的连续窗口最大值。','输入 n k 和数组；输出 n-k+1 个最大值。','1≤k≤n≤200000，|a_i|≤10^9。',
    '双端队列存下标，队首过期即弹出，队尾不大于新值的元素永远不会成为最大值，可删除。',
    '队列保留尚可能成为最大值的候选且值递减，队首总是当前窗口最大值。被新元素支配的位置更早过期，删除不影响未来。','O(n) 时间，O(k) 空间。',
    '''from collections import deque
n,k=map(int,input().split()); a=list(map(int,input().split())); q=deque(); ans=[]
for i,x in enumerate(a):
    while q and q[0]<=i-k: q.popleft()
    while q and a[q[-1]]<=x: q.pop()
    q.append(i)
    if i>=k-1: ans.append(a[q[0]])
print(*ans)
''',[('8 3\n1 3 -1 -3 5 3 6 7\n','3 3 5 5 6 7\n'),('2 1\n-2 1\n','-2 1\n'),('3 3\n4 4 4\n','4\n')])
add('merge-intervals','区间合并计数',lc('merge-intervals'),'排序,贪心',
    '合并有交集的闭区间，端点相接也合并，输出最终区间数。','输入 n，随后 n 行 l r；输出合并后的区间数。','1≤n≤200000，-10^9≤l≤r≤10^9。',
    '按左端排序，维护当前右端。新左端不大于当前右端则延长，否则开启新区间。',
    '排序后若新左端超过当前右端，后续左端更大，当前区间不可能再与后续合并；否则两者并集仍是一个区间。','O(n log n) 时间，O(n) 排序空间。',
    '''n=int(input()); a=sorted(tuple(map(int,input().split())) for _ in range(n)); ans=0; end=None
for l,r in a:
    if end is None or l>end: ans+=1; end=r
    else: end=max(end,r)
print(ans)
''',[('4\n1 3\n2 6\n8 10\n15 18\n','3\n'),('2\n1 2\n2 3\n','1\n'),('1\n-1 -1\n','1\n')])
add('jump-game','能否到达终点',lc('jump-game'),'贪心,数组',
    '数组元素表示当前位置最多能向右跳多少步，判断从首项能否到达末项。','输入 n 和非负数组；输出 YES 或 NO。','1≤n≤200000，0≤a_i≤200000。',
    '维护当前可达最远位置 far。若扫描下标超过 far 则失败，否则用 i+a_i 扩展。',
    '可达位置构成从起点开始的连续前缀，扫描前缀中的每个位置可发现其全部后继；无法越过的间隙也不可能由后续不可达位置补上。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); far=0
for i,x in enumerate(a):
    if i>far: break
    far=max(far,i+x)
print('YES' if far>=n-1 else 'NO')
''',[('5\n2 3 1 1 4\n','YES\n'),('5\n3 2 1 0 4\n','NO\n'),('1\n0\n','YES\n')])
add('stock-profit','单次股票交易',lc('best-time-to-buy-and-sell-stock'),'贪心,数组',
    '最多买卖一次且先买后卖，求最大利润，允许不交易。','输入 n 和每天价格；输出利润。','1≤n≤200000，0≤价格≤10^9。',
    '扫描时维护历史最低价格，以当前价格卖出的利润减去最低买入价，取最大值。',
    '任意卖出日的最优买入日一定是此前最低价日，扫描所有卖出日就穷尽所有候选。答案初始 0 覆盖不交易。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); low=a[0]; ans=0
for x in a:
    ans=max(ans,x-low); low=min(low,x)
print(ans)
''',[('6\n7 1 5 3 6 4\n','5\n'),('3\n9 5 1\n','0\n'),('1\n0\n','0\n')])
add('climbing-stairs','爬楼梯计数',lc('climbing-stairs'),'动态规划,递推',
    '每次爬 1 或 2 级台阶，求到达第 n 级的方法数，对 1000000007 取模。','输入 n；输出方法数。','1≤n≤1000000。',
    '最后一步来自 n-1 或 n-2，使用滚动变量计算 Fibonacci 型递推。',
    '按最后一步分类，两类方案互斥且穷尽。设空路径 f[0]=1，f[1]=1 后递推成立。','O(n) 时间，O(1) 空间。',
    '''n=int(input()); a=b=1
for _ in range(1,n): a,b=b,(a+b)%1000000007
print(b)
''',[('2\n','2\n'),('1\n','1\n'),('5\n','8\n')])
add('house-robber','不相邻元素最大和',lc('house-robber'),'动态规划,数组',
    '选择若干互不相邻的数组元素，求最大和，可以不选。','输入 n 和非负数组；输出最大和。','1≤n≤200000，0≤a_i≤10^9。',
    'dp[i]=max(dp[i-1],dp[i-2]+a_i)，用两个变量滚动保存。',
    '最优方案要么不选第 i 项，要么选它并排除第 i-1 项；两种情况的剩余部分均为更小前缀上的最优问题。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); p=q=0
for x in a: p,q=q,max(q,p+x)
print(q)
''',[('5\n2 7 9 3 1\n','12\n'),('1\n0\n','0\n'),('3\n5 1 5\n','10\n')])
add('coin-change','最少硬币数',lc('coin-change'),'动态规划,完全背包',
    '每种正整数面值硬币无限，求凑出金额 amount 最少硬币数，无法凑出输出 -1。','输入 n amount，再输入 n 个不同面值；输出最少数。','1≤n≤50，0≤amount≤10000，1≤面值≤10000。',
    'dp[0]=0，其余无穷。按金额递增，枚举最后一枚硬币，更新 dp[s]=min(dp[s-c]+1)。',
    '正面值保证递推依赖更小金额，任意最优方案删除最后一枚硬币后仍须最优，否则可以替换得到更短方案。','O(n·amount) 时间，O(amount) 空间。',
    '''n,m=map(int,input().split()); coins=list(map(int,input().split())); dp=[0]+[m+1]*m
for s in range(1,m+1):
    for c in coins:
        if c<=s: dp[s]=min(dp[s],dp[s-c]+1)
print(dp[m] if dp[m]<=m else -1)
''',[('3 11\n1 2 5\n','3\n'),('1 3\n2\n','-1\n'),('1 0\n7\n','0\n')])
add('lis','最长严格递增子序列',lc('longest-increasing-subsequence'),'动态规划,二分查找',
    '求可不连续的严格递增子序列最大长度。','输入 n 和数组；输出长度。','1≤n≤200000，|a_i|≤10^9。',
    'tails[k] 保存长度 k+1 的递增子序列最小末尾，二分首个不小于 x 的位置替换。',
    '同长度末尾越小越利于延伸，替换不丢失任何可行长度；只有 x 大于所有末尾时才能创造新长度。由归纳得到 tails 长度等于最优长度。','O(n log n) 时间，O(n) 空间。',
    '''from bisect import bisect_left
n=int(input()); a=list(map(int,input().split())); tails=[]
for x in a:
    k=bisect_left(tails,x)
    if k==len(tails): tails.append(x)
    else: tails[k]=x
print(len(tails))
''',[('8\n10 9 2 5 3 7 101 18\n','4\n'),('3\n2 2 2\n','1\n'),('1\n-5\n','1\n')])
add('lcs','最长公共子序列',lc('longest-common-subsequence'),'字符串,动态规划',
    '求两个小写字母字符串的最长公共子序列长度。','输入两行字符串；输出长度。','每个字符串长度为 1 至 1000。',
    '滚动行计算 dp。相等时取左上角加一，否则取上方和左方最大值。',
    '末字符相同可把公共解延伸一位；不同时至少一边末字符不被同时选中，去掉其中一边即覆盖全部最优可能。','O(nm) 时间，O(m) 空间。',
    '''a=input().strip(); b=input().strip(); dp=[0]*(len(b)+1)
for x in a:
    prev=0
    for j,y in enumerate(b,1):
        old=dp[j]; dp[j]=prev+1 if x==y else max(dp[j],dp[j-1]); prev=old
print(dp[-1])
''',[('abcde\nace\n','3\n'),('abc\ndef\n','0\n'),('aaa\naa\n','2\n')])
add('edit-distance','编辑距离',lc('edit-distance'),'字符串,动态规划',
    '插入、删除、替换一个字符各花费 1，求把第一个字符串变成第二个的最小代价。','输入两行小写字母字符串；输出最小代价。','字符串长度均为 1 至 1000。',
    'dp[i][j] 表示两个前缀距离，枚举最后一次插入、删除、替换/匹配，滚动行优化。',
    '任意转换序列可按末字符处理方式归入这三类，取最小且子问题最优即得全局最优。空前缀到长度 j 的距离是 j。','O(nm) 时间，O(m) 空间。',
    '''a=input().strip(); b=input().strip(); dp=list(range(len(b)+1))
for i,x in enumerate(a,1):
    row=[i]
    for j,y in enumerate(b,1): row.append(min(dp[j]+1,row[-1]+1,dp[j-1]+(x!=y)))
    dp=row
print(dp[-1])
''',[('horse\nros\n','3\n'),('a\na\n','0\n'),('abc\nx\n','3\n')])
add('grid-paths','网格路径数',lc('unique-paths-ii'),'网格,动态规划',
    '从左上到右下只能向右或向下，1 为障碍、0 可走，求路径数模 1000000007。','输入 n m，随后 n 行 m 个 0/1；输出路径数。','1≤n,m≤500。',
    '滚动数组存每列到达方式，障碍清零，否则从上方和左方转移。',
    '每条到当前格的路径最后一步恰好从上或左而来，两类互斥；障碍无路径。初始化起点前的虚拟路径为 1。','O(nm) 时间，O(m) 空间。',
    '''n,m=map(int,input().split()); dp=[0]*m; dp[0]=1
for _ in range(n):
    row=list(map(int,input().split()))
    for j,x in enumerate(row):
        if x: dp[j]=0
        elif j: dp[j]=(dp[j]+dp[j-1])%1000000007
print(dp[-1])
''',[('3 3\n0 0 0\n0 1 0\n0 0 0\n','2\n'),('1 1\n1\n','0\n'),('1 1\n0\n','1\n')])
add('islands','岛屿数量',lc('number-of-islands'),'网格,广度优先搜索',
    '统计 0/1 网格中 1 的四连通块个数。','输入 n m，随后 n 行无空格 01 串；输出数量。','1≤n,m≤500。',
    '扫描未访问陆地时计数加一，用迭代 BFS 清除整个连通块，避免递归栈溢出。',
    'BFS 恰好访问起点所在连通块；每个连通块第一次被扫描时贡献一次，之后均已标记，不会漏计或重复。','O(nm) 时间，O(nm) 空间。',
    '''from collections import deque
n,m=map(int,input().split()); a=[list(input().strip()) for _ in range(n)]; ans=0
for i in range(n):
    for j in range(m):
        if a[i][j]!='1': continue
        ans+=1; a[i][j]='0'; q=deque([(i,j)])
        while q:
            x,y=q.popleft()
            for dx,dy in ((1,0),(-1,0),(0,1),(0,-1)):
                u,v=x+dx,y+dy
                if 0<=u<n and 0<=v<m and a[u][v]=='1': a[u][v]='0'; q.append((u,v))
print(ans)
''',[('3 3\n110\n010\n001\n','2\n'),('1 3\n000\n','0\n'),('2 2\n11\n11\n','1\n')])
add('course-order','课程依赖是否可行',lc('course-schedule'),'图论,拓扑排序',
    '有 n 门课，依赖边 u v 表示学 v 前必须先学 u，判断能否修完全部课程。','输入 n m，随后 m 行有向边 u v；可行输出 YES，否则 NO。','1≤n≤200000，0≤m≤300000，顶点 1 至 n。',
    'Kahn 算法不断删除入度为 0 的顶点及其出边，计数是否达到 n。',
    '无环图一定有入度零顶点，删除后仍无环；环上的顶点始终至少保留一条环内入边。因此全部删完等价于无环。','O(n+m) 时间及空间。',
    '''from collections import deque
n,m=map(int,input().split()); g=[[] for _ in range(n)]; d=[0]*n
for _ in range(m):
    u,v=map(int,input().split()); g[u-1].append(v-1); d[v-1]+=1
q=deque(i for i in range(n) if d[i]==0); cnt=0
while q:
    u=q.popleft(); cnt+=1
    for v in g[u]:
        d[v]-=1
        if d[v]==0: q.append(v)
print('YES' if cnt==n else 'NO')
''',[('3 2\n1 2\n2 3\n','YES\n'),('2 2\n1 2\n2 1\n','NO\n'),('1 0\n','YES\n')])
add('network-delay','网络最远到达时间',lc('network-delay-time'),'图论,最短路',
    '从源点 s 发送信息，非负有向边有传输时间，求所有点都收到信息的最早时刻；有点不可达则输出 -1。','输入 n m s，随后 m 行 u v w；输出最大最短路距离或 -1。','1≤n≤100000，0≤m≤200000，0≤w≤10^9。',
    '使用小根堆 Dijkstra，跳过过时堆项，最后检查所有距离。',
    '非负权保证堆中最小未定距离已无法被其他更远路径改善，按此顺序松弛得到全部最短距离；最迟收到者决定总时间。','O((n+m) log(n+m)) 时间，O(n+m) 空间。',
    '''import heapq
n,m,s=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(m):
    u,v,w=map(int,input().split()); g[u-1].append((v-1,w))
d=[float('inf')]*n; d[s-1]=0; q=[(0,s-1)]
while q:
    du,u=heapq.heappop(q)
    if du!=d[u]: continue
    for v,w in g[u]:
        if du+w<d[v]: d[v]=du+w; heapq.heappush(q,(d[v],v))
print(-1 if max(d)==float('inf') else max(d))
''',[('4 3 2\n2 1 1\n2 3 1\n3 4 1\n','2\n'),('2 0 1\n','-1\n'),('1 0 1\n','0\n')])
add('province-count','连通分量计数',lc('number-of-provinces'),'并查集,图论',
    '给定无向图，求连通分量数量，孤立顶点也算一个。','输入 n m，随后 m 行边 u v；输出数量。','1≤n≤200000，0≤m≤300000。',
    '并查集初始 n 个集合，按大小合并不同根，成功合并一次计数减一。',
    '每条边只要求两端属于同一连通分量，合并维护所有已处理边生成的连通关系。处理完全部边后集合恰为连通分量。','O((n+m) α(n)) 时间，O(n) 空间。',
    '''n,m=map(int,input().split()); p=list(range(n)); size=[1]*n; ans=n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
for _ in range(m):
    u,v=map(int,input().split()); a,b=find(u-1),find(v-1)
    if a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]; ans-=1
print(ans)
''',[('5 3\n1 2\n2 3\n4 5\n','2\n'),('3 0\n','3\n'),('2 3\n1 2\n1 2\n1 1\n','1\n')])
add('tree-depth','有根树最大深度',lc('maximum-depth-of-binary-tree'),'树,广度优先搜索',
    '给定以 1 为根的无向树，求最大深度，根深度为 1。本题将二叉树经典题推广到一般树。','输入 n，随后 n-1 行树边；输出最大深度。','1≤n≤200000。',
    '从根 BFS，孩子深度为父亲深度加一，用访问标记避免沿无向边返回。',
    '树上从根到每点路径唯一，BFS 为每点赋予该路径顶点数，所有深度最大值就是答案。','O(n) 时间及空间。',
    '''from collections import deque
n=int(input()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
d=[0]*n; d[0]=1; q=deque([0])
while q:
    u=q.popleft()
    for v in g[u]:
        if not d[v]: d[v]=d[u]+1; q.append(v)
print(max(d))
''',[('4\n1 2\n2 3\n1 4\n','3\n'),('1\n','1\n'),('3\n1 2\n1 3\n','2\n')])
add('tree-diameter','树的直径',lc('diameter-of-binary-tree'),'树,图论',
    '求无向树中最长简单路径的边数；由经典二叉树直径题推广。','输入 n 和 n-1 条无向边；输出直径。','1≤n≤200000。',
    '从任意点 BFS 找最远点 u，再从 u BFS，最远距离为直径。',
    '在树的唯一路径性质下，任意点的某个最远点是某条直径端点；否则沿直径向更远端延伸会得到更远点，矛盾。第二次遍历即得到最长路径。','O(n) 时间及空间。',
    '''from collections import deque
n=int(input()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
def far(s):
    d=[-1]*n; d[s]=0; q=deque([s])
    while q:
        u=q.popleft()
        for v in g[u]:
            if d[v]<0: d[v]=d[u]+1; q.append(v)
    u=max(range(n),key=lambda x:d[x]); return u,d[u]
print(far(far(0)[0])[1])
''',[('4\n1 2\n2 3\n2 4\n','2\n'),('1\n','0\n'),('4\n1 2\n2 3\n3 4\n','3\n')])
add('reverse-list','反转单链表序列',lc('reverse-linked-list'),'链表,模拟',
    '用数组给出链表节点顺序，构造链表并反转，输出反转后的节点值。','输入 n 和 n 个整数；输出反转顺序。','1≤n≤200000，|值|≤10^9。',
    '按顺序创建单链表，循环保存后继，将当前 next 指向 prev，再前移。',
    '循环前 prev 是已处理前缀的反转链，cur 是未处理后缀；每步把后缀首节点移到反转链头，结束时整链反转。','O(n) 时间，构建 O(n) 空间，反转 O(1) 额外空间。',
    '''n=int(input()); a=list(map(int,input().split())); nodes=[[x,None] for x in a]
for i in range(n-1): nodes[i][1]=nodes[i+1]
prev=None; cur=nodes[0]
while cur:
    nxt=cur[1]; cur[1]=prev; prev=cur; cur=nxt
ans=[]
while prev: ans.append(prev[0]); prev=prev[1]
print(*ans)
''',[('4\n1 2 3 4\n','4 3 2 1\n'),('1\n-1\n','-1\n'),('2\n0 0\n','0 0\n')])
add('single-number','只出现一次的数',lc('single-number'),'位运算,数组',
    '数组中一个数出现一次，其余每个数出现两次，求单独的数。','输入 n 和整数数组；输出该数。','1≤n≤200001，n 为奇数，0≤a_i≤10^9。',
    '把所有元素按位异或。',
    '异或可交换结合，x xor x=0，0 xor y=y。成对元素抵消，最终仅剩唯一数。','O(n) 时间，O(1) 额外空间。',
    '''n=int(input()); ans=0
for x in map(int,input().split()): ans^=x
print(ans)
''',[('5\n4 1 2 1 2\n','4\n'),('1\n0\n','0\n'),('3\n7 9 7\n','9\n')])
add('kmp','字符串匹配位置',lg('P3375'),'字符串,KMP',
    '给定文本和模式，输出模式首次出现的 1 开始位置，没有则 -1。这是模板题的单次定位版本。','输入文本、模式各一行；输出位置。','1≤两串长度≤200000，仅小写英文字母。',
    '先构造模式前缀函数，匹配失败时回退到最长可用 border，成功时推进。',
    '前缀函数枚举失配后仍可能匹配的最长前后缀，不跳过可能起点；每次完整匹配是最先结束的，固定模式长度下也是最先开始的。','O(n+m) 时间，O(m) 空间。',
    '''s=input().strip(); p=input().strip(); pi=[0]*len(p); j=0
for i in range(1,len(p)):
    while j and p[i]!=p[j]: j=pi[j-1]
    if p[i]==p[j]: j+=1
    pi[i]=j
j=0; ans=-1
for i,c in enumerate(s):
    while j and c!=p[j]: j=pi[j-1]
    if c==p[j]: j+=1
    if j==len(p): ans=i-len(p)+2; break
print(ans)
''',[('ababcabc\nabc\n','3\n'),('aaaa\nb\n','-1\n'),('aaaaa\naaa\n','1\n')])
add('knapsack','采药式零一背包',lg('P1048'),'动态规划,零一背包',
    '每件物品最多取一次，在容量限制内最大化总价值。','输入 n capacity，随后 n 行 weight value；输出最大价值。','1≤n≤100，0≤capacity≤10000，1≤weight≤10000，0≤value≤10^6。',
    '一维 dp 按容量降序更新，保证当前物品只使用一次。',
    '更新 dp[c] 时较小容量尚是上一轮值，故转移恰好表示不取或取当前物品一次。若正序会错误地重复选取。','O(n·capacity) 时间，O(capacity) 空间。',
    '''n,c=map(int,input().split()); dp=[0]*(c+1)
for _ in range(n):
    w,v=map(int,input().split())
    for j in range(c,w-1,-1): dp[j]=max(dp[j],dp[j-w]+v)
print(dp[c])
''',[('3 5\n2 3\n3 4\n4 5\n','7\n'),('1 4\n2 3\n','3\n'),('1 0\n1 9\n','0\n')])
add('triangle-path','数字三角形',lg('P1216'),'动态规划,网格',
    '从三角形顶端向下，每次到左下或右下，求到最后一行的最大路径和。','输入 n，随后第 i 行 i 个整数；输出最大和。','1≤n≤500，|元素|≤10^6。',
    '自底向上把每个位置改为自身值加两个孩子最优值的最大者。',
    '任意路径下一步只能选择两个孩子之一，孩子最优解已知时取最大即为当前最优，逐层归纳到根。','O(n²) 时间，O(n²) 空间。',
    '''n=int(input()); a=[list(map(int,input().split())) for _ in range(n)]
for i in range(n-2,-1,-1):
    for j in range(i+1): a[i][j]+=max(a[i+1][j],a[i+1][j+1])
print(a[0][0])
''',[('3\n1\n2 3\n4 5 6\n','10\n'),('1\n-7\n','-7\n'),('2\n-1\n-2 -3\n','-3\n')])
add('mst','最小生成树',lg('P3366'),'图论,最小生成树,并查集',
    '无向带权图中求连接所有顶点的最小总边权，图不连通输出 -1。','输入 n m，随后 m 行 u v w；输出最小权和或 -1。','1≤n≤100000，0≤m≤200000，0≤w≤10^9。',
    'Kruskal 按边权排序，并查集跳过形成环的边，选满 n-1 条即得到生成树。',
    '每次跨越当前不同连通块选择的最小边满足割性质，可包含于某棵最小生成树。重复安全选择最终得到最优生成树；选不满说明不连通。','O(m log m) 时间，O(n+m) 空间。',
    '''n,m=map(int,input().split()); edges=[]
for _ in range(m):
    u,v,w=map(int,input().split()); edges.append((w,u-1,v-1))
p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
ans=cnt=0
for w,u,v in sorted(edges):
    a,b=find(u),find(v)
    if a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]; ans+=w; cnt+=1
print(ans if cnt==n-1 else -1)
''',[('3 3\n1 2 5\n2 3 2\n1 3 4\n','6\n'),('2 0\n','-1\n'),('1 0\n','0\n')])
add('floyd','多源最短路',lg('P2910'),'图论,Floyd',
    '给定有向非负权图，回答点对之间最短距离，不可达输出 -1。','输入 n m q，随后 m 行 u v w，再 q 行 s t；逐行输出距离。','1≤n≤100，0≤m≤10000，1≤q≤10000，0≤w≤10^6。',
    'Floyd 按允许的中间顶点逐个扩展，d[i][j]=min(d[i][j],d[i][k]+d[k][j])。',
    '允许前 k 个中间点的最短路径要么不用 k，要么经过 k 并分成两条只用前 k-1 个中间点的路径。k 必须在最外层。','O(n³+q) 时间，O(n²) 空间。',
    '''n,m,q=map(int,input().split()); inf=10**30; d=[[inf]*n for _ in range(n)]
for i in range(n): d[i][i]=0
for _ in range(m):
    u,v,w=map(int,input().split()); d[u-1][v-1]=min(d[u-1][v-1],w)
for k in range(n):
    for i in range(n):
        for j in range(n): d[i][j]=min(d[i][j],d[i][k]+d[k][j])
for _ in range(q):
    s,t=map(int,input().split()); print(-1 if d[s-1][t-1]==inf else d[s-1][t-1])
''',[('3 2 3\n1 2 2\n2 3 4\n1 3\n3 1\n2 2\n','6\n-1\n0\n'),('1 0 1\n1 1\n','0\n'),('2 2 1\n1 2 8\n1 2 3\n1 2\n','3\n')])
add('maze','迷宫最短路',lg('P1141'),'广度优先搜索,网格',
    '网格中 0 可走、1 障碍，四方向移动，求左上角到右下角最少步数，不可达为 -1。这是网格搜索的改编练习。','输入 n m，随后 n 行 01 串；输出步数。','1≤n,m≤500。',
    '无权图 BFS，入队时标记访问，距离加一。起点或终点为障碍直接失败。',
    'BFS 按路径长度递增分层，每个格首次到达时已经是最短路径；队列耗尽仍未到达则不可达。','O(nm) 时间及空间。',
    '''from collections import deque
n,m=map(int,input().split()); a=[input().strip() for _ in range(n)]; d=[[-1]*m for _ in range(n)]; q=deque()
if a[0][0]=='0': d[0][0]=0; q.append((0,0))
while q:
    x,y=q.popleft()
    for dx,dy in ((1,0),(-1,0),(0,1),(0,-1)):
        u,v=x+dx,y+dy
        if 0<=u<n and 0<=v<m and a[u][v]=='0' and d[u][v]<0: d[u][v]=d[x][y]+1; q.append((u,v))
print(d[-1][-1])
''',[('3 3\n000\n110\n000\n','4\n'),('1 1\n0\n','0\n'),('2 2\n01\n10\n','-1\n')])
add('n-queens','N 皇后方案数',lg('P1219'),'回溯,搜索',
    '在 n×n 棋盘放 n 个皇后，使任意两枚不同行、列、对角线，求方案数，不要求输出棋盘。','输入 n；输出方案数。','1≤n≤11。',
    '逐行放置，集合记录列及两类对角线，合法时递归，返回时撤销。',
    '每行恰放一个皇后，列和对角线检查充要地排除了攻击。搜索枚举所有逐行选择且每种棋盘序列唯一，故不漏不重。','O(n!) 搜索上界，O(n) 递归及集合空间。',
    '''n=int(input()); cols=set(); diag1=set(); diag2=set()
def dfs(r):
    if r==n: return 1
    total=0
    for c in range(n):
        if c in cols or r-c in diag1 or r+c in diag2: continue
        cols.add(c); diag1.add(r-c); diag2.add(r+c); total+=dfs(r+1)
        cols.remove(c); diag1.remove(r-c); diag2.remove(r+c)
    return total
print(dfs(0))
''',[('4\n','2\n'),('1\n','1\n'),('2\n','0\n')])
add('mod-power','快速幂取模',lg('P1226'),'数论,快速幂',
    '计算 a 的 b 次方对 p 取余，约定零次幂为 1。','输入 a b p；仅输出余数。','0≤a,b≤10^18，1≤p≤10^9。',
    '按指数二进制位处理，位为 1 时乘入当前底数，每轮底数平方、指数减半。',
    '保持 ans·base^remaining 与原幂同余，处理奇偶指数均保持不变量；指数归零后 ans 即答案。','O(log(b+1)) 时间，O(1) 空间。',
    '''a,b,p=map(int,input().split()); a%=p; ans=1%p
while b:
    if b&1: ans=ans*a%p
    a=a*a%p; b//=2
print(ans)
''',[('2 10 1000\n','24\n'),('0 0 7\n','1\n'),('7 5 1\n','0\n')])
add('prime-count','区间素数计数',lg('P3383'),'数论,筛法',
    '统计 1 到 n 内的素数个数；这是筛法模板的计数版本。','输入 n；输出素数数量。','1≤n≤2000000。',
    '埃氏筛从每个素数的平方开始标记其倍数，最后统计未标记的大于 1 的数。',
    '每个合数都含不超过平方根的素因子，必被标记；素数不会被更小因数标记。小于平方的倍数早已由更小因子处理。','O(n log log n) 时间，O(n) 空间。',
    '''n=int(input()); prime=bytearray(b'\\x01')*(n+1); prime[0]=prime[1]=0
for p in range(2,int(n**0.5)+1):
    if prime[p]: prime[p*p:n+1:p]=b'\\x00'*((n-p*p)//p+1)
print(sum(prime))
''',[('10\n','4\n'),('1\n','0\n'),('2\n','1\n')])
add('fenwick','单点加区间和',lg('P3374'),'树状数组,数据结构',
    '维护数组，操作 1 i delta 把 a_i 加 delta，操作 2 l r 查询区间和。','输入 n q 和初始数组，再 q 行操作；每次查询输出区间和。','1≤n,q≤200000，初值及增量绝对值≤10^9。',
    '树状数组保存 lowbit 长度区间和，单点加向上更新，前缀和向下累加。',
    '前缀查询把 [1,x] 分解成互不相交的 lowbit 区间；单点更新遍历所有包含该位置的维护区间，因此查询始终正确。','O((n+q) log n) 时间，O(n) 空间。',
    '''import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); bit=[0]*(n+1)
def add(i,x):
    while i<=n: bit[i]+=x; i+=i&-i
def pref(i):
    s=0
    while i: s+=bit[i]; i-=i&-i
    return s
for i in range(1,n+1): add(i,next(it))
for _ in range(q):
    op,l,r=next(it),next(it),next(it)
    if op==1: add(l,r)
    else: print(pref(r)-pref(l-1))
''',[('3 3\n1 2 3\n2 1 3\n1 2 -5\n2 2 3\n','6\n0\n'),('1 1\n-1\n2 1 1\n','-1\n'),('2 2\n0 0\n1 1 9\n2 1 2\n','9\n')])
add('segment-tree','区间加与区间和',lg('P3372'),'线段树,数据结构',
    '支持 1 l r delta 区间加，以及 2 l r 查询区间和。','输入 n q 和数组，再 q 行操作；每次查询输出和。','1≤n,q≤100000，初值及增量绝对值≤10^6。',
    '线段树节点存区间和及懒标记，整段更新把和加 delta×长度，部分访问前下推。',
    '节点和包含本段所有更新，懒标记代表尚未传到孩子的共同增量。下推保持父子和一致，分治覆盖恰好查询/更新区间。','O((n+q) log n) 时间，O(n) 空间。',
    '''import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); a=[next(it) for _ in range(n)]; tr=[0]*(4*n); lazy=[0]*(4*n)
def build(p,l,r):
    if l==r: tr[p]=a[l]; return
    m=(l+r)//2; build(p*2,l,m); build(p*2+1,m+1,r); tr[p]=tr[p*2]+tr[p*2+1]
def apply(p,l,r,x): tr[p]+=x*(r-l+1); lazy[p]+=x
def push(p,l,r):
    if lazy[p] and l<r:
        m=(l+r)//2; apply(p*2,l,m,lazy[p]); apply(p*2+1,m+1,r,lazy[p]); lazy[p]=0
def change(p,l,r,ql,qr,x):
    if ql<=l and r<=qr: apply(p,l,r,x); return
    push(p,l,r); m=(l+r)//2
    if ql<=m: change(p*2,l,m,ql,qr,x)
    if qr>m: change(p*2+1,m+1,r,ql,qr,x)
    tr[p]=tr[p*2]+tr[p*2+1]
def query(p,l,r,ql,qr):
    if ql<=l and r<=qr: return tr[p]
    push(p,l,r); m=(l+r)//2; ans=0
    if ql<=m: ans+=query(p*2,l,m,ql,qr)
    if qr>m: ans+=query(p*2+1,m+1,r,ql,qr)
    return ans
build(1,0,n-1)
for _ in range(q):
    op,l,r=next(it),next(it)-1,next(it)-1
    if op==1: change(1,0,n-1,l,r,next(it))
    else: print(query(1,0,n-1,l,r))
''',[('3 4\n1 2 3\n1 1 3 2\n2 2 3\n1 2 2 -5\n2 1 3\n','9\n7\n'),('1 1\n7\n2 1 1\n','7\n'),('2 2\n0 0\n1 1 1 3\n2 1 2\n','3\n')])
add('inversions','逆序对计数',lg('P1908'),'归并排序,分治',
    '统计 i<j 且 a_i>a_j 的下标对数量。相等不算逆序。','输入 n 和数组；输出逆序对数。','1≤n≤200000，|a_i|≤10^9。',
    '归并排序，右边元素先取时累加左半尚未合并的元素数，相等时先取左边。',
    '逆序对分为左内部、右内部和跨区间。递归计算前两类，合并时每个右元素恰好统计所有大于它的左元素，三类互斥穷尽。','O(n log n) 时间，O(n) 辅助空间。',
    '''n=int(input()); a=list(map(int,input().split()))
def solve(a):
    if len(a)<=1: return a,0
    m=len(a)//2; l,x=solve(a[:m]); r,y=solve(a[m:]); i=j=0; out=[]; ans=x+y
    while i<len(l) and j<len(r):
        if l[i]<=r[j]: out.append(l[i]); i+=1
        else: out.append(r[j]); j+=1; ans+=len(l)-i
    return out+l[i:]+r[j:],ans
print(solve(a)[1])
''',[('5\n5 4 3 2 1\n','10\n'),('3\n2 2 2\n','0\n'),('3\n1 3 2\n','1\n')])
add('huffman','合并果子',lg('P1090'),'堆,贪心',
    '每次合并两堆果子，代价为两堆总重量，求全部合并成一堆的最小总代价。','输入 n 和 n 个正重量；输出代价。','1≤n≤200000，1≤重量≤10^9。',
    '每次从小根堆取最小两项合并，再放回。',
    '最优合并树可通过交换让最小的两个权重位于最深同父叶子，合并它们后问题缩小；归纳得到贪心最优。','O(n log n) 时间，O(n) 空间。',
    '''import heapq
n=int(input()); a=list(map(int,input().split())); heapq.heapify(a); ans=0
while len(a)>1:
    x=heapq.heappop(a)+heapq.heappop(a); ans+=x; heapq.heappush(a,x)
print(ans)
''',[('3\n1 2 9\n','15\n'),('1\n8\n','0\n'),('4\n1 1 1 1\n','8\n')])
add('interval-schedule','最多不重叠活动',lg('P1803'),'贪心,排序',
    '从活动 [start,end) 中选尽量多活动，时间不重叠，结束时刻等于下一活动开始允许。','输入 n，随后 n 行 start end；输出最多活动数。','1≤n≤200000，0≤start<end≤10^9。',
    '按结束时间排序，总选与此前兼容且结束最早的活动。',
    '任意最优方案第一项可以替换为结束最早项而不影响后续可行性，固定该项后对剩余活动归纳。','O(n log n) 时间，O(n) 空间。',
    '''n=int(input()); a=[]
for _ in range(n):
    s,e=map(int,input().split()); a.append((e,s))
end=-1; ans=0
for e,s in sorted(a):
    if s>=end: ans+=1; end=e
print(ans)
''',[('4\n1 3\n2 4\n3 5\n5 6\n','3\n'),('2\n0 10\n1 2\n','1\n'),('1\n0 1\n','1\n')])
add('lca','最近公共祖先',lg('P3379'),'树,倍增',
    '给定根为 1 的树，回答两点最近公共祖先。','输入 n q，随后 n-1 条边，再 q 行 u v；逐行输出祖先编号。','1≤n,q≤100000。',
    '迭代遍历建立深度与父亲，倍增表记录 2^k 祖先。先拉齐深度，再同时从高位向上跳到祖先不同的最近位置。',
    '拉齐不越过共同祖先；同步跳跃只在两祖先不同时进行，始终位于 LCA 下方。无法继续后两点父亲相同，正是最近公共祖先。','O((n+q) log n) 时间，O(n log n) 空间。',
    '''from collections import deque
n,q=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
L=n.bit_length(); up=[[0]*n for _ in range(L)]; dep=[-1]*n; dep[0]=0; queue=deque([0])
while queue:
    u=queue.popleft()
    for v in g[u]:
        if dep[v]<0: dep[v]=dep[u]+1; up[0][v]=u; queue.append(v)
for k in range(1,L):
    for v in range(n): up[k][v]=up[k-1][up[k-1][v]]
for _ in range(q):
    a,b=map(int,input().split()); a-=1; b-=1
    if dep[a]<dep[b]: a,b=b,a
    diff=dep[a]-dep[b]
    for k in range(L):
        if diff>>k&1: a=up[k][a]
    if a!=b:
        for k in range(L-1,-1,-1):
            if up[k][a]!=up[k][b]: a,b=up[k][a],up[k][b]
        a=up[0][a]
    print(a+1)
''',[('5 3\n1 2\n1 3\n2 4\n2 5\n4 5\n3 4\n2 4\n','2\n1\n2\n'),('1 1\n1 1\n','1\n'),('2 1\n1 2\n2 2\n','2\n')])
add('trie','前缀单词计数',lg('P8306'),'字符串,Trie',
    '给定单词集合，重复词分别计数，回答有多少单词以某查询串开头。','输入 n q，随后 n 行单词和 q 行前缀；逐行输出数量。','1≤n,q≤100000，单词/查询均为非空小写字母串，总长度≤300000。',
    '插入时沿 Trie 路径给每个节点计数加一，查询沿前缀走到节点，读出计数。',
    '经过某节点的插入路径恰好是含该节点前缀的单词，节点计数因此与要求完全相同。缺失边则没有匹配。','O(总字符数) 时间与空间。',
    '''n,q=map(int,input().split()); nodes=[{}]; count=[0]
for _ in range(n):
    u=0
    for c in input().strip():
        if c not in nodes[u]: nodes[u][c]=len(nodes); nodes.append({}); count.append(0)
        u=nodes[u][c]; count[u]+=1
for _ in range(q):
    u=0
    for c in input().strip():
        if c not in nodes[u]: u=-1; break
        u=nodes[u][c]
    print(0 if u<0 else count[u])
''',[('3 3\napple\napp\napt\nap\napp\nb\n','3\n2\n0\n'),('2 1\na\na\na\n','2\n'),('1 1\na\naa\n','0\n')])
add('bipartite','二分图判定',lc('is-graph-bipartite'),'图论,染色',
    '判断无向图能否用两种颜色染点，使每条边两端颜色不同，允许不连通。','输入 n m，随后 m 行无向边；输出 YES 或 NO。','1≤n≤100000，0≤m≤200000。',
    '对每个未染色分量 BFS，邻居染相反颜色，发现同色边即失败。',
    '连通分量内顶点颜色由根与路径奇偶决定，若发现同色边则存在奇环；若无冲突则已经构造出合法二染色。','O(n+m) 时间与空间。',
    '''from collections import deque
n,m=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(m):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
c=[-1]*n; ok=True
for s in range(n):
    if c[s]>=0: continue
    c[s]=0; q=deque([s])
    while q:
        u=q.popleft()
        for v in g[u]:
            if c[v]<0: c[v]=c[u]^1; q.append(v)
            elif c[v]==c[u]: ok=False
print('YES' if ok else 'NO')
''',[('3 3\n1 2\n2 3\n3 1\n','NO\n'),('4 2\n1 2\n3 4\n','YES\n'),('1 1\n1 1\n','NO\n')])


# 与开发环境已有的 12 道题保持 slug / 输入输出兼容，避免导入产生重复题。
MATCHES = {
    'two-sum': ('pair-sum-index','两数目标和'),
    'valid-parentheses': ('balanced-bracket-sequence','括号序列检查'),
    'max-subarray': ('maximum-contiguous-sum','最大连续子段'),
    'merge-intervals': ('merge-overlapping-intervals','合并重叠区间'),
    'maze': ('grid-shortest-steps','网格中的最短步数'),
    'lis': ('longest-increasing-subsequence','最长严格递增子序列'),
    'knapsack': ('zero-one-capacity-pack','有限容量装载'),
    'network-delay': ('nonnegative-single-source-shortest-path','非负权单源最短路'),
    'course-order': ('dependency-topological-order','任务依赖排序'),
    'province-count': ('dynamic-connectivity-queries','动态图连通查询'),
}
for entry in ENTRIES:
    if entry['slug'] in MATCHES:
        old = entry['slug']; entry['slug'],entry['title'] = MATCHES[old]
        if old == 'merge-intervals':
            entry.update(task='合并有交集的闭区间，端点相接也合并，输出合并后的全部区间。',
                         fmt='输入 n，随后 n 行 l r；先输出合并后的区间数，再按左端递增逐行输出左右端。',
                         code='''n=int(input()); a=sorted(tuple(map(int,input().split())) for _ in range(n)); out=[]
for l,r in a:
    if not out or l>out[-1][1]: out.append([l,r])
    else: out[-1][1]=max(out[-1][1],r)
print(len(out))
for l,r in out: print(l,r)
''', cases=[('4\n1 3\n2 6\n8 10\n15 18\n','3\n1 6\n8 10\n15 18\n'),('2\n1 2\n2 3\n','1\n1 3\n'),('1\n-1 -1\n','1\n-1 -1\n')])
        elif old == 'network-delay':
            entry['task']='给定非负权有向图，求源点 s 到终点 t 的最短距离，不可达输出 -1。'
            entry['fmt']='输入 n m s t，随后 m 行 u v w；输出距离。'
            entry['code']=entry['code'].replace('n,m,s=map','n,m,s,t=map').replace("print(-1 if max(d)==float('inf') else max(d))", "print(-1 if d[t-1]==float('inf') else d[t-1])")
            entry['proof']='非负边权保证每次堆中取出的最小有效距离已经最优；松弛保持距离为已发现路径的最小值，最终得到 s 到所有可达点的最短距离，读取 t 即可。'
            entry['cases']=[('4 3 2 4\n2 1 1\n2 3 1\n3 4 1\n','2\n'),('2 0 1 2\n','-1\n'),('1 0 1 1\n','0\n')]
        elif old == 'course-order':
            entry['task']='有向依赖 u v 表示 u 必须先于 v，输出字典序最小的拓扑序，有环输出 IMPOSSIBLE。'
            entry['fmt']='输入 n m，再 m 行 u v；输出 n 个任务编号或 IMPOSSIBLE。'
            entry['idea']='Kahn 算法用小根堆保存入度为零顶点，每次取编号最小者，删除出边。'
            entry['proof']='入度零点恰为当前可选任务，选最小者使输出的第一个不同位置最小；贪心归纳得到字典序最小拓扑序。环上点无法全部移除。'
            entry['complexity']='O((n+m) log n) 时间，O(n+m) 空间。'
            entry['code']='''import heapq
n,m=map(int,input().split()); g=[[] for _ in range(n)]; d=[0]*n
for _ in range(m):
    u,v=map(int,input().split()); g[u-1].append(v-1); d[v-1]+=1
q=[i for i in range(n) if d[i]==0]; heapq.heapify(q); ans=[]
while q:
    u=heapq.heappop(q); ans.append(u+1)
    for v in g[u]:
        d[v]-=1
        if d[v]==0: heapq.heappush(q,v)
print(' '.join(map(str,ans)) if len(ans)==n else 'IMPOSSIBLE')
'''
            entry['cases']=[('3 1\n1 3\n','1 2 3\n'),('2 2\n1 2\n2 1\n','IMPOSSIBLE\n'),('1 0\n','1\n')]
        elif old == 'province-count':
            entry.update(task='初始 n 个点互不相连，U a b 合并集合，Q a b 查询连通性。',
                         fmt='输入 n q，随后 q 行 U/Q a b；查询输出 YES 或 NO。',
                         idea='路径压缩及按大小合并维护并查集，查询比较两点根。',
                         proof='初始各点独立，合并操作准确地取两个等价类的并集，路径压缩只改内部树形不改集合，因此根相同等价于已连接。',
                         source=lg('P3367'),
                         code='''n,q=map(int,input().split()); p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
for _ in range(q):
    op,u,v=input().split(); a,b=find(int(u)-1),find(int(v)-1)
    if op=='Q': print('YES' if a==b else 'NO')
    elif a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]
''',cases=[('3 4\nQ 1 2\nU 1 2\nQ 1 2\nQ 3 3\n','NO\nYES\nYES\n'),('1 1\nQ 1 1\n','YES\n'),('2 3\nU 1 2\nU 2 1\nQ 2 1\n','YES\n')])
    elif entry['slug']=='move-zeroes':
        entry.update(slug='word-frequency-order', title='单词频次排序',source=lc('top-k-frequent-words'),tags=['哈希表','排序','字符串'],
                     task='统计一行小写单词的出现次数，按次数降序，同次数按字典序升序，输出所有不同单词。',
                     fmt='输入一行以单个空格分隔的单词；每行输出单词及出现次数。',limits='1≤总字符数≤200000。',
                     idea='哈希计数后按 (-频次, 单词) 的二元组排序。',
                     proof='哈希表准确累计每个单词频次，排序的首关键字保证次数降序，次关键字保证同频次字典序，因此完整满足输出规则。',
                     complexity='O(L+k log k) 次字符/单词操作，O(L) 空间；L 为输入长度，k 为不同词数，字符串比较还计入公共前缀长度。',
                     code='''from collections import Counter
counts=Counter(input().split())
for word,count in sorted(counts.items(),key=lambda x:(-x[1],x[0])): print(word,count)
''',cases=[('a b a c b a\n','a 3\nb 2\nc 1\n'),('z a\n','a 1\nz 1\n'),('hello\n','hello 1\n')])
    elif entry['slug']=='sorted-squares':
        entry.update(slug='static-range-minimum-query', title='区间最小值查询',source=lg('P3865'),tags=['ST表','倍增','数据结构'],
                     task='给定不可修改整数数组，回答若干闭区间最小值查询。这是 RMQ 模板的最小值版本。',
                     fmt='输入 n q 和数组，再 q 行 l r；每行输出区间最小值，下标从 1 开始。',limits='1≤n,q≤100000，|a_i|≤10^9。',
                     idea='ST 表保存每个起点长度为 2^k 的区间最小值，查询用两段覆盖，允许重叠。',
                     proof='两个半长区间的最小值可合并出完整区间。查询选 k=floor(log2(length))，首尾两段覆盖全部查询区间；重复取最小值不改变结果。',
                     complexity='O(n log n+q) 时间，O(n log n) 空间。',
                     code='''n,q=map(int,input().split()); a=list(map(int,input().split())); table=[a]; k=1
while 1<<k<=n:
    prev=table[-1]; half=1<<(k-1); table.append([min(prev[i],prev[i+half]) for i in range(n-(1<<k)+1)]); k+=1
for _ in range(q):
    l,r=map(int,input().split()); k=(r-l+1).bit_length()-1; print(min(table[k][l-1],table[k][r-(1<<k)]))
''',cases=[('4 3\n3 -1 4 2\n1 4\n3 4\n2 2\n','-1\n2\n-1\n'),('1 1\n7\n1 1\n','7\n'),('2 1\n5 5\n1 2\n','5\n')])


def build():
    assert len(ENTRIES) == 50, len(ENTRIES)
    manifest = []
    for entry in ENTRIES:
        path = ROOT / entry['slug']
        path.mkdir(parents=True, exist_ok=True)
        code = entry['code']
        (path / 'solution.py').write_text(code)
        cases = []
        for index, (stdin, expected) in enumerate(entry['cases'], 1):
            run = subprocess.run([sys.executable, str(path / 'solution.py')], input=stdin,
                                 capture_output=True, text=True, timeout=5, check=True)
            assert run.stdout.split() == expected.split(), (entry['slug'], index, run.stdout, expected)
            cases.append({'input': stdin, 'output': expected})
        for stdin in generated_inputs(entry['slug']):
            run = subprocess.run([sys.executable, str(path / 'solution.py')], input=stdin,
                                 capture_output=True, text=True, timeout=10, check=True)
            cases.append({'input': stdin, 'output': run.stdout})
        (path / 'tests.json').write_text(json.dumps(cases, ensure_ascii=False, indent=2) + '\n')
        sample = cases[0]
        statement = (f"# {entry['title']}\n\n{entry['task']}\n\n## 输入与输出\n\n{entry['fmt']}\n\n"
                     f"## 数据范围\n\n{entry['limits']}\n\n## 样例输入\n\n```text\n{sample['input']}```\n\n"
                     f"## 样例输出\n\n```text\n{sample['output']}```\n\n## 来源\n\n"
                     f"经典题型参考：{entry['source']}。本项目重新表述并适配标准输入输出，范围或要求可能与原题不同。\n")
        (path / 'statement.md').write_text(statement)
        editorial = (f"# {entry['title']}：标准题解\n\n## 题目与来源\n\n{entry['task']}\n\n"
                     f"来源参考：{entry['source']}。以下讲解、参考代码与测试数据为本项目编写。\n\n"
                     f"算法标签：{'、'.join(entry['tags'])}\n\n## 算法思路\n\n{entry['idea']}\n\n"
                     f"## 正确性说明\n\n{entry['proof']}\n\n## 复杂度\n\n{entry['complexity']}\n\n"
                     f"## 边界与易错点\n\n数据约束：{entry['limits']}\n\n"
                     f"回归样例除普通情况外包括：\n\n" + '\n'.join(
                         f"- 输入 `{case['input'].strip().replace(chr(10),' / ')}`，期望输出 `{case['output'].strip().replace(chr(10),' / ')}`。"
                         for case in cases[1:3]) + f"\n\n## Python 3 参考实现\n\n```python\n{code}```\n")
        (path / 'editorial.md').write_text(editorial)
        manifest.append({key: entry[key] for key in ('slug','title','source','tags')})
    (ROOT / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
    print(f'已校验并生成 {len(manifest)} 题，150 组已知答案及 400 组生成测试。')


if __name__ == '__main__':
    build()
