# 最小生成树：标准题解

## 题目与来源

无向带权图中求连接所有顶点的最小总边权，图不连通输出 -1。

来源参考：https://www.luogu.com.cn/problem/P3366。以下讲解、参考代码与测试数据为本项目编写。

算法标签：图论、最小生成树、并查集

## 算法思路

Kruskal 按边权排序，并查集跳过形成环的边，选满 n-1 条即得到生成树。

## 正确性说明

每次跨越当前不同连通块选择的最小边满足割性质，可包含于某棵最小生成树。重复安全选择最终得到最优生成树；选不满说明不连通。

## 复杂度

O(m log m) 时间，O(n+m) 空间。

## 边界与易错点

数据约束：1≤n≤100000，0≤m≤200000，0≤w≤10^9。

回归样例除普通情况外包括：

- 输入 `2 0`，期望输出 `-1`。
- 输入 `1 0`，期望输出 `0`。

## Python 3 参考实现

```python
n,m=map(int,input().split()); edges=[]
for _ in range(m):
    u,v,w=map(int,input().split()); edges.append((w,u-1,v-1))
p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
ans=cnt=0
for w,u,v in sorted(edges):
    a,b=find(u),find(v)
    if a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]; ans+=w; cnt+=1
print(ans if cnt==n-1 else -1)
```
