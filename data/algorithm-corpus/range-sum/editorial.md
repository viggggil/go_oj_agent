# 静态区间求和：标准题解

## 题目与来源

回答若干闭区间 [l,r] 的元素和，下标从 1 开始。

来源参考：https://leetcode.cn/problems/range-sum-query-immutable/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：前缀和、数组

## 算法思路

令 p[0]=0，p[i]=p[i-1]+a_i，区间和等于 p[r]-p[l-1]。

## 正确性说明

两个前缀相减时前 l-1 项抵消，仅剩区间项。初始化 p[0] 覆盖从首项开始的查询。

## 复杂度

O(n+q) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n,q≤200000，|a_i|≤10^9。

回归样例除普通情况外包括：

- 输入 `1 1 / 0 / 1 1`，期望输出 `0`。
- 输入 `2 1 / 1000000000 1000000000 / 1 2`，期望输出 `2000000000`。

## Python 3 参考实现

```python
import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); p=[0]
for _ in range(n): p.append(p[-1]+next(it))
for _ in range(q):
    l=next(it); r=next(it); print(p[r]-p[l-1])
```
