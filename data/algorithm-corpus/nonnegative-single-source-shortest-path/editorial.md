# 非负权单源最短路：标准题解

## 题目与来源

给定非负权有向图，求源点 s 到终点 t 的最短距离，不可达输出 -1。

来源参考：https://leetcode.cn/problems/network-delay-time/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：图论、最短路

## 算法思路

使用小根堆 Dijkstra，跳过过时堆项，最后检查所有距离。

## 正确性说明

非负边权保证每次堆中取出的最小有效距离已经最优；松弛保持距离为已发现路径的最小值，最终得到 s 到所有可达点的最短距离，读取 t 即可。

## 复杂度

O((n+m) log(n+m)) 时间，O(n+m) 空间。

## 边界与易错点

数据约束：1≤n≤100000，0≤m≤200000，0≤w≤10^9。

回归样例除普通情况外包括：

- 输入 `2 0 1 2`，期望输出 `-1`。
- 输入 `1 0 1 1`，期望输出 `0`。

## Python 3 参考实现

```python
import heapq
n,m,s,t=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(m):
    u,v,w=map(int,input().split()); g[u-1].append((v-1,w))
d=[float('inf')]*n; d[s-1]=0; q=[(0,s-1)]
while q:
    du,u=heapq.heappop(q)
    if du!=d[u]: continue
    for v,w in g[u]:
        if du+w<d[v]: d[v]=du+w; heapq.heappush(q,(d[v],v))
print(-1 if d[t-1]==float('inf') else d[t-1])
```
