# Web 前端

这是基于 Vue 3、Pinia、Axios 和 Vue Router 的认证前端，提供注册、登录和个人资料页面。

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
