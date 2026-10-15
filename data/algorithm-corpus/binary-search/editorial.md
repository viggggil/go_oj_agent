# 二分查找首个目标：标准题解

## 题目与来源

在非递减数组中求 target 首次出现的位置（1 开始），不存在输出 -1。

来源参考：https://leetcode.cn/problems/find-first-and-last-position-of-element-in-sorted-array/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：数组、二分查找

## 算法思路

在半开区间 [l,r) 查找首个大于等于 target 的位置，再检查是否相等。

## 正确性说明

循环保持 l 左侧都小于 target，r 及右侧都不小于 target。区间为空时 l 就是唯一分界点。

## 复杂度

O(log n) 时间，O(1) 空间。

## 边界与易错点

数据约束：1≤n≤200000，|a_i|、|target|≤10^9。

回归样例除普通情况外包括：

- 输入 `2 9 / 1 2`，期望输出 `-1`。
- 输入 `1 -2 / -2`，期望输出 `1`。

## Python 3 参考实现

```python
n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; r=n
while l<r:
    m=(l+r)//2
    if a[m]<t: l=m+1
    else: r=m
print(l+1 if l<n and a[l]==t else -1)
```
