# 合并重叠区间：标准题解

## 题目与来源

合并有交集的闭区间，端点相接也合并，输出合并后的全部区间。

来源参考：https://leetcode.cn/problems/merge-intervals/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：排序、贪心

## 算法思路

按左端排序，维护当前右端。新左端不大于当前右端则延长，否则开启新区间。

## 正确性说明

排序后若新左端超过当前右端，后续左端更大，当前区间不可能再与后续合并；否则两者并集仍是一个区间。

## 复杂度

O(n log n) 时间，O(n) 排序空间。

## 边界与易错点

数据约束：1≤n≤200000，-10^9≤l≤r≤10^9。

回归样例除普通情况外包括：

- 输入 `2 / 1 2 / 2 3`，期望输出 `1 / 1 3`。
- 输入 `1 / -1 -1`，期望输出 `1 / -1 -1`。

## Python 3 参考实现

```python
n=int(input()); a=sorted(tuple(map(int,input().split())) for _ in range(n)); out=[]
for l,r in a:
    if not out or l>out[-1][1]: out.append([l,r])
    else: out[-1][1]=max(out[-1][1],r)
print(len(out))
for l,r in out: print(l,r)
```
