# 括号序列检查：标准题解

## 题目与来源

判断仅含 ()[]{} 的非空字符串是否正确嵌套且完全匹配。

来源参考：https://leetcode.cn/problems/valid-parentheses/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：栈、字符串

## 算法思路

左括号入栈，右括号必须与栈顶类型相同并出栈，最后栈须为空。

## 正确性说明

栈始终是已扫描部分尚未匹配的左括号，最后开启的括号必须最先关闭，故每次检查正是合法嵌套的必要条件，也足以保证完整匹配。

## 复杂度

O(n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤长度≤200000。

回归样例除普通情况外包括：

- 输入 `([)]`，期望输出 `NO`。
- 输入 `((`，期望输出 `NO`。

## Python 3 参考实现

```python
s=input().strip(); st=[]; pairs={')':'(',']':'[','}':'{'}; ok=True
for c in s:
    if c in '([{': st.append(c)
    elif not st or st.pop()!=pairs[c]: ok=False; break
print('YES' if ok and not st else 'NO')
```
