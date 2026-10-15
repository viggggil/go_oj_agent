# 反转单链表序列：标准题解

## 题目与来源

用数组给出链表节点顺序，构造链表并反转，输出反转后的节点值。

来源参考：https://leetcode.cn/problems/reverse-linked-list/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：链表、模拟

## 算法思路

按顺序创建单链表，循环保存后继，将当前 next 指向 prev，再前移。

## 正确性说明

循环前 prev 是已处理前缀的反转链，cur 是未处理后缀；每步把后缀首节点移到反转链头，结束时整链反转。

## 复杂度

O(n) 时间，构建 O(n) 空间，反转 O(1) 额外空间。

## 边界与易错点

数据约束：1≤n≤200000，|值|≤10^9。

回归样例除普通情况外包括：

- 输入 `1 / -1`，期望输出 `-1`。
- 输入 `2 / 0 0`，期望输出 `0 0`。

## Python 3 参考实现

```python
n=int(input()); a=list(map(int,input().split())); nodes=[[x,None] for x in a]
for i in range(n-1): nodes[i][1]=nodes[i+1]
prev=None; cur=nodes[0]
while cur:
    nxt=cur[1]; cur[1]=prev; prev=cur; cur=nxt
ans=[]
while prev: ans.append(prev[0]); prev=prev[1]
print(*ans)
```
