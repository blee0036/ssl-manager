# Bugfix Requirements Document

## Introduction

前端 JS 文件（`web/static/js/` 目录下）中的 API 调用与后端 Go handler 实际实现存在大面积不匹配。涉及几乎所有页面：域名监控（domains.js）、告警管理（alerts.js）、第三方 DNS 配置（thirdpart-dns.js）、审计日志（audit-logs.js）、用户管理（users.js）、系统配置（system.js）、仪表盘（dashboard.js）、机器管理（machines.js）。

这些不匹配导致用户部署后打开 Web 管理界面时，初始化配置保存、系统参数保存、证书上传/签发、机器安装命令、机器证书部署配置、域名监控、Cloudflare DNS、告警渠道、用户管理等验收主流程全部失败或缺少入口。

根本原因：前端 JS 由 AI 子代理生成时未读取后端 handler 实际代码，凭空想象了 API 结构、字段名、请求格式和可用端点。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN domains.js 加载域名列表时使用 `page`/`page_size` 分页参数 THEN 后端忽略这些参数（后端不支持分页），前端分页逻辑无法正常工作

1.2 WHEN domains.js 渲染域名列表时使用 `d.domain`、`d.cert_expires_at`、`d.not_after`、`d.fingerprint_match`、`d.tls_status`、`d.last_checked_at`、`d.remote_fingerprint` 等字段 THEN 这些字段在后端 Domain 模型中不存在（后端使用 `name`、`monitor_port`、`monitor_enabled` 等字段），页面显示空白

1.3 WHEN domains.js 创建域名时发送 `{ domain, port, certificate_id }` THEN 后端 CreateDomainInput 期望 `{ name, monitor_port, linked_certificate_id }`，创建操作失败

1.4 WHEN domains.js 调用 `/api/domains/probe-all` THEN 后端不存在此端点，请求返回 404

1.5 WHEN thirdpart-dns.js 渲染 DNS 配置列表时使用 `c.provider`、`c.domain`、`c.last_synced_at` 字段 THEN 后端 ThirdpartDNS 模型使用 `type`、`main_domains`（数组）、无 last_synced_at 字段，页面显示空白

1.6 WHEN thirdpart-dns.js 创建/更新 DNS 配置时发送 `{ name, provider, domain, api_token, zone_id, enabled }` THEN 后端 CreateThirdpartDNSInput 期望 `{ name, type, api_token, config_json, main_domains }`，操作失败

1.7 WHEN thirdpart-dns.js 调用 `/api/thirdpart-dns/sync-all` THEN 后端不存在此端点，请求返回 404

1.8 WHEN alerts.js 加载告警历史时使用 `page`/`page_size` 分页参数 THEN 后端不支持分页参数，前端分页逻辑无法正常工作

1.9 WHEN alerts.js 渲染告警列表时使用 `a.severity`、`a.alert_type`、`a.message`、`a.sent`、`a.timestamp` 字段 THEN 后端 Alert 模型使用 `level`、`type`、`title`/`content`、`status`、`sent_channels`、`created_at`，页面显示空白

1.10 WHEN alerts.js 创建通知渠道时发送 `{ name, type, webhook_url, enabled }` 且 type 可选 webhook/email/dingtalk/feishu/wechat THEN 后端期望 `{ name, type, config_json, enabled }` 且 type 只允许 'lark' 或 'telegram'，创建操作失败

1.11 WHEN alerts.js 渲染通知渠道列表时使用 `c.channel_type`、`c.webhook_url`、`c.config.url` 字段 THEN 后端 NotificationChannel 模型使用 `type`、`config_json`（JSON 字符串），页面显示不正确

1.12 WHEN audit-logs.js 加载审计日志时使用 `page`/`page_size`/`action`/`from`/`to` 参数 THEN 后端支持 `limit`/`offset`/`actor_type`/`target_type` 参数，过滤和分页无法正常工作

1.13 WHEN audit-logs.js 渲染审计日志时使用 `log.username`、`log.user_id`、`log.resource_type`、`log.resource_id`、`log.details`、`log.ip_address`、`log.timestamp` 字段 THEN 后端 AuditLog 模型使用 `actor_type`、`actor_id`、`target_type`、`target_id`、`detail`、`ip`、`created_at`，页面显示空白

1.14 WHEN users.js 提供 `readonly` 角色选项 THEN 后端 CreateUserInput 只有 `username`/`password` 字段，只允许 `admin` 和 `user` 角色，创建用户时角色设置无效

1.15 WHEN users.js 重置密码调用 `PUT /api/users/{id}/password` 并发送 `{ password }` THEN 后端实际路由是 `POST /api/users/{id}/reset-password` 且字段为 `new_password`，重置密码失败

