# Web 前端

前端基于 Vue 3、Pinia、Axios 和 Vue Router，提供认证、题库、提交、比赛和 Agent 配置管理。

## 本地启动

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

默认访问 `http://127.0.0.1:5173`，开发服务器会把 `/api` 请求代理到 `http://127.0.0.1:8080`；生产构建默认通过同源 Nginx 反向代理访问 Gateway。也可以使用 `VITE_API_BASE_URL` 配置独立 API 地址。先在仓库根目录执行 `make infra-up` 启动 Gateway、user-service、MySQL 和 Redis。

## 认证状态

- Access Token 和 Refresh Token 保存在浏览器 `localStorage`，Pinia 保存当前用户资料。
- Axios 自动附加 Bearer Access Token。
- 收到 401 时只发起一个 Refresh 请求，其余请求等待同一个 Promise，避免并发刷新竞争。
- Refresh 成功后重放原请求并更新 Token；Refresh 失败则清理本地状态并回到登录页。
- Logout 调用 `/api/v1/auth/logout` 后清理全部本地认证状态。

生产环境应通过 HTTPS、服务端安全响应头和更严格的 Token 存储策略保护认证数据；当前 localStorage 方案仅用于本地开发联调。

## Agent 管理（PR8）

顶栏 Agent 进入 `/agent/chat`。管理员（admin/system_admin/agent_admin）可从边栏切换至
`/agent/agents`、`/agent/prompts`、`/agent/skills`；普通用户直接访问管理 URL 会返回对话。
新建使用 `/{resource}/new`，编辑使用 `/{resource}/{key}/edit`，合法标识 `new` 仍可管理。
加载/重试用户资料失败时不渲染管理内容，后端仍独立验证权限。

- 列表提供搜索、状态筛选、分页；详情支持新建、保存新 UUID、归档、启停和历史恢复。
- Prompt 支持正文/变量白名单；Skill 绑定 Prompt，编辑 direct 模式、工具白名单和预算。
- Agent 绑定 Prompt、多个 Skill、默认 Skill及具体模型 UUID，设置可见范围和测试到期时间。
- 所有选项来自 API，保留历史 UUID。共享配置更新不自动换绑，创建依赖后可返回原表单。
- 未保存输入仅留在当前账号的页面内存中，离开页面有保护，退出/账号权限变化清除。
- 保存/重试保留 X-Request-ID，409 保留输入，读取服务器内容比较后明确继续。

部署需要 PR7 API 与 PR8 历史详情扩展，Gateway Agent client 和 `AGENT_ADMIN_ENABLED=true`、
Agent 数据库/Gateway 公钥，以及已通过 CLI 配置的 su8 Responses Model Profile。
Provider URL/Key 继续部署管理，不进入页面或浏览器存储。
Chat 可以保持关闭而独立管理配置。工具白名单可编辑，但真实运行时尚不调用工具。
`/agent/chat?agent_key=...` 本阶段保留对话目标；发送/停止和流式回答由 PR9 接通。

## 验证

```bash
npm ci
npm run build       # Vue 类型与生产构建
npm run format:check
npm test            # 模板/角色/草稿隔离/幂等及 401 重放
npx playwright install chromium
npm run test:e2e    # 浏览器管理流程与失败路径，无外网模型调用
```

常规浏览器测试使用接口 fixture 验证页面交互与失败路径，截屏和失败 trace 位于
`test-results/`。`agent-live.spec.ts` 默认跳过，在仓库根目录运行下面的命令可启用：

```bash
AGENT_TEST_WEB=true make agent-test-integration
```

该命令启动隔离的 MySQL、Gateway、Agent 和 mock 模型服务，生成临时测试身份密钥，
验证页面创建 Prompt/Skill/test Agent、历史 UUID 绑定和配置运行，退出时清理测试栈。
测试栈没有 user-service，仅用户资料使用固定身份 fixture；管理请求均通过真实 Gateway
写入 MySQL，模型请求使用隔离 mock Provider，不调用外网模型或真实 su8 Key。
