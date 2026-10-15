# 树的直径：标准题解

## 题目与来源

求无向树中最长简单路径的边数；由经典二叉树直径题推广。

来源参考：https://leetcode.cn/problems/diameter-of-binary-tree/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：树、图论

## 算法思路

从任意点 BFS 找最远点 u，再从 u BFS，最远距离为直径。

## 正确性说明

在树的唯一路径性质下，任意点的某个最远点是某条直径端点；否则沿直径向更远端延伸会得到更远点，矛盾。第二次遍历即得到最长路径。

## 复杂度

O(n) 时间及空间。

## 边界与易错点

数据约束：1≤n≤200000。

回归样例除普通情况外包括：

- 输入 `1`，期望输出 `0`。
- 输入 `4 / 1 2 / 2 3 / 3 4`，期望输出 `3`。

## Python 3 参考实现

```python
from collections import deque
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
```
