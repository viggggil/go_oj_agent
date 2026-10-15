# 合并果子：标准题解

## 题目与来源

每次合并两堆果子，代价为两堆总重量，求全部合并成一堆的最小总代价。

来源参考：https://www.luogu.com.cn/problem/P1090。以下讲解、参考代码与测试数据为本项目编写。

算法标签：堆、贪心

## 算法思路

每次从小根堆取最小两项合并，再放回。

## 正确性说明

最优合并树可通过交换让最小的两个权重位于最深同父叶子，合并它们后问题缩小；归纳得到贪心最优。

## 复杂度

O(n log n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，1≤重量≤10^9。

回归样例除普通情况外包括：

- 输入 `1 / 8`，期望输出 `0`。
- 输入 `4 / 1 1 1 1`，期望输出 `8`。

## Python 3 参考实现

```python
import heapq
n=int(input()); a=list(map(int,input().split())); heapq.heapify(a); ans=0
while len(a)>1:
    x=heapq.heappop(a)+heapq.heappop(a); ans+=x; heapq.heappush(a,x)
print(ans)
```