1.16 WHEN users.js 启停用户调用 `PUT /api/users/{id}` 传 `{ enabled }` THEN 后端该接口只接受 `UpdateUserRoleInput { role }` 用于改角色，启停操作无效

1.17 WHEN users.js 禁用用户时没有调用正确的禁用接口 THEN 后端提供 `POST /api/users/{id}/disable` 专用接口，但前端未使用

1.18 WHEN users.js 调用 `DELETE /api/users/{id}` 删除用户 THEN 后端不存在删除用户路由，请求返回 404/405

1.19 WHEN users.js 调用 `GET /api/users/{id}` 获取单个用户详情 THEN 后端不存在该路由，编辑用户功能无法工作

1.20 WHEN system.js 保存配置时发送扁平字段 `{ listen_addr, certbot_email, cert_renew_before_days, ... }` THEN 后端 `PUT /api/system/config` 期望完整嵌套的 `config.Config` 结构（`server.listen_addr`、`certbot.email`、`domain_monitor.interval_minutes` 等），保存因缺少必填字段而失败

1.21 WHEN system.js 加载配置后尝试读取 `cfg.listen_addr`、`cfg.certbot_email` 等扁平字段 THEN 后端返回嵌套结构 `cfg.server.listen_addr`、`cfg.certbot.email`，页面显示空白（虽然代码有 fallback 到嵌套路径，但保存时仍用扁平结构）

1.22 WHEN dashboard.js 使用 `/api/dashboard/` 末尾带斜杠 THEN 后端路由注册为 `r.Get("/", h.GetDashboard)` 在 `/api/dashboard` 路由组下，chi 路由器可能因尾部斜杠处理导致 301 重定向或 404

1.23 WHEN machines.js 调用 `GET /api/machines/{id}/` 和 `DELETE /api/machines/{id}/` 时 URL 末尾带斜杠 THEN 后端路由注册为 `/{id}` 下的子路由，可能因尾部斜杠不一致导致请求失败

1.24 WHEN machines.js 调用 `GET /api/machines/{id}/certificates/` 和 `DELETE /api/machines/{id}/certificates/{mc_id}/` 时 URL 末尾带斜杠 THEN 可能因路由匹配问题导致请求失败

### Expected Behavior (Correct)

2.1 WHEN domains.js 加载域名列表时 THEN 系统 SHALL 调用 `GET /api/domains` 并使用后端支持的过滤参数（`source`、`thirdpart_dns_id`、`monitor_enabled`），不使用分页参数

2.2 WHEN domains.js 渲染域名列表时 THEN 系统 SHALL 使用后端 Domain 模型的正确字段名：`name`、`monitor_port`、`monitor_enabled`、`linked_certificate_id`、`source`、`created_at`、`updated_at`

2.3 WHEN domains.js 创建域名时 THEN 系统 SHALL 发送 `{ name, monitor_port, linked_machine_id, linked_certificate_id, linked_machine_certificate_id }` 格式的请求体

2.4 WHEN 用户需要探测所有域名时 THEN 系统 SHALL 移除 probe-all 按钮或改为逐个调用各域名的 `POST /api/domains/{id}/probe` 接口

2.5 WHEN thirdpart-dns.js 渲染 DNS 配置列表时 THEN 系统 SHALL 使用后端 ThirdpartDNS 模型的正确字段名：`type`（而非 provider）、`main_domains`（数组，而非单个 domain）、`config_json`、`enabled`

2.6 WHEN thirdpart-dns.js 创建 DNS 配置时 THEN 系统 SHALL 发送 `{ name, type, api_token, config_json, main_domains }` 格式的请求体

2.7 WHEN thirdpart-dns.js 调用同步时 THEN 系统 SHALL 仅使用 `POST /api/thirdpart-dns/{id}/sync` 单个同步接口，移除不存在的 sync-all 功能

2.8 WHEN alerts.js 加载告警历史时 THEN 系统 SHALL 调用 `GET /api/alerts` 并使用后端支持的过滤参数（`level`、`type`、`status`），不使用分页参数

2.9 WHEN alerts.js 渲染告警列表时 THEN 系统 SHALL 使用后端 Alert 模型的正确字段名：`level`、`type`、`title`、`content`、`status`、`sent_channels`、`created_at`、`resolved_at`

2.10 WHEN alerts.js 创建通知渠道时 THEN 系统 SHALL 发送 `{ name, type, config_json, enabled }` 格式的请求体，且 type 值为 'lark' 或 'telegram'

2.11 WHEN alerts.js 渲染通知渠道列表时 THEN 系统 SHALL 使用后端 NotificationChannel 模型的正确字段名：`type`、`config_json`、`enabled`、`created_at`

2.12 WHEN audit-logs.js 加载审计日志时 THEN 系统 SHALL 使用后端支持的查询参数 `limit`、`offset`、`actor_type`、`target_type`

