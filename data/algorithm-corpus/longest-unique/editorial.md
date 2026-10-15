# 最长无重复子串：标准题解

## 题目与来源

求字符串中不含重复字符的最长连续子串长度。

来源参考：https://leetcode.cn/problems/longest-substring-without-repeating-characters/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：字符串、滑动窗口

## 算法思路

记录每个字符最近出现的位置。右端加入字符后，左端跳至上次同字符之后，但不能向左回退。

## 正确性说明

更新后窗口中当前字符唯一，其余字符仍唯一；左端是满足条件的最左位置，因此对每个右端得到了最长合法窗口。

## 复杂度

O(n) 时间，O(26) 空间。

## 边界与易错点

数据约束：1≤长度≤200000。

回归样例除普通情况外包括：

- 输入 `bbbbb`，期望输出 `1`。
- 输入 `abba`，期望输出 `2`。

## Python 3 参考实现

```python
s=input().strip(); last={}; l=0; ans=0
for r,c in enumerate(s):
    l=max(l,last.get(c,-1)+1); last[c]=r; ans=max(ans,r-l+1)
print(ans)
```
