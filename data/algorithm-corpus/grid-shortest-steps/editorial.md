# 网格中的最短步数：标准题解

## 题目与来源

网格中 0 可走、1 障碍，四方向移动，求左上角到右下角最少步数，不可达为 -1。这是网格搜索的改编练习。

来源参考：https://www.luogu.com.cn/problem/P1141。以下讲解、参考代码与测试数据为本项目编写。

算法标签：广度优先搜索、网格

## 算法思路

无权图 BFS，入队时标记访问，距离加一。起点或终点为障碍直接失败。

## 正确性说明

BFS 按路径长度递增分层，每个格首次到达时已经是最短路径；队列耗尽仍未到达则不可达。

## 复杂度

O(nm) 时间及空间。

## 边界与易错点

数据约束：1≤n,m≤500。

回归样例除普通情况外包括：

- 输入 `1 1 / 0`，期望输出 `0`。
- 输入 `2 2 / 01 / 10`，期望输出 `-1`。

## Python 3 参考实现

```python
from collections import deque
n,m=map(int,input().split()); a=[input().strip() for _ in range(n)]; d=[[-1]*m for _ in range(n)]; q=deque()
if a[0][0]=='0': d[0][0]=0; q.append((0,0))
while q:
    x,y=q.popleft()
    for dx,dy in ((1,0),(-1,0),(0,1),(0,-1)):
        u,v=x+dx,y+dy
        if 0<=u<n and 0<=v<m and a[u][v]=='0' and d[u][v]<0: d[u][v]=d[x][y]+1; q.append((u,v))
print(d[-1][-1])
```
