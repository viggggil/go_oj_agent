# 和为 K 的子数组数：标准题解

## 题目与来源

统计元素和等于 k 的非空连续子数组数，数组可以有负数。

来源参考：https://leetcode.cn/problems/subarray-sum-equals-k/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：前缀和、哈希表

## 算法思路

扫描前缀和 s，累加此前前缀 s-k 的出现次数，再登记 s。初始前缀 0 出现一次。

## 正确性说明

区间和为 k 等价于两端前缀差为 k，查找历史前缀恰好枚举每个以当前项结尾的合法区间，且不会计入空区间。

## 复杂度

期望 O(n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，|a_i|≤10^6，|k|≤10^12。

回归样例除普通情况外包括：

- 输入 `3 0 / 0 0 0`，期望输出 `6`。
- 输入 `3 0 / 1 -1 1`，期望输出 `2`。

## Python 3 参考实现

```python
n,k=map(int,input().split()); a=list(map(int,input().split())); count={0:1}; s=ans=0
for x in a:
    s+=x; ans+=count.get(s-k,0); count[s]=count.get(s,0)+1
print(ans)
```
