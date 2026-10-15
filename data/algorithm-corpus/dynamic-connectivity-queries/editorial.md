# 动态图连通查询：标准题解

## 题目与来源

初始 n 个点互不相连，U a b 合并集合，Q a b 查询连通性。

来源参考：https://www.luogu.com.cn/problem/P3367。以下讲解、参考代码与测试数据为本项目编写。

算法标签：并查集、图论

## 算法思路

路径压缩及按大小合并维护并查集，查询比较两点根。

## 正确性说明

初始各点独立，合并操作准确地取两个等价类的并集，路径压缩只改内部树形不改集合，因此根相同等价于已连接。

## 复杂度

O((n+m) α(n)) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，0≤m≤300000。

回归样例除普通情况外包括：

- 输入 `1 1 / Q 1 1`，期望输出 `YES`。
- 输入 `2 3 / U 1 2 / U 2 1 / Q 2 1`，期望输出 `YES`。

## Python 3 参考实现

```python
n,q=map(int,input().split()); p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
for _ in range(q):
    op,u,v=input().split(); a,b=find(int(u)-1),find(int(v)-1)
    if op=='Q': print('YES' if a==b else 'NO')
    elif a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]
```
