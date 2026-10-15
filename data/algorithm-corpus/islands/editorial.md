# 岛屿数量：标准题解

## 题目与来源

统计 0/1 网格中 1 的四连通块个数。

来源参考：https://leetcode.cn/problems/number-of-islands/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：网格、广度优先搜索

## 算法思路

扫描未访问陆地时计数加一，用迭代 BFS 清除整个连通块，避免递归栈溢出。

## 正确性说明

BFS 恰好访问起点所在连通块；每个连通块第一次被扫描时贡献一次，之后均已标记，不会漏计或重复。

## 复杂度

O(nm) 时间，O(nm) 空间。

## 边界与易错点

数据约束：1≤n,m≤500。

回归样例除普通情况外包括：

- 输入 `1 3 / 000`，期望输出 `0`。
- 输入 `2 2 / 11 / 11`，期望输出 `1`。

## Python 3 参考实现

```python
from collections import deque
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
```
