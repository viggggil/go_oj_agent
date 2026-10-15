# 经典算法题库与 RAG 题解

本语料包含 50 道适配平台标准输入输出的中文题目。保留原有 12 道开发示例题的 slug 与判题格式，新增 38 道。
以力扣、洛谷的公开经典题型为主要参考，来源链接列在 manifest 与逐题题面中。输入输出、范围和要求有适配差异，
不声称与原站完全相同；题面重新表述，题解、参考代码及测试数据由本项目编写，不包含抓取的第三方题解或隐藏测试。

每题目录包含：

- `statement.md`：题意、标准输入输出、范围、样例和来源。
- `editorial.md`：思路、正确性、复杂度、边界及 Python 3 代码，只导入管理员知识库。
- `solution.py`：可提交到现有 Python 判题运行器的参考程序。
- `tests.json`：3 组已知答案样例及 8 组确定性生成数据。生成数据的输出由已校验的参考程序产生。

算法覆盖：哈希、排序、链表、双指针、滑窗、前缀和、二分与二分答案、单调栈/队列、堆、贪心、
零一/完全背包、序列/字符串/网格 DP、KMP、Trie、BFS、回溯、最短路、拓扑、最小生成树、并查集、
树深度/直径/LCA、树状数组、懒标记线段树、ST 表、归并逆序对、快速幂、素数筛及位运算。

校验与导入：

```bash
make rag-corpus-check
make infra-up
# token 文件保存正常登录所得管理员 access token，限制文件权限；不要提交到仓库。
agent/.venv/bin/python scripts/import_algorithm_corpus.py --token-file /secure/admin-token
agent/.venv/bin/python scripts/verify_algorithm_submissions.py --token-file /secure/admin-token
agent/.venv/bin/python scripts/rag_search.py --token-file /secure/admin-token --base-id BASE_UUID --query '滑动窗口最大值如何维护单调队列'
```

导入先经 Gateway 查询题目，以 slug 复用既有题目，不覆盖原题面/原测试点；补充缺少的输入输出 hash 对。
在本项目原始 12 道示例基础上首次导入达到 50 题。若部署已包含其他用户题目，不删除或归档它们以凑数。
创建题目时携带测试点，超时先重新查 slug；测试点写失败退出，下次运行查 hash 后继续，避免盲目重放。
知识库写入使用稳定请求 UUID 幂等；等待索引就绪再启用，报告保存真实题号、文档及版本 UUID。
首次索引失败应按文档管理员 API 重试；重新运行不会自动恢复管理员主动归档的文档。

语料通过显式导入写入业务服务，不在 `make infra-up` 时自动修改题库。PR1 尚不向聊天注册检索工具；
知识库管理员可以通过检索测试 API 验证命中，普通用户的模型检索在后续 PR3 接入。
