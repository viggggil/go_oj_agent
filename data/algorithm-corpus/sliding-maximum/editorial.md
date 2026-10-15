# 滑动窗口最大值：标准题解

## 题目与来源

输出所有长度为 k 的连续窗口最大值。

来源参考：https://leetcode.cn/problems/sliding-window-maximum/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：单调队列、滑动窗口

## 算法思路

双端队列存下标，队首过期即弹出，队尾不大于新值的元素永远不会成为最大值，可删除。

## 正确性说明

队列保留尚可能成为最大值的候选且值递减，队首总是当前窗口最大值。被新元素支配的位置更早过期，删除不影响未来。

## 复杂度

O(n) 时间，O(k) 空间。

## 边界与易错点

数据约束：1≤k≤n≤200000，|a_i|≤10^9。

回归样例除普通情况外包括：

- 输入 `2 1 / -2 1`，期望输出 `-2 1`。
- 输入 `3 3 / 4 4 4`，期望输出 `4`。

## Python 3 参考实现

```python
from collections import deque
n,k=map(int,input().split()); a=list(map(int,input().split())); q=deque(); ans=[]
for i,x in enumerate(a):
    while q and q[0]<=i-k: q.popleft()
    while q and a[q[-1]]<=x: q.pop()
    q.append(i)
    if i>=k-1: ans.append(a[q[0]])
print(*ans)
```
