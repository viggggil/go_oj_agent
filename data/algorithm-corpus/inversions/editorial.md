# 逆序对计数：标准题解

## 题目与来源

统计 i<j 且 a_i>a_j 的下标对数量。相等不算逆序。

来源参考：https://www.luogu.com.cn/problem/P1908。以下讲解、参考代码与测试数据为本项目编写。

算法标签：归并排序、分治

## 算法思路

归并排序，右边元素先取时累加左半尚未合并的元素数，相等时先取左边。

## 正确性说明

逆序对分为左内部、右内部和跨区间。递归计算前两类，合并时每个右元素恰好统计所有大于它的左元素，三类互斥穷尽。

## 复杂度

O(n log n) 时间，O(n) 辅助空间。

## 边界与易错点

数据约束：1≤n≤200000，|a_i|≤10^9。

回归样例除普通情况外包括：

- 输入 `3 / 2 2 2`，期望输出 `0`。
- 输入 `3 / 1 3 2`，期望输出 `1`。

## Python 3 参考实现

```python
n=int(input()); a=list(map(int,input().split()))
def solve(a):
    if len(a)<=1: return a,0
    m=len(a)//2; l,x=solve(a[:m]); r,y=solve(a[m:]); i=j=0; out=[]; ans=x+y
    while i<len(l) and j<len(r):
        if l[i]<=r[j]: out.append(l[i]); i+=1
        else: out.append(r[j]); j+=1; ans+=len(l)-i
    return out+l[i:]+r[j:],ans
print(solve(a)[1])
```
