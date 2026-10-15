# 最少硬币数：标准题解

## 题目与来源

每种正整数面值硬币无限，求凑出金额 amount 最少硬币数，无法凑出输出 -1。

来源参考：https://leetcode.cn/problems/coin-change/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：动态规划、完全背包

## 算法思路

dp[0]=0，其余无穷。按金额递增，枚举最后一枚硬币，更新 dp[s]=min(dp[s-c]+1)。

## 正确性说明

正面值保证递推依赖更小金额，任意最优方案删除最后一枚硬币后仍须最优，否则可以替换得到更短方案。

## 复杂度

O(n·amount) 时间，O(amount) 空间。

## 边界与易错点

数据约束：1≤n≤50，0≤amount≤10000，1≤面值≤10000。

回归样例除普通情况外包括：

- 输入 `1 3 / 2`，期望输出 `-1`。
- 输入 `1 0 / 7`，期望输出 `0`。

## Python 3 参考实现

```python
n,m=map(int,input().split()); coins=list(map(int,input().split())); dp=[0]+[m+1]*m
for s in range(1,m+1):
    for c in coins:
        if c<=s: dp[s]=min(dp[s],dp[s-c]+1)
print(dp[m] if dp[m]<=m else -1)
```
