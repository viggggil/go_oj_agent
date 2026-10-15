# 最长公共子序列：标准题解

## 题目与来源

求两个小写字母字符串的最长公共子序列长度。

来源参考：https://leetcode.cn/problems/longest-common-subsequence/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：字符串、动态规划

## 算法思路

滚动行计算 dp。相等时取左上角加一，否则取上方和左方最大值。

## 正确性说明

末字符相同可把公共解延伸一位；不同时至少一边末字符不被同时选中，去掉其中一边即覆盖全部最优可能。

## 复杂度

O(nm) 时间，O(m) 空间。

## 边界与易错点

数据约束：每个字符串长度为 1 至 1000。

回归样例除普通情况外包括：

- 输入 `abc / def`，期望输出 `0`。
- 输入 `aaa / aa`，期望输出 `2`。

## Python 3 参考实现

```python
a=input().strip(); b=input().strip(); dp=[0]*(len(b)+1)
for x in a:
    prev=0
    for j,y in enumerate(b,1):
        old=dp[j]; dp[j]=prev+1 if x==y else max(dp[j],dp[j-1]); prev=old
print(dp[-1])
```
