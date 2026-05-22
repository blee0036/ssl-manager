# Frontend API Mismatch Bugfix Design

## Overview

前端 JS 文件（`web/static/js/` 目录下）中的 API 调用与后端 Go handler 实际实现存在大面积不匹配。涉及 8 个页面文件：domains.js、alerts.js、thirdpart-dns.js、audit-logs.js、users.js、system.js、dashboard.js、machines.js。

根本原因是前端 JS 由 AI 子代理生成时未读取后端 handler 实际代码，凭空想象了 API 结构、字段名、请求格式和可用端点。修复策略是逐一对齐每个前端 JS 文件的 API 调用，使其与后端 handler 的实际路由、请求体结构和响应字段完全一致。不需要修改任何后端代码。

## Glossary

- **Bug_Condition (C)**: 前端 JS 文件中的 API 调用（端点路径、请求体字段、响应字段）与后端 handler 实际实现不匹配的条件
- **Property (P)**: 前端 JS 的 API 调用应与后端 handler 的路由注册、输入结构体、模型字段完全一致
- **Preservation**: 已经正确工作的前端功能（certificates.js、login.js、init.js、app.js 工具函数、dashboard 字段渲染）不受修改影响
- **Handler**: `internal/web/handler/` 目录下的 Go HTTP 处理函数，定义了实际的 API 路由和请求/响应格式
- **Model**: `internal/model/` 目录下的 Go 结构体，定义了数据库实体的 JSON 序列化字段名
- **Input Struct**: `internal/model/input.go` 和 `internal/web/service/` 中定义的请求体结构体

## Bug Details

### Bug Condition

当前端 JS 文件向后端发起 API 请求时，使用了错误的端点路径、请求体字段名或期望了不存在的响应字段，导致请求失败（404/400）或页面渲染空白。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type FrontendAPICall { file, endpoint, method, requestBody, responseFieldsUsed }
  OUTPUT: boolean
  
  RETURN (input.endpoint NOT IN backendRegisteredRoutes)
         OR (input.requestBody.fieldNames NOT SUBSET OF backendInputStruct.jsonTags)
         OR (input.responseFieldsUsed NOT SUBSET OF backendModel.jsonTags)
         OR (input.queryParams NOT SUBSET OF backendHandler.supportedQueryParams)
