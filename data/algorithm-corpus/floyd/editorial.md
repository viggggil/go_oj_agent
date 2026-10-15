# 多源最短路：标准题解

## 题目与来源

给定有向非负权图，回答点对之间最短距离，不可达输出 -1。

来源参考：https://www.luogu.com.cn/problem/P2910。以下讲解、参考代码与测试数据为本项目编写。

算法标签：图论、Floyd

## 算法思路

Floyd 按允许的中间顶点逐个扩展，d[i][j]=min(d[i][j],d[i][k]+d[k][j])。

## 正确性说明

允许前 k 个中间点的最短路径要么不用 k，要么经过 k 并分成两条只用前 k-1 个中间点的路径。k 必须在最外层。

## 复杂度

O(n³+q) 时间，O(n²) 空间。

## 边界与易错点

数据约束：1≤n≤100，0≤m≤10000，1≤q≤10000，0≤w≤10^6。

回归样例除普通情况外包括：

- 输入 `1 0 1 / 1 1`，期望输出 `0`。
- 输入 `2 2 1 / 1 2 8 / 1 2 3 / 1 2`，期望输出 `3`。

## Python 3 参考实现

```python
n,m,q=map(int,input().split()); inf=10**30; d=[[inf]*n for _ in range(n)]
for i in range(n): d[i][i]=0
for _ in range(m):
    u,v,w=map(int,input().split()); d[u-1][v-1]=min(d[u-1][v-1],w)
for k in range(n):
    for i in range(n):
        for j in range(n): d[i][j]=min(d[i][j],d[i][k]+d[k][j])
for _ in range(q):
    s,t=map(int,input().split()); print(-1 if d[s-1][t-1]==inf else d[s-1][t-1])
```
