# 网格路径数：标准题解

## 题目与来源

从左上到右下只能向右或向下，1 为障碍、0 可走，求路径数模 1000000007。

来源参考：https://leetcode.cn/problems/unique-paths-ii/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：网格、动态规划

## 算法思路

滚动数组存每列到达方式，障碍清零，否则从上方和左方转移。

## 正确性说明

每条到当前格的路径最后一步恰好从上或左而来，两类互斥；障碍无路径。初始化起点前的虚拟路径为 1。

## 复杂度

O(nm) 时间，O(m) 空间。

## 边界与易错点

数据约束：1≤n,m≤500。

回归样例除普通情况外包括：

- 输入 `1 1 / 1`，期望输出 `0`。
- 输入 `1 1 / 0`，期望输出 `1`。

## Python 3 参考实现

```python
n,m=map(int,input().split()); dp=[0]*m; dp[0]=1
for _ in range(n):
    row=list(map(int,input().split()))
    for j,x in enumerate(row):
        if x: dp[j]=0
        elif j: dp[j]=(dp[j]+dp[j-1])%1000000007
print(dp[-1])
```
