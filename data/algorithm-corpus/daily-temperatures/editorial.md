# 等待更高温度：标准题解

## 题目与来源

对每天输出距离下一次严格更高温度的天数，没有则为 0。

来源参考：https://leetcode.cn/problems/daily-temperatures/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：单调栈、数组

## 算法思路

维护温度非递增的下标栈。遇到更高温度，弹出所有较低温度位置并计算距离。

## 正确性说明

栈中位置此前都没有更高温度，当前项第一次满足条件，故弹栈时得到最近的更高位置；未弹出的不存在答案。

## 复杂度

O(n) 时间，O(n) 空间。

## 边界与易错点

数据约束：1≤n≤200000，0≤温度≤100。

回归样例除普通情况外包括：

- 输入 `3 / 5 5 5`，期望输出 `0 0 0`。
- 输入 `1 / 0`，期望输出 `0`。

## Python 3 参考实现

```python
n=int(input()); a=list(map(int,input().split())); st=[]; ans=[0]*n
for i,x in enumerate(a):
    while st and a[st[-1]]<x:
        j=st.pop(); ans[j]=i-j
    st.append(i)
print(*ans)
```
