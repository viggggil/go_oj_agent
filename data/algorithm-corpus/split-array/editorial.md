# 最小化最大分段和：标准题解

## 题目与来源

把正整数数组分成恰好 k 个非空连续段，使最大段和最小。

来源参考：https://leetcode.cn/problems/split-array-largest-sum/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：二分答案、贪心

## 算法思路

二分容量 C，贪心尽量填满每段，统计所需段数。若段数≤k，可拆分成恰好 k 段。

## 正确性说明

贪心第一段覆盖不短于任何可行方案第一段，归纳可知其段数最少。可行容量具有单调性，因此二分首个可行值正确。

## 复杂度

O(n log(sum a)) 时间，O(1) 额外空间。

## 边界与易错点

数据约束：1≤k≤n≤100000，1≤a_i≤10^9。

回归样例除普通情况外包括：

- 输入 `3 3 / 1 2 3`，期望输出 `3`。
- 输入 `1 1 / 9`，期望输出 `9`。

## Python 3 参考实现

```python
n,k=map(int,input().split()); a=list(map(int,input().split())); l=max(a); r=sum(a)
while l<r:
    m=(l+r)//2; cnt=1; s=0
    for x in a:
        if s+x>m: cnt+=1; s=0
        s+=x
    if cnt<=k: r=m
    else: l=m+1
print(l)
```
