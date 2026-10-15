# 最长严格递增子序列：标准题解

## 题目与来源

求可不连续的严格递增子序列最大长度。

来源参考：https://leetcode.cn/problems/longest-increasing-subsequence/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：动态规划、二分查找

## 算法思路

tails[k] 保存长度 k+1 的递增子序列最小末尾，二分首个不小于 x 的位置替换。

## 正确性说明

同长度末尾越小越利于延伸，替换不丢失任何可行长度；只有 x 大于所有末尾时才能创造新长度。由归纳得到 tails 长度等于最优长度。

## 复杂度

O(n log n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，|a_i|≤10^9。

回归样例除普通情况外包括：

- 输入 `3 / 2 2 2`，期望输出 `1`。
- 输入 `1 / -5`，期望输出 `1`。

## Python 3 参考实现

```python
from bisect import bisect_left
n=int(input()); a=list(map(int,input().split())); tails=[]
for x in a:
    k=bisect_left(tails,x)
    if k==len(tails): tails.append(x)
    else: tails[k]=x
print(len(tails))
```