END FUNCTION
```

### Examples

- **domains.js 创建域名**: 前端发送 `{ domain, port, certificate_id }` → 后端期望 `{ name, monitor_port, linked_certificate_id }` → 400 Bad Request
- **domains.js 调用 probe-all**: 前端调用 `POST /api/domains/probe-all` → 后端不存在此路由 → 404 Not Found
- **thirdpart-dns.js 渲染列表**: 前端读取 `c.provider`、`c.domain` → 后端返回 `type`、`main_domains`（数组） → 页面显示空白
- **alerts.js 创建渠道**: 前端发送 `{ name, type: "webhook", webhook_url, enabled }` → 后端期望 `{ name, type: "lark"|"telegram", config_json, enabled }` → 400 Bad Request
- **users.js 重置密码**: 前端调用 `PUT /api/users/{id}/password` 发送 `{ password }` → 后端路由是 `POST /api/users/{id}/reset-password` 期望 `{ new_password }` → 404/405
- **system.js 保存配置**: 前端发送扁平结构 `{ listen_addr, certbot_email, ... }` → 后端期望嵌套 `config.Config` 结构 → 400 validation failed
- **audit-logs.js 分页**: 前端使用 `page`/`page_size`/`action`/`from`/`to` → 后端支持 `limit`/`offset`/`actor_type`/`target_type` → 过滤无效

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- certificates.js 的所有 API 调用（列表、详情、上传、Cloudflare 签发、手动 DNS 签发、删除）必须继续正常工作
- dashboard.js 的统计字段渲染（`certificates_total`、`machines_online` 等）必须继续正确显示
- machines.js 的创建机器流程（发送 `{ name, ip, tags, remark }` 并接收 `{ machine, agent_token }`）必须继续正常工作
- machines.js 的 Token 管理（regenerate-token、revoke-token）和安装命令获取必须继续正常工作
- machines.js 的机器证书部署配置 CRUD 和部署触发必须继续正常工作
- init.js 的初始化流程必须继续正常工作
- login.js 的登录流程必须继续正常工作
- app.js 的通用工具函数（`App.get`、`App.post`、`App.put`、`App.delete`、`App.escapeHtml`、`App.formatDate`、`App.toast`）必须继续正常工作
- 所有 API 响应的统一 `{ code, message, data }` 包装格式处理必须继续正常工作

**Scope:**
所有不涉及 API 端点路径、请求体字段名、响应字段名修改的前端逻辑应完全不受影响。这包括：
- UI 布局和样式
- 事件绑定和交互逻辑
- 错误处理和 toast 提示
- 模态框展示逻辑

## Hypothesized Root Cause

Based on the bug description, the root cause is confirmed:

1. **AI 子代理未读取后端代码**: 生成前端 JS 时，AI 子代理凭空想象了 API 结构，未参考 `internal/web/handler/` 中的实际路由注册和 `internal/model/` 中的实际字段定义

2. **字段名映射错误**: 前端使用了直觉性的字段名（如 `domain`、`port`、`provider`、`webhook_url`、`severity`、`username`），而后端使用了更规范的命名（如 `name`、`monitor_port`、`type`、`config_json`、`level`、`actor_type`）

3. **不存在的端点被调用**: 前端假设存在 `probe-all`、`sync-all`、`DELETE /api/users/{id}`、`GET /api/users/{id}` 等端点，但后端从未实现这些路由

4. **请求格式结构错误**: system.js 使用扁平字段结构，而后端期望完整的嵌套 `config.Config` JSON；alerts.js 使用 `webhook_url` 而后端期望 `config_json` 字符串

5. **分页参数不匹配**: 前端使用 `page`/`page_size` 分页模式，而后端（audit-logs）使用 `limit`/`offset` 模式，或完全不支持分页（domains、alerts）

6. **URL 尾部斜杠不一致**: machines.js 和 dashboard.js 在某些调用中使用了尾部斜杠（如 `/api/dashboard/`、`/api/machines/{id}/`），虽然 chi 路由器的 Route 子路由注册方式可能兼容，但应统一为无尾部斜杠以避免潜在的 301 重定向

## Correctness Properties

Property 1: Bug Condition - API 调用与后端契约一致

_For any_ frontend API call where the bug condition holds (endpoint path, request body fields, or response field access does not match the backend handler), the fixed frontend code SHALL use the correct endpoint path as registered in the backend handler's `RegisterRoutes`, the correct request body field names as defined in the backend input structs, and the correct response field names as defined in the backend model structs.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 2.12, 2.13, 2.14, 2.15, 2.16, 2.17, 2.18, 2.19, 2.20, 2.21, 2.22, 2.23, 2.24**

Property 2: Preservation - 已正确工作的功能不受影响

_For any_ frontend functionality that is currently working correctly (certificates.js, login.js, init.js, app.js utilities, dashboard field rendering, machines CRUD and token management), the fixed code SHALL produce exactly the same behavior as the original code, preserving all existing correct API interactions and UI rendering.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9**

## Fix Implementation

### Changes Required

无需修改后端代码。所有修改仅限于 `web/static/js/` 目录下的前端 JS 文件。

---

**File**: `web/static/js/domains.js`

**Specific Changes**:
1. **移除分页逻辑**: 删除 `page`/`page_size` 参数，后端 `GET /api/domains` 不支持分页，返回全部数据
2. **修正查询参数**: 使用后端支持的 `source`、`thirdpart_dns_id`、`monitor_enabled` 过滤参数
3. **修正列表渲染字段**: 将 `d.domain` → `d.name`，移除 `d.cert_expires_at`、`d.not_after`、`d.fingerprint_match`、`d.tls_status`、`d.last_checked_at`、`d.remote_fingerprint`（这些是探测结果字段，不在 Domain 模型中），改为显示 `d.monitor_port`、`d.monitor_enabled`、`d.source`
4. **修正创建请求体**: 将 `{ domain, port, certificate_id }` → `{ name, monitor_port, linked_certificate_id }`
5. **移除 probe-all**: 删除 `probeAll()` 函数和对应按钮事件，后端不存在 `/api/domains/probe-all` 端点
6. **修正详情展示**: 使用 Domain 模型的实际字段（`name`、`monitor_port`、`monitor_enabled`、`source`、`linked_certificate_id`）

---

**File**: `web/static/js/thirdpart-dns.js`

**Specific Changes**:
1. **修正列表渲染字段**: 将 `c.provider` → `c.type`，将 `c.domain` → `c.main_domains`（数组，需 join 显示），移除 `c.last_synced_at`
2. **修正创建/更新请求体**: 将 `{ name, provider, domain, api_token, zone_id, enabled }` → `{ name, type, api_token, config_json, main_domains }`，其中 `config_json` 为 JSON 字符串（包含 zone_id 等配置），`main_domains` 为字符串数组
3. **移除 sync-all**: 删除 `syncAll()` 函数和对应按钮事件，后端不存在 `/api/thirdpart-dns/sync-all` 端点
4. **修正编辑表单**: 表单字段从 provider/domain/zone_id 改为 type/main_domains/config_json

---

**File**: `web/static/js/alerts.js`

**Specific Changes**:
1. **移除告警历史分页**: 删除 `page`/`page_size` 参数，后端 `GET /api/alerts` 不支持分页
2. **修正告警历史查询参数**: 使用后端支持的 `level`、`type`、`status` 过滤参数
3. **修正告警列表渲染字段**: 将 `a.severity` → `a.level`，将 `a.alert_type` → `a.type`，将 `a.message` → `a.title`/`a.content`，将 `a.sent` → `a.status`，将 `a.timestamp` → `a.created_at`，添加 `a.sent_channels` 显示
4. **修正通知渠道类型选项**: 将 webhook/email/dingtalk/feishu/wechat → lark/telegram
5. **修正通知渠道创建/更新请求体**: 将 `{ name, type, webhook_url, enabled }` → `{ name, type, config_json, enabled }`，其中 `config_json` 为 JSON 字符串（包含 webhook_url 或 bot_token）
6. **修正通知渠道列表渲染**: 移除 `c.channel_type`、`c.webhook_url`、`c.config.url` 的使用，改为 `c.type`、`c.config_json`
7. **移除 editChannel 中的 GET 单个渠道调用**: 后端 `GET /api/alerts/channels/{id}` 不存在（只有 `GET /api/alerts/{id}` 是获取告警详情），编辑时从列表数据中获取

---

**File**: `web/static/js/audit-logs.js`

**Specific Changes**:
1. **修正分页参数**: 将 `page`/`page_size` → `limit`/`offset`（offset = (page-1) * limit）
2. **修正过滤参数**: 将 `action`/`from`/`to` → `actor_type`/`target_type`
3. **修正列表渲染字段**: 将 `log.username`/`log.user_id` → `log.actor_type`/`log.actor_id`，将 `log.resource_type`/`log.resource_id` → `log.target_type`/`log.target_id`，将 `log.details` → `log.detail`，将 `log.ip_address` → `log.ip`，将 `log.timestamp` → `log.created_at`

---

**File**: `web/static/js/users.js`

**Specific Changes**:
1. **修正角色选项**: 将 `admin`/`readonly` → `admin`/`user`
2. **修正创建请求体**: 只发送 `{ username, password }`，不发送 role/enabled（后端 CreateUserInput 只有 username 和 password）
3. **修正重置密码**: 将 `PUT /api/users/{id}/password` + `{ password }` → `POST /api/users/{id}/reset-password` + `{ new_password }`
4. **修正禁用用户**: 将 `PUT /api/users/{id}` + `{ enabled: false }` → `POST /api/users/{id}/disable`
5. **移除启用用户功能**: 后端只有 disable 接口，没有 enable 接口
6. **修正更新角色**: `PUT /api/users/{id}` 只接受 `{ role }` 字段
7. **移除删除用户**: 后端不存在 `DELETE /api/users/{id}` 路由
8. **移除编辑用户的 GET 单个用户**: 后端不存在 `GET /api/users/{id}` 路由，改为从列表数据中获取用户信息
9. **移除 last_login_at 字段显示**: 后端 User 模型没有此字段

---

**File**: `web/static/js/system.js`

**Specific Changes**:
1. **修正加载配置字段映射**: 从嵌套结构读取：`cfg.server.listen_addr`、`cfg.server.external_url`、`cfg.certbot.email`、`cfg.certbot.binary_path`、`cfg.certbot.data_dir`、`cfg.alert.default_before_days`、`cfg.agent.heartbeat_timeout_seconds`、`cfg.agent.poll_interval_seconds`、`cfg.readonly.enabled`、`cfg.domain_monitor.default_port`、`cfg.domain_monitor.interval_minutes`
2. **修正保存配置请求体**: 发送完整嵌套的 `config.Config` 结构：
   ```json
   {
     "server": { "external_url": "...", "listen_addr": "..." },
     "agent": { "heartbeat_timeout_seconds": 120, "poll_interval_seconds": 60 },
     "alert": { "default_before_days": 15 },
     "certbot": { "binary_path": "certbot", "data_dir": "...", "email": "..." },
     "readonly": { "enabled": false, "view_password": "..." },
     "domain_monitor": { "default_port": 443, "interval_minutes": 60 }
   }
   ```
3. **处理敏感字段掩码**: 加载时如果 `readonly.view_password` 为 `"***"`，保存时保持原值不变（发送 `"***"` 让后端保留原密码）

---

**File**: `web/static/js/dashboard.js`

**Specific Changes**:
1. **修正 URL 尾部斜杠**: 将 `/api/dashboard/` → `/api/dashboard`（虽然 chi 的 Route 子路由注册 `r.Get("/", h.GetDashboard)` 在 `/api/dashboard` 路由组下可能兼容尾部斜杠，但统一为无尾部斜杠更规范）

---

**File**: `web/static/js/machines.js`

**Specific Changes**:
1. **修正列表 URL**: 将 `/api/machines/` → `/api/machines`（列表端点注册为 `r.Get("/", h.List)` 在 `/api/machines` 路由组下）
2. **修正详情/删除 URL 尾部斜杠**: 注意后端 machine handler 使用 `r.Route("/{id}", func(r chi.Router) { r.Get("/", ...) })`，chi 中这意味着 `GET /api/machines/{id}` 和 `GET /api/machines/{id}/` 都可能匹配。但为一致性，统一移除尾部斜杠
3. **修正机器证书 URL 尾部斜杠**: 同理，`/api/machines/{id}/certificates/` → `/api/machines/{id}/certificates`，`/api/machines/{id}/certificates/{mc_id}/` → `/api/machines/{id}/certificates/{mc_id}`
4. **注意**: machines.js 的创建请求体 `{ name, ip, tags, remark }` 和响应处理 `{ machine, agent_token }` 已经正确，无需修改

## Testing Strategy

### Validation Approach

测试策略分为两个阶段：首先在未修复代码上验证 bug 确实存在（API 调用失败），然后在修复后验证所有 API 调用正确工作且已有功能未被破坏。

### Exploratory Bug Condition Checking

**Goal**: 在实施修复前，验证 bug 确实存在。通过启动后端服务并使用浏览器或 curl 模拟前端请求，确认错误的 API 调用确实返回 404/400/空数据。

**Test Plan**: 对每个有问题的前端文件，模拟其 API 调用并观察后端响应。

**Test Cases**:
1. **domains.js 创建测试**: 发送 `POST /api/domains` + `{ "domain": "test.com", "port": 443 }` → 期望 400（缺少 name 字段）
2. **domains.js probe-all 测试**: 发送 `POST /api/domains/probe-all` → 期望 404
3. **thirdpart-dns.js sync-all 测试**: 发送 `POST /api/thirdpart-dns/sync-all` → 期望 404
4. **alerts.js 创建渠道测试**: 发送 `POST /api/alerts/channels` + `{ "name": "test", "type": "webhook", "webhook_url": "..." }` → 期望 400（type 必须是 lark 或 telegram）
5. **users.js 重置密码测试**: 发送 `PUT /api/users/{id}/password` → 期望 404/405
6. **users.js 删除测试**: 发送 `DELETE /api/users/{id}` → 期望 404/405
7. **system.js 保存测试**: 发送 `PUT /api/system/config` + `{ "listen_addr": ":8080" }` → 期望 400（validation failed，缺少必填嵌套字段）

**Expected Counterexamples**:
- 所有使用错误字段名的创建/更新请求返回 400 或静默忽略字段
- 所有调用不存在端点的请求返回 404
- 所有使用错误分页参数的请求返回全部数据（参数被忽略）

### Fix Checking

**Goal**: 验证修复后，所有前端 API 调用使用正确的端点、请求体和响应字段。

**Pseudocode:**
```
FOR ALL apiCall WHERE isBugCondition(apiCall) DO
  result := fixedFrontend.makeAPICall(apiCall)
  ASSERT result.httpStatus IN [200, 201]
  ASSERT result.responseBody.code == 0
  ASSERT frontendRendering(result.responseBody.data) displays correct values
