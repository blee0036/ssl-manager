# 实施计划

- [x] 1. 编写 Bug 条件探索测试
  - **属性 1: Bug 条件** - 前端 API 调用使用了错误的端点、字段和请求体
  - **关键**: 此测试必须在未修复代码上失败 - 失败即确认 bug 存在
  - **不要在测试失败时尝试修复测试或代码**
  - **说明**: 此测试编码了期望行为 - 修复实施后测试通过即验证修复正确
  - **目标**: 产出反例，证明前端 API 调用与后端契约不匹配
  - **范围化 PBT 方法**: 对每个受影响文件，验证 API 调用参数是否匹配后端期望
  - 测试 domains.js 创建域名发送 `{ name, monitor_port, linked_certificate_id }`（而非 `{ domain, port, certificate_id }`）
  - 测试 domains.js 不调用 `/api/domains/probe-all`（端点不存在）
  - 测试 domains.js 不使用分页参数 `page`/`page_size`
  - 测试 thirdpart-dns.js 创建配置发送 `{ name, type, api_token, config_json, main_domains }`（而非 `{ name, provider, domain, api_token, zone_id, enabled }`）
  - 测试 thirdpart-dns.js 不调用 `/api/thirdpart-dns/sync-all`
  - 测试 alerts.js 创建通知渠道发送 `{ name, type, config_json, enabled }` 且 type 仅为 ['lark','telegram']（而非 `{ name, type, webhook_url, enabled }` 且 type 为 ['webhook','email','dingtalk','feishu','wechat']）
  - 测试 alerts.js 不使用分页参数，使用 `level`/`type`/`status` 过滤
  - 测试 alerts.js 渲染使用 `a.level`、`a.type`、`a.title`、`a.content`、`a.created_at`（而非 `a.severity`、`a.alert_type`、`a.message`、`a.timestamp`）
  - 测试 audit-logs.js 使用 `limit`/`offset`/`actor_type`/`target_type` 参数（而非 `page`/`page_size`/`action`/`from`/`to`）
  - 测试 audit-logs.js 渲染使用 `log.actor_type`、`log.actor_id`、`log.target_type`、`log.target_id`、`log.detail`、`log.ip`、`log.created_at`
  - 测试 users.js 重置密码调用 `POST /api/users/{id}/reset-password` + `{ new_password }`（而非 `PUT /api/users/{id}/password` + `{ password }`）
  - 测试 users.js 禁用用户调用 `POST /api/users/{id}/disable`（而非 `PUT /api/users/{id}` + `{ enabled: false }`）
  - 测试 users.js 不调用 `DELETE /api/users/{id}` 或 `GET /api/users/{id}`
  - 测试 users.js 角色选项仅有 'admin'/'user'（无 'readonly'）
  - 测试 system.js 保存配置为嵌套结构 `{ server: {...}, agent: {...}, alert: {...}, certbot: {...}, readonly: {...}, domain_monitor: {...} }`
  - 测试 system.js 从嵌套路径加载配置（`cfg.server.listen_addr`、`cfg.certbot.email` 等）
  - 测试 dashboard.js 使用 `/api/dashboard`（无尾部斜杠）
  - 测试 machines.js 使用无尾部斜杠的 URL
  - 在未修复代码上运行测试
  - **预期结果**: 测试失败（这是正确的 - 证明当前前端代码存在 bug）
  - 记录发现的反例以了解不匹配的范围
  - 测试编写完成、运行完毕、失败已记录后标记任务完成
  - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 1.11, 1.12, 1.13, 1.14, 1.15, 1.16, 1.17, 1.18, 1.19, 1.20, 1.21, 1.22, 1.23, 1.24_

