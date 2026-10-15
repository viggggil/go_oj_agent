# 有限容量装载：标准题解

## 题目与来源

每件物品最多取一次，在容量限制内最大化总价值。

来源参考：https://www.luogu.com.cn/problem/P1048。以下讲解、参考代码与测试数据为本项目编写。

算法标签：动态规划、零一背包

## 算法思路

一维 dp 按容量降序更新，保证当前物品只使用一次。

## 正确性说明

更新 dp[c] 时较小容量尚是上一轮值，故转移恰好表示不取或取当前物品一次。若正序会错误地重复选取。

## 复杂度

O(n·capacity) 时间，O(capacity) 空间。

## 边界与易错点

数据约束：1≤n≤100，0≤capacity≤10000，1≤weight≤10000，0≤value≤10^6。

回归样例除普通情况外包括：

- 输入 `1 4 / 2 3`，期望输出 `3`。
- 输入 `1 0 / 1 9`，期望输出 `0`。

## Python 3 参考实现

```python
n,c=map(int,input().split()); dp=[0]*(c+1)
for _ in range(n):
    w,v=map(int,input().split())
    for j in range(c,w-1,-1): dp[j]=max(dp[j],dp[j-w]+v)
print(dp[c])
```
