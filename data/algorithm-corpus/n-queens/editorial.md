# N 皇后方案数：标准题解

## 题目与来源

在 n×n 棋盘放 n 个皇后，使任意两枚不同行、列、对角线，求方案数，不要求输出棋盘。

来源参考：https://www.luogu.com.cn/problem/P1219。以下讲解、参考代码与测试数据为本项目编写。

算法标签：回溯、搜索

## 算法思路

逐行放置，集合记录列及两类对角线，合法时递归，返回时撤销。

## 正确性说明

每行恰放一个皇后，列和对角线检查充要地排除了攻击。搜索枚举所有逐行选择且每种棋盘序列唯一，故不漏不重。

## 复杂度

O(n!) 搜索上界，O(n) 递归及集合空间。

## 边界与易错点

数据约束：1≤n≤11。

回归样例除普通情况外包括：

- 输入 `1`，期望输出 `1`。
- 输入 `2`，期望输出 `0`。

## Python 3 参考实现

```python
n=int(input()); cols=set(); diag1=set(); diag2=set()
def dfs(r):
    if r==n: return 1
    total=0
    for c in range(n):
        if c in cols or r-c in diag1 or r+c in diag2: continue
        cols.add(c); diag1.add(r-c); diag2.add(r+c); total+=dfs(r+1)
        cols.remove(c); diag1.remove(r-c); diag2.remove(r+c)
    return total
print(dfs(0))
```
