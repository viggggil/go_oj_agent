# 任务依赖排序

有向依赖 u v 表示 u 必须先于 v，输出字典序最小的拓扑序，有环输出 IMPOSSIBLE。

## 输入与输出

输入 n m，再 m 行 u v；输出 n 个任务编号或 IMPOSSIBLE。

## 数据范围

1≤n≤200000，0≤m≤300000，顶点 1 至 n。

## 样例输入

```text
3 1
1 3
```

## 样例输出

```text
1 2 3
```

## 来源

经典题型参考：https://leetcode.cn/problems/course-schedule/。本项目重新表述并适配标准输入输出，范围或要求可能与原题不同。