- [x] 2. 编写保持性属性测试（在实施修复之前）
  - **属性 2: 保持性** - 已正确工作的前端功能保持不变
  - **重要**: 遵循观察优先方法论
  - 观察: certificates.js API 调用（列表、详情、上传、签发、删除）在未修复代码上正确工作
  - 观察: dashboard.js 渲染统计字段（`certificates_total`、`machines_online`、`certificates_expiring_15d` 等）在未修复代码上正确
  - 观察: machines.js 创建发送 `{ name, ip, tags, remark }` 并接收 `{ machine, agent_token }` 在未修复代码上正确
  - 观察: machines.js Token 管理（regenerate-token、revoke-token、install-command）在未修复代码上正确
  - 观察: machines.js 证书部署配置 CRUD 在未修复代码上正确
  - 观察: init.js 初始化流程使用嵌套 config 结构在未修复代码上正确
  - 观察: login.js 登录流程在未修复代码上正确
  - 观察: app.js 工具函数（`App.get`、`App.post`、`App.put`、`App.delete`、`App.escapeHtml`、`App.formatDate`、`App.toast`）在未修复代码上正确
  - 观察: 所有 API 响应使用统一 `{ code, message, data }` 包装且 `resp.data` 提取在未修复代码上正确
  - 编写属性测试断言这些行为被保持:
    - certificates.js 未被修改（文件哈希不变）
    - login.js 未被修改（文件哈希不变）
    - init.js 未被修改（文件哈希不变）
    - app.js 未被修改（文件哈希不变）
    - dashboard.js 统计字段渲染逻辑（`certificates_total`、`machines_online` 等）被保持
    - machines.js 创建请求体 `{ name, ip, tags, remark }` 和响应处理 `{ machine, agent_token }` 被保持
    - machines.js Token 管理端点被保持
    - machines.js 证书部署配置 CRUD 逻辑被保持
  - 验证测试在未修复代码上通过
  - **预期结果**: 测试通过（确认需要保持的基线行为）
  - 测试编写完成、运行完毕、在未修复代码上通过后标记任务完成
  - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9_

