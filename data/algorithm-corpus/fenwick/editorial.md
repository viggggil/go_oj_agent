# 单点加区间和：标准题解

## 题目与来源

维护数组，操作 1 i delta 把 a_i 加 delta，操作 2 l r 查询区间和。

来源参考：https://www.luogu.com.cn/problem/P3374。以下讲解、参考代码与测试数据为本项目编写。

算法标签：树状数组、数据结构

## 算法思路

树状数组保存 lowbit 长度区间和，单点加向上更新，前缀和向下累加。

## 正确性说明

前缀查询把 [1,x] 分解成互不相交的 lowbit 区间；单点更新遍历所有包含该位置的维护区间，因此查询始终正确。

## 复杂度

O((n+q) log n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n,q≤200000，初值及增量绝对值≤10^9。

回归样例除普通情况外包括：

- 输入 `1 1 / -1 / 2 1 1`，期望输出 `-1`。
- 输入 `2 2 / 0 0 / 1 1 9 / 2 1 2`，期望输出 `9`。

## Python 3 参考实现

```python
import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); bit=[0]*(n+1)
def add(i,x):
    while i<=n: bit[i]+=x; i+=i&-i
def pref(i):
    s=0
    while i: s+=bit[i]; i-=i&-i
    return s
for i in range(1,n+1): add(i,next(it))
for _ in range(q):
    op,l,r=next(it),next(it),next(it)
    if op==1: add(l,r)
    else: print(pref(r)-pref(l-1))
```
