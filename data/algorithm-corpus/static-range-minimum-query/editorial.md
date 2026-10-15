# 区间最小值查询：标准题解

## 题目与来源

给定不可修改整数数组，回答若干闭区间最小值查询。这是 RMQ 模板的最小值版本。

来源参考：https://www.luogu.com.cn/problem/P3865。以下讲解、参考代码与测试数据为本项目编写。

算法标签：ST表、倍增、数据结构

## 算法思路

ST 表保存每个起点长度为 2^k 的区间最小值，查询用两段覆盖，允许重叠。

## 正确性说明

两个半长区间的最小值可合并出完整区间。查询选 k=floor(log2(length))，首尾两段覆盖全部查询区间；重复取最小值不改变结果。

## 复杂度

O(n log n+q) 时间，O(n log n) 空间。

## 边界与易错点

数据约束：1≤n,q≤100000，|a_i|≤10^9。

回归样例除普通情况外包括：

- 输入 `1 1 / 7 / 1 1`，期望输出 `7`。
- 输入 `2 1 / 5 5 / 1 2`，期望输出 `5`。

## Python 3 参考实现

```python
n,q=map(int,input().split()); a=list(map(int,input().split())); table=[a]; k=1
while 1<<k<=n:
    prev=table[-1]; half=1<<(k-1); table.append([min(prev[i],prev[i+half]) for i in range(n-(1<<k)+1)]); k+=1
for _ in range(q):
    l,r=map(int,input().split()); k=(r-l+1).bit_length()-1; print(min(table[k][l-1],table[k][r-(1<<k)]))
```
