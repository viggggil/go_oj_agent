# 最近公共祖先：标准题解

## 题目与来源

给定根为 1 的树，回答两点最近公共祖先。

来源参考：https://www.luogu.com.cn/problem/P3379。以下讲解、参考代码与测试数据为本项目编写。

算法标签：树、倍增

## 算法思路

迭代遍历建立深度与父亲，倍增表记录 2^k 祖先。先拉齐深度，再同时从高位向上跳到祖先不同的最近位置。

## 正确性说明

拉齐不越过共同祖先；同步跳跃只在两祖先不同时进行，始终位于 LCA 下方。无法继续后两点父亲相同，正是最近公共祖先。

## 复杂度

O((n+q) log n) 时间，O(n log n) 空间。

## 边界与易错点

数据约束：1≤n,q≤100000。

回归样例除普通情况外包括：

- 输入 `1 1 / 1 1`，期望输出 `1`。
- 输入 `2 1 / 1 2 / 2 2`，期望输出 `2`。

## Python 3 参考实现

```python
from collections import deque
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
```
