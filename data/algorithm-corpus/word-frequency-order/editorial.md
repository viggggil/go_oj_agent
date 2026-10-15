# 单词频次排序：标准题解

## 题目与来源

统计一行小写单词的出现次数，按次数降序，同次数按字典序升序，输出所有不同单词。

来源参考：https://leetcode.cn/problems/top-k-frequent-words/。以下讲解、参考代码与测试数据为本项目编写。

算法标签：哈希表、排序、字符串

## 算法思路

哈希计数后按 (-频次, 单词) 的二元组排序。

## 正确性说明

哈希表准确累计每个单词频次，排序的首关键字保证次数降序，次关键字保证同频次字典序，因此完整满足输出规则。

## 复杂度

O(L+k log k) 次字符/单词操作，O(L) 空间；L 为输入长度，k 为不同词数，字符串比较还计入公共前缀长度。

## 边界与易错点

数据约束：1≤总字符数≤200000。

回归样例除普通情况外包括：

- 输入 `z a`，期望输出 `a 1 / z 1`。
- 输入 `hello`，期望输出 `hello 1`。

## Python 3 参考实现

```python
from collections import Counter
counts=Counter(input().split())
for word,count in sorted(counts.items(),key=lambda x:(-x[1],x[0])): print(word,count)
```
