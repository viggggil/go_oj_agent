# 二分图判定：标准题解

## 题目与来源

判断无向图能否用两种颜色染点，使每条边两端颜色不同，允许不连通。

来源参考：https://leetcode.cn/problems/is-graph-bipartite/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：图论、染色

## 算法思路

对每个未染色分量 BFS，邻居染相反颜色，发现同色边即失败。

## 正确性说明

连通分量内顶点颜色由根与路径奇偶决定，若发现同色边则存在奇环；若无冲突则已经构造出合法二染色。

## 复杂度

O(n+m) 时间与空间。

## 边界与易错点

数据约束：1≤n≤100000，0≤m≤200000。

回归样例除普通情况外包括：

- 输入 `4 2 / 1 2 / 3 4`，期望输出 `YES`。
- 输入 `1 1 / 1 1`，期望输出 `NO`。

## Python 3 参考实现

```python
from collections import deque
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
```