2.13 WHEN audit-logs.js 渲染审计日志时 THEN 系统 SHALL 使用后端 AuditLog 模型的正确字段名：`actor_type`、`actor_id`、`action`、`target_type`、`target_id`、`detail`、`ip`、`created_at`

2.14 WHEN users.js 创建用户时 THEN 系统 SHALL 发送 `{ username, password }` 并且角色选项只提供 'admin' 和 'user'

2.15 WHEN users.js 重置密码时 THEN 系统 SHALL 调用 `POST /api/users/{id}/reset-password` 并发送 `{ new_password }`

2.16 WHEN users.js 禁用用户时 THEN 系统 SHALL 调用 `POST /api/users/{id}/disable`

2.17 WHEN users.js 更新用户角色时 THEN 系统 SHALL 调用 `PUT /api/users/{id}` 并发送 `{ role }`

2.18 WHEN users.js 需要删除用户功能时 THEN 系统 SHALL 移除删除按钮（后端不支持删除用户，只支持禁用）

2.19 WHEN users.js 需要编辑用户时 THEN 系统 SHALL 移除 `GET /api/users/{id}` 调用（后端不存在该路由），改为从列表数据中获取用户信息

2.20 WHEN system.js 保存配置时 THEN 系统 SHALL 发送完整嵌套的 `config.Config` 结构：`{ server: { external_url, listen_addr }, agent: { heartbeat_timeout_seconds, poll_interval_seconds }, alert: { default_before_days }, certbot: { binary_path, data_dir, email }, readonly: { enabled, view_password }, domain_monitor: { default_port, interval_minutes } }`

2.21 WHEN system.js 加载配置时 THEN 系统 SHALL 从嵌套结构中读取字段（`cfg.server.listen_addr`、`cfg.certbot.email` 等）

2.22 WHEN dashboard.js 调用仪表盘 API 时 THEN 系统 SHALL 使用不带末尾斜杠的 URL `/api/dashboard`

2.23 WHEN machines.js 调用机器相关 API 时 THEN 系统 SHALL 使用不带末尾斜杠的 URL 格式（`/api/machines/{id}` 而非 `/api/machines/{id}/`）

2.24 WHEN machines.js 调用机器证书部署配置 API 时 THEN 系统 SHALL 使用不带末尾斜杠的 URL 格式（`/api/machines/{machine_id}/certificates` 和 `/api/machines/{machine_id}/certificates/{mc_id}`）

### Unchanged Behavior (Regression Prevention)

3.1 WHEN certificates.js 调用证书列表、详情、上传、Cloudflare 签发、手动 DNS 签发、删除等 API 时 THEN 系统 SHALL CONTINUE TO 使用正确的端点和字段名（`/api/certificates/`、`name`、`domains`、`expire_at`、`fingerprint_sha256`、`/api/certificates/issue/cloudflare`、`/api/certificates/issue/manual-dns/start`、`/api/certificates/issue/manual-dns/complete`）

3.2 WHEN dashboard.js 渲染仪表盘统计数据时 THEN 系统 SHALL CONTINUE TO 使用正确的字段名（`certificates_total`、`certificates_expiring_15d`、`certificates_expired`、`machines_online`、`machines_offline`、`deploy_failures_24h`、`renew_failures_24h`、`domain_anomalies`、`has_anomalies`）

3.3 WHEN machines.js 创建机器时发送 `{ name, ip, tags, remark }` 并接收 `{ machine, agent_token }` 响应 THEN 系统 SHALL CONTINUE TO 正确处理机器创建流程和 Token 展示

3.4 WHEN machines.js 调用 regenerate-token、revoke-token、install-command 接口时 THEN 系统 SHALL CONTINUE TO 正确处理 Token 管理和安装命令获取

3.5 WHEN machines.js 管理机器证书部署配置（列表、创建、部署触发、删除）时 THEN 系统 SHALL CONTINUE TO 使用正确的嵌套路由 `/api/machines/{machine_id}/certificates` 和正确的请求体字段

3.6 WHEN init.js 处理系统初始化流程（检查状态、创建管理员、保存配置）时 THEN 系统 SHALL CONTINUE TO 使用正确的嵌套 config 结构提交到 `/init/config`

3.7 WHEN login.js 处理用户登录时 THEN 系统 SHALL CONTINUE TO 正常工作

3.8 WHEN app.js 提供的通用工具函数（`App.get`、`App.post`、`App.put`、`App.delete`、`App.escapeHtml`、`App.formatDate`、`App.toast` 等）被各页面调用时 THEN 系统 SHALL CONTINUE TO 正常工作

3.9 WHEN 所有 API 响应使用统一的 `{ code, message, data }` 包装格式时 THEN 系统 SHALL CONTINUE TO 通过 `resp.data` 正确提取业务数据
