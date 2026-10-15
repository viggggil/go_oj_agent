# 前缀单词计数：标准题解

## 题目与来源

给定单词集合，重复词分别计数，回答有多少单词以某查询串开头。

来源参考：https://www.luogu.com.cn/problem/P8306。以下讲解、参考代码与测试数据为本项目编写。

算法标签：字符串、Trie

## 算法思路

插入时沿 Trie 路径给每个节点计数加一，查询沿前缀走到节点，读出计数。

## 正确性说明

经过某节点的插入路径恰好是含该节点前缀的单词，节点计数因此与要求完全相同。缺失边则没有匹配。

## 复杂度

O(总字符数) 时间与空间。

## 边界与易错点

数据约束：1≤n,q≤100000，单词/查询均为非空小写字母串，总长度≤300000。

回归样例除普通情况外包括：

- 输入 `2 1 / a / a / a`，期望输出 `2`。
- 输入 `1 1 / a / aa`，期望输出 `0`。

## Python 3 参考实现

```python
n,q=map(int,input().split()); nodes=[{}]; count=[0]
for _ in range(n):
    u=0
    for c in input().strip():
        if c not in nodes[u]: nodes[u][c]=len(nodes); nodes.append({}); count.append(0)
        u=nodes[u][c]; count[u]+=1
for _ in range(q):
    u=0
    for c in input().strip():
        if c not in nodes[u]: u=-1; break
        u=nodes[u][c]
    print(0 if u<0 else count[u])
```