END FOR
```

### Preservation Checking

**Goal**: 验证修复后，所有已正确工作的功能（certificates.js、login.js、init.js、app.js、dashboard 字段渲染、machines CRUD）继续正常工作。

**Pseudocode:**
```
FOR ALL apiCall WHERE NOT isBugCondition(apiCall) DO
  ASSERT originalFrontend.makeAPICall(apiCall) = fixedFrontend.makeAPICall(apiCall)
END FOR
```

**Testing Approach**: 由于这是纯前端修改，preservation checking 主要通过以下方式验证：
- 确认未修改的文件（certificates.js、login.js、init.js、app.js）完全未被改动
- 确认 dashboard.js 中统计字段渲染逻辑未被改动（只修正了 URL 尾部斜杠）
- 确认 machines.js 中已正确的创建/Token 管理逻辑未被改动（只修正了 URL 尾部斜杠）

**Test Plan**: 在修复后启动完整应用，逐一验证每个页面的核心功能。

**Test Cases**:
1. **证书管理保持**: 验证证书列表加载、上传、签发、删除功能正常
2. **仪表盘保持**: 验证统计数据正确显示
3. **机器管理保持**: 验证机器创建、Token 管理、安装命令功能正常
4. **登录保持**: 验证登录流程正常
5. **初始化保持**: 验证初始化流程正常

### Unit Tests

- 对每个修改的 JS 文件，验证 API 调用使用正确的 URL 和请求体
- 验证 domains.js 创建域名发送 `{ name, monitor_port, linked_certificate_id }`
- 验证 thirdpart-dns.js 创建配置发送 `{ name, type, api_token, config_json, main_domains }`
- 验证 alerts.js 创建渠道发送 `{ name, type, config_json, enabled }` 且 type 为 lark/telegram
- 验证 users.js 重置密码调用 `POST /api/users/{id}/reset-password` + `{ new_password }`
- 验证 system.js 保存配置发送完整嵌套结构
- 验证 audit-logs.js 使用 `limit`/`offset` 参数

### Property-Based Tests

- 生成随机的域名创建输入，验证请求体始终包含 `name` 字段（而非 `domain`）
- 生成随机的通知渠道类型，验证只允许 `lark` 或 `telegram`
- 生成随机的系统配置值，验证保存时始终使用嵌套结构且所有必填字段存在
- 生成随机的审计日志查询参数，验证始终使用 `limit`/`offset`/`actor_type`/`target_type`

### Integration Tests

- 启动完整应用后，通过浏览器自动化测试每个页面的 CRUD 流程
- 验证域名监控页面：添加域名 → 列表显示 → 探测 → 删除
- 验证第三方 DNS 页面：添加配置 → 列表显示 → 同步 → 删除
- 验证告警管理页面：添加渠道 → 测试发送 → 查看告警历史
- 验证用户管理页面：创建用户 → 修改角色 → 重置密码 → 禁用
- 验证系统配置页面：加载配置 → 修改 → 保存 → 重新加载验证
