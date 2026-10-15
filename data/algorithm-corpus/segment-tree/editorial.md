# 区间加与区间和：标准题解

## 题目与来源

支持 1 l r delta 区间加，以及 2 l r 查询区间和。

来源参考：https://www.luogu.com.cn/problem/P3372。以下讲解、参考代码与测试数据为本项目编写。

算法标签：线段树、数据结构

## 算法思路

线段树节点存区间和及懒标记，整段更新把和加 delta×长度，部分访问前下推。

## 正确性说明

节点和包含本段所有更新，懒标记代表尚未传到孩子的共同增量。下推保持父子和一致，分治覆盖恰好查询/更新区间。

## 复杂度

O((n+q) log n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n,q≤100000，初值及增量绝对值≤10^6。

回归样例除普通情况外包括：

- 输入 `1 1 / 7 / 2 1 1`，期望输出 `7`。
- 输入 `2 2 / 0 0 / 1 1 1 3 / 2 1 2`，期望输出 `3`。

## Python 3 参考实现

```python
import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); a=[next(it) for _ in range(n)]; tr=[0]*(4*n); lazy=[0]*(4*n)
def build(p,l,r):
    if l==r: tr[p]=a[l]; return
    m=(l+r)//2; build(p*2,l,m); build(p*2+1,m+1,r); tr[p]=tr[p*2]+tr[p*2+1]
def apply(p,l,r,x): tr[p]+=x*(r-l+1); lazy[p]+=x
def push(p,l,r):
    if lazy[p] and l<r:
        m=(l+r)//2; apply(p*2,l,m,lazy[p]); apply(p*2+1,m+1,r,lazy[p]); lazy[p]=0
def change(p,l,r,ql,qr,x):
    if ql<=l and r<=qr: apply(p,l,r,x); return
    push(p,l,r); m=(l+r)//2
    if ql<=m: change(p*2,l,m,ql,qr,x)
    if qr>m: change(p*2+1,m+1,r,ql,qr,x)
    tr[p]=tr[p*2]+tr[p*2+1]
def query(p,l,r,ql,qr):
    if ql<=l and r<=qr: return tr[p]
    push(p,l,r); m=(l+r)//2; ans=0
    if ql<=m: ans+=query(p*2,l,m,ql,qr)
    if qr>m: ans+=query(p*2+1,m+1,r,ql,qr)
    return ans
build(1,0,n-1)
for _ in range(q):
    op,l,r=next(it),next(it)-1,next(it)-1
    if op==1: change(1,0,n-1,l,r,next(it))
    else: print(query(1,0,n-1,l,r))
```
