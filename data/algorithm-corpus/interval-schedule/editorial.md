# 最多不重叠活动：标准题解

## 题目与来源

从活动 [start,end) 中选尽量多活动，时间不重叠，结束时刻等于下一活动开始允许。

来源参考：https://www.luogu.com.cn/problem/P1803。以下讲解、参考代码与测试数据为本项目编写。

算法标签：贪心、排序

## 算法思路

按结束时间排序，总选与此前兼容且结束最早的活动。

## 正确性说明

任意最优方案第一项可以替换为结束最早项而不影响后续可行性，固定该项后对剩余活动归纳。

## 复杂度

O(n log n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，0≤start<end≤10^9。

回归样例除普通情况外包括：

- 输入 `2 / 0 10 / 1 2`，期望输出 `1`。
- 输入 `1 / 0 1`，期望输出 `1`。

## Python 3 参考实现

```python
n=int(input()); a=[]
for _ in range(n):
    s,e=map(int,input().split()); a.append((e,s))
end=-1; ans=0
for e,s in sorted(a):
    if s>=end: ans+=1; end=e
print(ans)
```
