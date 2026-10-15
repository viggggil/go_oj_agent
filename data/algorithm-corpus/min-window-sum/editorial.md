# 最短达标子数组：标准题解

## 题目与来源

正整数数组中，求和至少 target 的最短非空连续子数组长度。不存在输出 0。

来源参考：https://leetcode.cn/problems/minimum-size-subarray-sum/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：数组、滑动窗口

## 算法思路

扩展右端累加，和达标时持续收缩左端并更新最短长度。正数保证窗口和单调。

## 正确性说明

每个右端对应的合法起点是一段前缀，收缩到刚好不合法前已检查最短合法窗口。每个元素最多进入和离开一次。

## 复杂度

O(n) 时间，O(1) 额外空间。

## 边界与易错点

数据约束：1≤n≤200000，1≤a_i≤10^6，1≤target≤10^15。

回归样例除普通情况外包括：

- 输入 `3 100 / 1 2 3`，期望输出 `0`。
- 输入 `1 5 / 5`，期望输出 `1`。

## Python 3 参考实现

```python
n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; total=0; ans=n+1
for r,x in enumerate(a):
    total+=x
    while total>=t:
        ans=min(ans,r-l+1); total-=a[l]; l+=1
print(0 if ans==n+1 else ans)
```
