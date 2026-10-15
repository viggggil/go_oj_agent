# 编辑距离：标准题解

## 题目与来源

插入、删除、替换一个字符各花费 1，求把第一个字符串变成第二个的最小代价。

来源参考：https://leetcode.cn/problems/edit-distance/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：字符串、动态规划

## 算法思路

dp[i][j] 表示两个前缀距离，枚举最后一次插入、删除、替换/匹配，滚动行优化。

## 正确性说明

任意转换序列可按末字符处理方式归入这三类，取最小且子问题最优即得全局最优。空前缀到长度 j 的距离是 j。

## 复杂度

O(nm) 时间，O(m) 空间。

## 边界与易错点

数据约束：字符串长度均为 1 至 1000。

回归样例除普通情况外包括：

- 输入 `a / a`，期望输出 `0`。
- 输入 `abc / x`，期望输出 `3`。

## Python 3 参考实现

```python
a=input().strip(); b=input().strip(); dp=list(range(len(b)+1))
for i,x in enumerate(a,1):
    row=[i]
    for j,y in enumerate(b,1): row.append(min(dp[j]+1,row[-1]+1,dp[j-1]+(x!=y)))
    dp=row
print(dp[-1])
```
