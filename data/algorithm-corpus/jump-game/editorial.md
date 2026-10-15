# 能否到达终点：标准题解

## 题目与来源

数组元素表示当前位置最多能向右跳多少步，判断从首项能否到达末项。

来源参考：https://leetcode.cn/problems/jump-game/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：贪心、数组

## 算法思路

维护当前可达最远位置 far。若扫描下标超过 far 则失败，否则用 i+a_i 扩展。

## 正确性说明

可达位置构成从起点开始的连续前缀，扫描前缀中的每个位置可发现其全部后继；无法越过的间隙也不可能由后续不可达位置补上。

## 复杂度

O(n) 时间，O(1) 额外空间。

## 边界与易错点

数据约束：1≤n≤200000，0≤a_i≤200000。

回归样例除普通情况外包括：

- 输入 `5 / 3 2 1 0 4`，期望输出 `NO`。
- 输入 `1 / 0`，期望输出 `YES`。

## Python 3 参考实现

```python
n=int(input()); a=list(map(int,input().split())); far=0
for i,x in enumerate(a):
    if i>far: break
    far=max(far,i+x)
print('YES' if far>=n-1 else 'NO')
```
