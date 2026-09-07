# Go-Kratos 工程规范

## 1. 核心依赖

```text
API / Proto
    ↓
 service
    ↓
   biz
    ↑
   data
```

原则：

* `service`：传输适配
* `biz`：业务核心
* `data`：基础设施实现
* `server`：HTTP/gRPC + Middleware
* `conf`：配置定义
* `cmd`：启动与 Wire 装配

禁止反向依赖：

```text
biz → data      ❌
biz → service   ❌
biz → protobuf  ❌
service → data  ❌
```

---

## 2. Service

负责：

```text
Proto DTO
 ↓
转换 / transport validation
 ↓
Biz Input / Domain Object
 ↓
Usecase
 ↓
Proto Response
```

可以：

* DTO ↔ Domain 转换
* 提取 metadata/context
* 调用 Usecase

不要：

* 查 MySQL / Redis
* 写业务规则
* 做资源权限判断

---

## 3. Biz

负责：

```text
Domain Model
Usecase
Business Rule
Repository Interface
Business Error
Policy
```

例如：

```go
type User struct {...}

type UserRepo interface {
    FindByID(ctx context.Context, id int64) (User, error)
}

type UserUsecase struct {
    users UserRepo
}
```

业务授权应放这里：

```go
if actor.UserID != userID && !actor.IsAdmin() {
    return ErrPermissionDenied
}
```

Biz 不应知道：

```text
sql.DB
redis.Client
GORM/sqlc
Proto Request
JWT implementation
bcrypt implementation
```

---

## 4. Data

负责：

```text
Repository Implementation
MySQL / Redis
Transaction
DO ↔ PO
Storage Error Mapping
```

依赖方向：

```text
biz.UserRepo
     ↑ implements
data.userRepo
     ↓
   MySQL
```

Repository constructor 推荐返回 Biz interface：

```go
func NewUserRepo(data *Data) biz.UserRepo
```

数据库错误不要泄漏到 Biz：

```text
sql.ErrNoRows
   ↓ data
ErrUserNotFound
```

---

## 5. Server

只负责：

```text
HTTP Server
gRPC Server
Middleware
Service Registration
```

例如：

```text
Recovery
Tracing
Logging
Authentication
RateLimit
```

区分：

```text
Authentication → Middleware
Authorization  → Biz
```

---

## 6. Conf 与 Wire

`conf` 只描述配置：

```text
Server
Database
Redis
Auth
```

不要在 Biz 解析配置。

最终依赖统一在：

```text
cmd/server
```

通过 Wire 装配：

```go
wire.Build(
    server.ProviderSet,
    data.ProviderSet,
    biz.ProviderSet,
    service.ProviderSet,
)
```

---

## 7. Infrastructure 扩展

Kratos 核心仍然是：

```text
service → biz ← data
```

复杂服务可以增加：

```text
internal/security/
internal/mq/
internal/client/
```

例如 User Service：

```text
biz.PasswordHasher interface
          ↑
security.BcryptPasswordHasher
```

```text
biz.TokenIssuer interface
          ↑
security.JWTTokenManager
```

注意：这些是项目扩展，不是所有服务都必须创建。

---

## 8. 模型边界

保持：

```text
DTO → Service → DO → Data → PO
```

* DTO：Proto Request/Response
* DO：Biz Domain Object
* PO：数据库/Redis存储对象

禁止：

```text
Biz 使用 Proto DTO
Service 使用 DB PO
Data 返回 PO 给上层
```

---

## 9. 文件组织

优先按**业务能力**命名：

```text
biz/
├── user.go
├── auth.go
└── errors.go
```

而不是大量：

```text
model.go
types.go
utils.go
helpers.go
common.go
```

普通服务推荐：

```text
internal/
├── biz/
├── data/
├── service/
├── server/
└── conf/
```

需要时再增加基础设施目录。

---

## 10. Review Checklist

写代码时只检查这些：

```text
[ ] Service 是否只做 DTO/DO 转换和调用 Usecase？
[ ] Biz 是否完全不知道 MySQL / Redis / Proto？
[ ] Repository interface 是否在 Biz？
[ ] Repository implementation 是否在 Data？
[ ] 业务规则和权限是否在 Biz？
[ ] Middleware 是否只负责横切能力？
[ ] 配置解析是否没有进入 Biz？
[ ] 最终依赖是否由 Wire 装配？
[ ] DO / DTO / PO 是否没有互相泄漏？
```

一句话：

```text
API 定义契约
Service 适配请求
Biz 表达业务
Data 实现存储
Server 负责传输
Wire 完成装配
```

**业务层定义需要什么能力，外围层负责实现这些能力。**
