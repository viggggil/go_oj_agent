# 任务依赖排序：标准题解

## 题目与来源

有向依赖 u v 表示 u 必须先于 v，输出字典序最小的拓扑序，有环输出 IMPOSSIBLE。

来源参考：https://leetcode.cn/problems/course-schedule/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：图论、拓扑排序

## 算法思路

Kahn 算法用小根堆保存入度为零顶点，每次取编号最小者，删除出边。

## 正确性说明

入度零点恰为当前可选任务，选最小者使输出的第一个不同位置最小；贪心归纳得到字典序最小拓扑序。环上点无法全部移除。

## 复杂度

O((n+m) log n) 时间，O(n+m) 空间。

## 边界与易错点

数据约束：1≤n≤200000，0≤m≤300000，顶点 1 至 n。

回归样例除普通情况外包括：

- 输入 `2 2 / 1 2 / 2 1`，期望输出 `IMPOSSIBLE`。
- 输入 `1 0`，期望输出 `1`。

## Python 3 参考实现

```python
import heapq
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
```
