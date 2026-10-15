# 字符串匹配位置：标准题解

## 题目与来源

给定文本和模式，输出模式首次出现的 1 开始位置，没有则 -1。这是模板题的单次定位版本。

来源参考：https://www.luogu.com.cn/problem/P3375。以下讲解、参考代码与测试数据为本项目编写。

算法标签：字符串、KMP

## 算法思路

先构造模式前缀函数，匹配失败时回退到最长可用 border，成功时推进。

## 正确性说明

前缀函数枚举失配后仍可能匹配的最长前后缀，不跳过可能起点；每次完整匹配是最先结束的，固定模式长度下也是最先开始的。

## 复杂度

O(n+m) 时间，O(m) 空间。

## 边界与易错点

数据约束：1≤两串长度≤200000，仅小写英文字母。

回归样例除普通情况外包括：

- 输入 `aaaa / b`，期望输出 `-1`。
- 输入 `aaaaa / aaa`，期望输出 `1`。

## Python 3 参考实现

```python
s=input().strip(); p=input().strip(); pi=[0]*len(p); j=0
for i in range(1,len(p)):
    while j and p[i]!=p[j]: j=pi[j-1]
    if p[i]==p[j]: j+=1
    pi[i]=j
j=0; ans=-1
for i,c in enumerate(s):
    while j and c!=p[j]: j=pi[j-1]
    if c==p[j]: j+=1
    if j==len(p): ans=i-len(p)+2; break
print(ans)
```
