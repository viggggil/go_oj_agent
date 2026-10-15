# 有根树最大深度：标准题解

## 题目与来源

给定以 1 为根的无向树，求最大深度，根深度为 1。本题将二叉树经典题推广到一般树。

来源参考：https://leetcode.cn/problems/maximum-depth-of-binary-tree/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：树、广度优先搜索

## 算法思路

从根 BFS，孩子深度为父亲深度加一，用访问标记避免沿无向边返回。

## 正确性说明

树上从根到每点路径唯一，BFS 为每点赋予该路径顶点数，所有深度最大值就是答案。

## 复杂度

O(n) 时间及空间。

## 边界与易错点

数据约束：1≤n≤200000。

回归样例除普通情况外包括：

- 输入 `1`，期望输出 `1`。
- 输入 `3 / 1 2 / 1 3`，期望输出 `2`。

## Python 3 参考实现

```python
from collections import deque
n=int(input()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
d=[0]*n; d[0]=1; q=deque([0])
while q:
    u=q.popleft()
    for v in g[u]:
        if not d[v]: d[v]=d[u]+1; q.append(v)
print(max(d))
```