- [x] 3. 修复 8 个 JS 文件的前端 API 不匹配问题

  - [x] 3.1 修复 domains.js - 对齐后端 Domain handler
    - 移除列表加载中的分页逻辑（`page`/`page_size` 参数）
    - 使用后端支持的过滤参数: `source`、`thirdpart_dns_id`、`monitor_enabled`
    - 修正列表渲染字段名: `d.domain` → `d.name`，添加 `d.monitor_port`、`d.monitor_enabled`、`d.source`
    - 移除不存在的字段: `d.cert_expires_at`、`d.not_after`、`d.fingerprint_match`、`d.tls_status`、`d.last_checked_at`、`d.remote_fingerprint`
    - 修正创建请求体: `{ domain, port, certificate_id }` → `{ name, monitor_port, linked_certificate_id }`
    - 移除 `probeAll()` 函数和按钮（端点 `/api/domains/probe-all` 不存在）
    - 修正详情展示使用 Domain 模型字段: `name`、`monitor_port`、`monitor_enabled`、`source`、`linked_certificate_id`
    - _Bug条件: isBugCondition(domains.js) 端点/字段与后端 handler 不匹配_
    - _期望行为: 所有 API 调用使用后端 Domain handler 的正确路由和字段名_
    - _保持性: 不修改与 API 字段/端点对齐无关的逻辑_
    - _需求: 2.1, 2.2, 2.3, 2.4_

  - [x] 3.2 修复 thirdpart-dns.js - 对齐后端 ThirdpartDNS handler
    - 修正列表渲染字段: `c.provider` → `c.type`，`c.domain` → `c.main_domains`（数组，join 显示）
    - 移除不存在的字段: `c.last_synced_at`
    - 修正创建/更新请求体: `{ name, provider, domain, api_token, zone_id, enabled }` → `{ name, type, api_token, config_json, main_domains }`
    - `config_json` 为 JSON 字符串，包含供应商特定配置（如 zone_id）
    - `main_domains` 为字符串数组
    - 移除 `syncAll()` 函数和按钮（端点 `/api/thirdpart-dns/sync-all` 不存在）
    - 修正编辑表单字段: provider/domain/zone_id → type/main_domains/config_json
    - _Bug条件: isBugCondition(thirdpart-dns.js) 字段与后端 ThirdpartDNS 模型不匹配_
    - _期望行为: 所有 API 调用使用后端 ThirdpartDNS handler 的正确路由和字段名_
    - _保持性: 不修改与 API 字段/端点对齐无关的逻辑_
    - _需求: 2.5, 2.6, 2.7_

  - [x] 3.3 修复 alerts.js - 对齐后端 Alert 和 NotificationChannel handler
    - 移除告警历史分页（`page`/`page_size` 参数）
    - 修正告警查询参数: 使用 `level`、`type`、`status` 过滤
    - 修正告警列表渲染字段: `a.severity` → `a.level`，`a.alert_type` → `a.type`，`a.message` → `a.title`/`a.content`，`a.sent` → `a.status`，`a.timestamp` → `a.created_at`
    - 添加 `a.sent_channels` 显示
    - 修正通知渠道类型选项: webhook/email/dingtalk/feishu/wechat → 仅 lark/telegram
    - 修正渠道创建/更新请求体: `{ name, type, webhook_url, enabled }` → `{ name, type, config_json, enabled }`
    - `config_json` 为 JSON 字符串，包含 webhook_url 或 bot_token
    - 修正渠道列表渲染: `c.channel_type`/`c.webhook_url`/`c.config.url` → `c.type`/`c.config_json`
    - 移除 editChannel 中的 GET 单个渠道调用（使用列表数据代替）
    - _Bug条件: isBugCondition(alerts.js) 字段/类型与后端 Alert/NotificationChannel 模型不匹配_
    - _期望行为: 所有 API 调用使用正确的路由、字段名和类型约束_
    - _保持性: 不修改与 API 字段/端点对齐无关的逻辑_
    - _需求: 2.8, 2.9, 2.10, 2.11_

  - [x] 3.4 修复 audit-logs.js - 对齐后端 AuditLog handler
    - 修正分页参数: `page`/`page_size` → `limit`/`offset`（offset = (page-1) * limit）
    - 修正过滤参数: `action`/`from`/`to` → `actor_type`/`target_type`
    - 修正列表渲染字段: `log.username`/`log.user_id` → `log.actor_type`/`log.actor_id`
    - 修正: `log.resource_type`/`log.resource_id` → `log.target_type`/`log.target_id`
    - 修正: `log.details` → `log.detail`，`log.ip_address` → `log.ip`，`log.timestamp` → `log.created_at`
    - _Bug条件: isBugCondition(audit-logs.js) 分页/过滤/字段名与后端不匹配_
    - _期望行为: 所有 API 调用使用正确的查询参数和响应字段名_
    - _保持性: 不修改与 API 字段/端点对齐无关的逻辑_
    - _需求: 2.12, 2.13_

  - [x] 3.5 修复 users.js - 对齐后端 User handler
    - 修正角色选项: 移除 'readonly'，仅提供 'admin' 和 'user'
    - 修正创建请求体: 仅发送 `{ username, password }`（不发送 role/enabled 字段）
    - 修正重置密码: `PUT /api/users/{id}/password` + `{ password }` → `POST /api/users/{id}/reset-password` + `{ new_password }`
    - 修正禁用用户: `PUT /api/users/{id}` + `{ enabled: false }` → `POST /api/users/{id}/disable`
    - 移除启用用户功能（后端仅有 disable 接口）
    - 修正更新角色: `PUT /api/users/{id}` 仅接受 `{ role }`
    - 移除删除用户按钮/函数（`DELETE /api/users/{id}` 不存在）
    - 移除 GET 单个用户调用（`GET /api/users/{id}` 不存在），使用列表数据编辑
    - 移除 `last_login_at` 字段显示（后端 User 模型无此字段）
    - _Bug条件: isBugCondition(users.js) 端点/方法/字段与后端 User handler 不匹配_
    - _期望行为: 所有 API 调用使用正确的路由、方法和字段名_
    - _保持性: 不修改与 API 字段/端点对齐无关的逻辑_
    - _需求: 2.14, 2.15, 2.16, 2.17, 2.18, 2.19_

  - [x] 3.6 修复 system.js - 对齐后端 config 结构
    - 修正配置加载: 从嵌套结构读取（`cfg.server.listen_addr`、`cfg.server.external_url`、`cfg.certbot.email`、`cfg.certbot.binary_path`、`cfg.certbot.data_dir`、`cfg.alert.default_before_days`、`cfg.agent.heartbeat_timeout_seconds`、`cfg.agent.poll_interval_seconds`、`cfg.readonly.enabled`、`cfg.readonly.view_password`、`cfg.domain_monitor.default_port`、`cfg.domain_monitor.interval_minutes`）
    - 修正配置保存: 发送完整嵌套的 `config.Config` 结构:
      ```
      { server: { external_url, listen_addr },
        agent: { heartbeat_timeout_seconds, poll_interval_seconds },
        alert: { default_before_days },
        certbot: { binary_path, data_dir, email },
        readonly: { enabled, view_password },
        domain_monitor: { default_port, interval_minutes } }
      ```
    - 处理敏感字段掩码: 如果 `readonly.view_password` 为 `"***"`，保存时保持原值
    - _Bug条件: isBugCondition(system.js) 扁平结构与嵌套 config.Config 不匹配_
    - _期望行为: 加载从嵌套路径读取，保存发送完整嵌套结构_
    - _保持性: 不修改与配置结构对齐无关的逻辑_
    - _需求: 2.20, 2.21_

  - [x] 3.7 修复 dashboard.js - 移除 URL 尾部斜杠
    - 将 `/api/dashboard/` → `/api/dashboard`
    - _Bug条件: isBugCondition(dashboard.js) 尾部斜杠可能导致 301 重定向或 404_
    - _期望行为: URL 与后端路由注册匹配，无尾部斜杠_
    - _保持性: 不修改统计字段渲染逻辑（certificates_total、machines_online 等）_
    - _需求: 2.22_

  - [x] 3.8 修复 machines.js - 移除 URL 尾部斜杠
    - 修正: `/api/machines/{id}/` → `/api/machines/{id}`
    - 修正: `DELETE /api/machines/{id}/` → `DELETE /api/machines/{id}`
    - 修正: `/api/machines/{id}/certificates/` → `/api/machines/{id}/certificates`
    - 修正: `/api/machines/{id}/certificates/{mc_id}/` → `/api/machines/{id}/certificates/{mc_id}`
    - _Bug条件: isBugCondition(machines.js) 尾部斜杠可能导致路由不匹配_
    - _期望行为: 所有 URL 与后端路由注册匹配，无尾部斜杠_
    - _保持性: 不修改创建请求体、Token 管理或证书部署配置逻辑_
    - _需求: 2.23, 2.24_

  - [x] 3.9 验证 Bug 条件探索测试现在通过
    - **属性 1: 期望行为** - 前端 API 调用与后端契约匹配
    - **重要**: 重新运行任务 1 中的同一测试 - 不要编写新测试
    - 任务 1 的测试编码了期望行为（正确的端点、字段、请求体）
    - 测试通过即确认所有 8 个文件现在使用正确的 API 契约
    - 运行步骤 1 的 bug 条件探索测试
    - **预期结果**: 测试通过（确认所有 API 不匹配已修复）
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 2.12, 2.13, 2.14, 2.15, 2.16, 2.17, 2.18, 2.19, 2.20, 2.21, 2.22, 2.23, 2.24_

  - [x] 3.10 验证保持性测试仍然通过
    - **属性 2: 保持性** - 已正确工作的前端功能保持不变
    - **重要**: 重新运行任务 2 中的同一测试 - 不要编写新测试
    - 运行步骤 2 的保持性属性测试
    - **预期结果**: 测试通过（确认无回归）
    - 确认 certificates.js、login.js、init.js、app.js 未被修改
    - 确认 dashboard.js 统计渲染和 machines.js 创建/Token 逻辑被保持
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9_

- [x] 4. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题询问用户
  - 验证所有 8 个前端 JS 文件已更新
  - 验证未修改任何后端代码
  - 验证无关前端文件（certificates.js、login.js、init.js、app.js）未被修改
  - 运行完整测试套件确认无回归
