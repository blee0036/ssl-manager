# SSL 证书管理系统需求文档

## 1. 背景

当前 SSL 证书通常分散在多台 Linux 服务器、多个域名和多个服务中。常见问题包括证书过期不可见、证书更新后需要人工分发、服务 reload 容易遗漏、机器在线状态无法集中感知、域名线上证书状态无法统一监控等。

本系统目标是建设一个轻量、可自部署的 SSL 证书管理 Web 系统，通过 Web 管理端集中管理证书、机器、部署路径、域名监控和告警，通过 Agent 在 Linux 机器上自动同步证书、写入文件、执行 reload 或自定义命令。

## 2. 技术约束

- Web 后端使用 Golang 开发。
- Agent 端使用 Golang 开发。
- 运行环境暂时只考虑 Linux 系列系统。
- 数据库存储使用 SQLite3。
- 全局配置使用 `config.json`。
- 证书不做多版本管理，续签或手动上传更新时直接覆盖当前证书内容。
- 自动续签由本系统调度器触发，不依赖 Certbot 自带的 systemd timer 或 `certbot renew` 自动续签机制。

## 3. 建设目标

### 3.1 核心目标

- 集中管理所有 SSL 证书，包括单域名证书、泛域名证书和多域名 SAN 复合证书。
- 支持手动上传证书。
- 支持使用 Certbot 签发证书。
- 支持 Certbot + Cloudflare DNS 自动认证签发和续签。
- 支持手动 DNS TXT 验证签发证书。
- 支持证书自动续签，默认在证书过期前 15 天触发。
- 支持 Web 端添加机器，并生成包含 Web 地址和 Agent Token 的安装命令。
- 支持 Agent 安装后定期同步状态，Web 端显示在线或离线。
- 支持在机器页面添加证书部署配置。
- 支持 Agent 自动下载证书、写入证书路径和私钥路径、设置默认权限、执行部署后命令。
- 支持部署日志收集，在机器详情下查看最近 30 条部署日志。
- 支持独立监控域名 SSL 证书状态，域名可以关联机器，也可以不关联机器。
- 支持从 Cloudflare API 自动获取 A、AAAA、CNAME 记录。
- 支持 Lark 和 Telegram 告警。
- 支持 Web 首次部署后的初始化流程，创建系统管理员用户并配置系统参数。

### 3.2 非目标

- 不做复杂 CMDB。
- 不做证书多版本回滚。
- 不做复杂 RBAC。
- 不做 Windows Server 支持。
- 不直接管理 Nginx、Apache、Docker Compose 等服务配置文件。
- 不代替专业漏洞扫描系统，只做证书、域名、Agent 和部署相关监控。

## 4. 系统组成

系统由两部分组成：

- Web 管理端：提供用户界面、API、SQLite3 存储、证书管理、机器管理、任务调度、告警发送。
- Agent 端：部署在 Linux 机器上，负责心跳、拉取证书部署配置、下载证书、写入本地文件、执行部署后命令、上报日志。

推荐整体结构：

```text
Browser
  |
  v
Go Web Server
  |
  +-- SQLite3
  |
  +-- config.json
  |
  +-- Scheduler
  |
  +-- Certbot Wrapper
  |
  +-- Alert Sender
  |
  +<-> Go Agent on Linux Servers
```

## 5. 存储设计

### 5.1 SQLite3

SQLite3 保存业务数据：

- 用户。
- 机器。
- Agent Token 状态。
- 证书元数据和证书内容。
- 机器证书部署配置。
- 部署日志。
- 域名监控对象。
- 域名监控结果。
- Cloudflare 同步记录。
- 告警记录。
- 审计日志。

### 5.2 config.json

`config.json` 保存全局配置：

- Web 监听地址。
- Web 外部访问地址。
- SQLite3 数据库路径。
- Agent 安装脚本下载地址。
- Agent 心跳超时时间。
- 默认续签阈值，默认 15 天。
- 域名监控频率。
- 域名监控默认端口，默认 443。
- 告警渠道全局开关。
- Lark 默认配置。
- Telegram 默认配置。
- Cloudflare 默认配置。
- 只读查看密码。
- Certbot 可执行文件路径。
- Certbot 工作目录，用于签发过程中的临时文件和插件配置。

证书 PEM 内容以 SQLite3 中保存的数据为准。Certbot 工作目录只作为签发和续签过程中的临时工作目录，不作为证书管理的主存储。

示例：

```json
{
  "server": {
    "listen": "0.0.0.0:8080",
    "external_url": "https://ssl.example.com"
  },
  "database": {
    "path": "./data/ssl-manager.db"
  },
  "agent": {
    "heartbeat_timeout_seconds": 180,
    "default_poll_interval_seconds": 60
  },
  "renew": {
    "default_before_days": 15
  },
  "domain_monitor": {
    "default_port": 443
  },
  "readonly": {
    "enabled": true,
    "view_password": "change-me"
  },
  "certbot": {
    "binary": "/usr/bin/certbot",
    "work_dir": "./data/certbot"
  }
}
```

生产环境要求：

- `config.json` 文件权限建议为 `0600`。
- 如果 `config.json` 内保存 Token 或密码，应限制系统用户访问权限。
- 后续可以增加配置项加密，但 MVP 阶段以文件权限保护为主。

## 6. 用户与权限

### 6.1 用户角色

系统只有两类登录用户：

- 系统管理员。
- 用户。

权限差异：

- 系统管理员可以添加、编辑、禁用用户。
- 用户不能管理用户。
- 除用户管理外，系统管理员和用户拥有相同操作权限。

### 6.2 只读查看模式

除正式登录用户外，系统支持只读查看模式：

- 在 `config.json` 中配置只读密码。
- 访问只读入口时，只需要输入 `viewpassword`。
- 通过只读密码进入后，只能查看数据，不能执行任何新增、编辑、删除、续签、部署、重启、同步等操作。
- 只读模式适合临时查看证书、机器、域名、部署日志和告警状态。

## 7. 初始化流程

Web 端首次部署后必须进入初始化流程。

初始化判断：

- SQLite3 数据库不存在。
- 或数据库中不存在系统管理员用户。

初始化步骤：

1. 访问 Web 后自动跳转到 `/init`。
2. 创建系统管理员用户。
3. 设置管理员用户名和密码。
4. 初始化 SQLite3 数据库表。
5. 跳转到系统参数配置页面。
6. 配置 Web 外部访问地址、证书存储目录、Agent 默认参数、告警参数、Certbot 参数等。
7. 保存配置后进入仪表盘。

初始化完成后：

- 禁止再次访问 `/init` 创建管理员。
- 如需重新初始化，必须通过命令行维护工具或删除数据库后重新启动。

## 8. 证书管理

### 8.1 证书字段

证书管理页面必须明确展示以下字段：

- 证书 ID。
- 证书覆盖域名。
- 证书来源。
- 过期时间。
- 是否自动续签。

建议额外展示：

- 证书名称。
- 主域名。
- 颁发者。
- 剩余有效天数。
- 指纹。
- 最近更新时间。
- 续签状态。
- 已关联机器数量。

### 8.2 证书来源

证书来源包括：

- 手动上传。
- Certbot + Cloudflare DNS 自动认证。
- Certbot + 手动 DNS 验证。

### 8.3 证书内容

每个证书对象保存当前证书内容：

- certificate PEM。
- private key PEM。
- chain PEM。
- fullchain PEM。

系统不做证书多版本管理。

当发生以下操作时，直接覆盖当前证书内容：

- 用户点击更新并重新上传证书。
- 自动续签成功。
- 用户手动触发 Certbot 重新签发成功。

覆盖后需要：

- 重新解析覆盖域名。
- 更新过期时间。
- 更新指纹。
- 更新证书来源。
- 更新最近更新时间。
- 标记所有关联机器的部署状态为待同步。

### 8.4 证书解析

上传或签发成功后，系统需要自动解析：

- 覆盖域名列表，包括 CN 和 SAN。
- 证书过期时间。
- 颁发者。
- 指纹。
- 私钥是否匹配证书。
- 证书链是否完整。

如果证书和私钥不匹配，禁止保存。

### 8.5 证书自动续签

自动续签由本系统自身调度器控制。

默认规则：

- 当证书开启自动续签，并且距离过期时间小于等于 15 天时，系统触发续签。
- 续签成功后直接覆盖原证书内容。
- 续签失败后记录错误并发送告警。
- 续签失败后按系统配置重试。

重要约束：

- 使用 Certbot + Cloudflare DNS 自动认证时，系统调用 Certbot 执行签发或续签。
- 系统不启用 Certbot 自带的自动续签机制。
- 系统不依赖 `certbot renew`、Certbot systemd timer 或 cron。
- 每次续签任务由 Web 后端调度器判断过期时间并主动发起。

Cloudflare DNS 自动认证续签流程：

1. 系统扫描到证书距离过期时间小于等于 15 天。
2. 判断该证书来源为 Certbot + Cloudflare DNS，且自动续签开启。
3. 系统读取 Cloudflare API 配置。
4. 系统调用 Certbot 使用 DNS-01 验证方式重新签发证书。
5. Cloudflare DNS TXT 记录由 Certbot DNS 插件或系统封装逻辑自动处理。
6. 签发成功后覆盖原证书内容。
7. 系统标记关联机器证书待同步。
8. Agent 在下次同步时下载新证书并部署。

手动 DNS 验证不建议自动续签，因为需要人工添加 TXT 记录。系统可以在过期前发送提醒，并允许用户手动触发重新签发。

## 9. 机器与 Agent 管理

### 9.1 机器管理

Web 端支持添加机器：

- 机器名称。
- IP。
- 备注。
- 标签。

添加机器后，系统生成 Agent 安装命令。

安装命令包含：

- Web 外部访问地址。
- 机器 ID。
- 长效 Agent Token。

示例：

```bash
curl -fsSL https://ssl.example.com/agent/install.sh | sudo bash -s -- \
  --server https://ssl.example.com \
  --machine-id <machine-id> \
  --token <agent-token>
```

### 9.2 Agent Token

Agent Token 简化为长效 Token：

- 每台机器一个长效 Agent Token。
- Agent 安装时写入本地配置文件。
- Web 后端校验机器 ID 和 Token。
- Token 支持 revoke。
- revoke 后该 Agent 不再允许心跳、拉取配置、下载证书或上报日志。
- 管理员可以重新生成 Token 并重新安装 Agent。

不需要：

- 一次性注册 Token。
- Token 多阶段状态。
- 短期注册 Token 和长期运行 Token 分离。

### 9.3 Agent 安装

Agent 安装脚本需要完成：

- 检查 Linux 系统环境。
- 下载 Golang 编译后的 Agent 二进制文件。
- 创建配置目录，例如 `/etc/ssl-manager-agent/`。
- 写入配置文件。
- 创建 systemd 服务。
- 启动 Agent。
- 设置开机自启。

Agent 配置示例：

```yaml
server_url: https://ssl.example.com
machine_id: machine_123
agent_token: token_xxx
poll_interval_seconds: 60
log_level: info
```

### 9.4 Agent 心跳

Agent 定期向 Web 后端发送心跳。

心跳内容：

- machine_id。
- Agent 版本。
- 主机名。
- IP。
- OS。
- Arch。
- 当前时间。
- 最近部署状态摘要。

机器状态：

- pending：已添加机器，但 Agent 从未成功心跳。
- online：最近心跳正常。
- offline：超过心跳超时时间未收到心跳。
- revoked：Agent Token 已吊销。
- disabled：机器被管理员禁用。

## 10. 证书部署配置

### 10.1 配置入口

证书部署配置放在机器详情页面。

页面路径建议：

```text
机器列表 -> 机器详情 -> 证书 -> 添加证书
```

不单独设计复杂的全局部署策略页面。

### 10.2 添加证书部署

在机器页面点击添加证书后，表单字段为：

- 选择证书。
- 证书路径。
- 私钥路径。
- 部署后执行命令。

证书下拉框展示信息：

- 证书 ID。
- 证书覆盖域名。
- 过期时间。
- 是否自动续签。

示例展示：

```text
cert_001 | *.example.com, example.com | 2026-08-01 | 自动续签: 是
```

### 10.3 文件路径

用户需要配置：

- 证书路径，例如 `/etc/nginx/ssl/example.com/fullchain.pem`。
- 私钥路径，例如 `/etc/nginx/ssl/example.com/privkey.pem`。

MVP 阶段只要求写入：

- fullchain 到证书路径。
- private key 到私钥路径。

后续如果需要，可以再扩展 cert、chain、pfx 等输出格式。

### 10.4 文件权限

MVP 阶段权限使用默认策略：

- 证书文件默认 `0644`。
- 私钥文件默认 `0600`。
- owner 和 group 默认为 Agent 运行用户。

暂不在页面上暴露复杂权限配置。

如后续需要支持 Nginx 等服务读取私钥，可以再增加 owner、group、mode 配置项。

### 10.5 部署后执行命令

每个机器证书配置支持填写部署后执行命令。

规则：

- 支持多行命令。
- 按行顺序执行。
- 默认以 Agent 运行用户执行。
- 每条命令记录 stdout、stderr、exit code。
- 任意命令失败时，本次部署标记为失败。
- 命令必须有超时时间，默认 60 秒。

示例：

```bash
nginx -t
systemctl reload nginx
docker restart gateway-nginx
```

MVP 阶段不支持：

- 多命令组。
- 自定义执行用户。
- 前置命令。
- 复杂失败策略。

## 11. Agent 同步与部署流程

Agent 周期性执行：

1. 发送心跳。
2. 拉取当前机器的证书部署配置。
3. 检查本地证书文件是否存在。
4. 检查本地证书指纹是否与 Web 端证书一致。
5. 如果不同步，下载证书内容。
6. 校验证书和私钥是否匹配。
7. 写入临时文件。
8. 原子替换目标证书路径和私钥路径。
9. 设置默认权限。
10. 执行部署后命令。
11. 上报部署状态和日志。

部署成功条件：

- 证书文件写入成功。
- 私钥文件写入成功。
- 权限设置成功。
- 所有部署后命令执行成功。

部署失败要求：

- 不删除旧证书。
- 不覆盖到半写入状态。
- 上报错误原因。
- 上报命令输出。

## 12. 部署日志

部署日志显示位置：

```text
机器 -> 证书 -> 部署信息 -> 日志
```

每个机器证书部署配置默认保留最近 30 条日志。

日志内容：

- 部署时间。
- 证书 ID。
- 证书覆盖域名。
- 证书指纹。
- 部署状态。
- 证书路径。
- 私钥路径。
- 执行的命令。
- 每条命令 stdout。
- 每条命令 stderr。
- 每条命令 exit code。
- 总耗时。
- 错误信息。

日志状态：

- success：部署成功。
- failed：部署失败。
- skipped：证书未变化，跳过部署。

## 13. 域名 SSL 状态监控

域名监控对象可以来自：

- 手动添加。
- 从证书覆盖域名自动生成。
- 从 Cloudflare API 自动导入 A、AAAA、CNAME 记录。

每个域名监控对象支持配置检测端口，默认端口为 443。

域名可以关联：

- 某台机器。
- 某张证书。
- 某个机器证书部署配置。

也可以不关联任何机器，只作为外部域名监控对象。

监控内容：

- DNS 是否可解析。
- 解析结果。
- 配置的检测端口是否可连接，默认 443。
- TLS 握手是否成功。
- 线上证书是否过期。
- 线上证书剩余有效期。
- 线上证书覆盖域名是否包含当前域名。
- 线上证书指纹是否与系统内证书一致。
- 证书链是否完整。
- 颁发者。

探测要求：

- 使用 SNI 探测。
- 支持为每个域名单独配置检测端口。
- 支持配置探测超时时间。
- 支持手动触发探测。

## 14. Cloudflare 同步

系统支持配置 Cloudflare API，用于：

- Certbot DNS 自动认证。
- 自动获取域名解析记录。

Cloudflare 同步内容：

- Zone。
- A 记录。
- AAAA 记录。
- CNAME 记录。

同步流程：

1. 系统按配置读取 Cloudflare API Token。
2. 拉取指定 Zone 下的 DNS 记录。
3. 过滤 A、AAAA、CNAME。
4. 写入本地域名列表。
5. 可选自动创建域名 SSL 监控对象。
6. 记录同步结果和错误信息。

## 15. 告警通知

支持渠道：

- Lark。
- Telegram。

告警事件：

- 证书距离过期时间小于等于 15 天。
- 证书已经过期。
- 自动续签失败。
- Agent 离线。
- Agent Token 被 revoke 后仍有请求。
- 证书部署失败。
- 域名 SSL 探测失败。
- 线上证书与系统证书不一致。
- Cloudflare 同步失败。

告警能力：

- 支持测试发送。
- 支持启用和禁用渠道。
- 支持重复告警抑制。
- 支持恢复通知。

## 16. Web 页面清单

- 初始化页面。
- 登录页面。
- 只读查看入口。
- 仪表盘。
- 用户管理页面。
- 系统参数配置页面。
- 机器列表页面。
- 机器详情页面。
- Agent 安装命令弹窗。
- 机器证书配置页面或标签页。
- 机器证书部署日志页面或抽屉。
- 证书列表页面。
- 证书详情页面。
- 证书上传和更新页面。
- Certbot 签发页面。
- 域名列表页面。
- 域名详情页面。
- Cloudflare 配置页面。
- 告警渠道配置页面。
- 告警历史页面。
- 审计日志页面。

## 17. 核心页面行为

### 17.1 仪表盘

展示：

- 证书总数。
- 15 天内过期证书数量。
- 已过期证书数量。
- 在线机器数量。
- 离线机器数量。
- 最近部署失败。
- 最近续签失败。
- 域名 SSL 异常数量。

### 17.2 证书列表

展示字段：

- 证书 ID。
- 覆盖域名。
- 来源。
- 过期时间。
- 剩余天数。
- 是否自动续签。
- 关联机器数量。
- 最近更新时间。

操作：

- 上传新证书。
- 更新上传证书。
- Certbot 签发。
- 手动触发续签。
- 删除证书。

### 17.3 机器详情

展示：

- 机器名称。
- IP。
- Agent 状态。
- Agent 版本。
- 最近心跳时间。
- 机器证书列表。

机器证书列表展示：

- 证书 ID。
- 覆盖域名。
- 过期时间。
- 是否自动续签。
- 证书路径。
- 私钥路径。
- 最近部署状态。
- 最近部署时间。

操作：

- 添加证书。
- 编辑证书路径。
- 编辑私钥路径。
- 编辑部署后命令。
- 手动触发部署。
- 查看部署日志。
- 删除机器上的证书配置。

## 18. 数据模型草案

### 18.1 users

- id
- username
- password_hash
- role
- enabled
- created_at
- updated_at

role 可选值：

- admin
- user

### 18.2 machines

- id
- name
- ip
- hostname
- os
- arch
- tags
- remark
- status
- agent_version
- agent_token_hash
- agent_token_revoked_at
- last_heartbeat_at
- created_at
- updated_at

### 18.3 certificates

- id
- name
- domains
- source
- expire_at
- auto_renew
- issuer
- fingerprint_sha256
- cert_pem
- private_key_pem
- chain_pem
- fullchain_pem
- last_renew_at
- renew_status
- created_at
- updated_at

source 可选值：

- upload
- certbot_cloudflare_dns
- certbot_manual_dns

### 18.4 machine_certificates

- id
- machine_id
- certificate_id
- cert_path
- private_key_path
- post_deploy_commands
- last_deploy_status
- last_deploy_at
- last_deploy_message
- created_at
- updated_at

### 18.5 deployment_logs

- id
- machine_certificate_id
- machine_id
- certificate_id
- status
- cert_fingerprint_sha256
- cert_path
- private_key_path
- command_outputs
- error_message
- started_at
- finished_at
- created_at

保留策略：

- 每个 `machine_certificate_id` 默认只保留最近 30 条。
- 超出数量后自动删除更旧日志。

### 18.6 domains

- id
- name
- source
- dns_record_type
- dns_record_value
- monitor_port
- linked_machine_id
- linked_certificate_id
- linked_machine_certificate_id
- monitor_enabled
- created_at
- updated_at

### 18.7 domain_monitor_results

- id
- domain_id
- checked_port
- resolved_ips
- tls_success
- certificate_fingerprint_sha256
- issuer
- expire_at
- days_remaining
- domain_matched
- chain_valid
- error_message
- checked_at

### 18.8 alerts

- id
- level
- type
- title
- content
- status
- target_type
- target_id
- sent_channels
- created_at
- resolved_at

### 18.9 audit_logs

- id
- actor_type
- actor_id
- action
- target_type
- target_id
- detail
- ip
- created_at

## 19. Agent API 草案

### 19.1 心跳

```http
POST /api/agent/heartbeat
```

请求：

```json
{
  "machine_id": "machine_123",
  "agent_version": "0.1.0",
  "hostname": "web-01",
  "ip": "10.0.0.10",
  "os": "linux",
  "arch": "amd64"
}
```

认证：

- Header 使用 `Authorization: Bearer <agent-token>`。
- 后端校验 machine_id 和 agent_token_hash。
- 如果 Token 已 revoke，返回 401 或 403。

### 19.2 拉取机器证书配置

```http
GET /api/agent/machines/{machine_id}/certificates
```

响应：

```json
{
  "certificates": [
    {
      "machine_certificate_id": "mc_123",
      "certificate_id": "cert_001",
      "domains": ["example.com", "*.example.com"],
      "expire_at": "2026-08-01T00:00:00Z",
      "fingerprint_sha256": "abc",
      "cert_path": "/etc/nginx/ssl/example.com/fullchain.pem",
      "private_key_path": "/etc/nginx/ssl/example.com/privkey.pem",
      "post_deploy_commands": "nginx -t\nsystemctl reload nginx"
    }
  ]
}
```

### 19.3 下载证书

```http
GET /api/agent/certificates/{certificate_id}/download
```

响应：

```json
{
  "certificate_id": "cert_001",
  "fingerprint_sha256": "abc",
  "fullchain_pem": "-----BEGIN CERTIFICATE-----...",
  "private_key_pem": "-----BEGIN PRIVATE KEY-----..."
}
```

### 19.4 上报部署日志

```http
POST /api/agent/deployment-logs
```

请求：

```json
{
  "machine_certificate_id": "mc_123",
  "certificate_id": "cert_001",
  "status": "success",
  "cert_fingerprint_sha256": "abc",
  "cert_path": "/etc/nginx/ssl/example.com/fullchain.pem",
  "private_key_path": "/etc/nginx/ssl/example.com/privkey.pem",
  "command_outputs": [
    {
      "command": "nginx -t",
      "exit_code": 0,
      "stdout": "syntax is ok",
      "stderr": ""
    },
    {
      "command": "systemctl reload nginx",
      "exit_code": 0,
      "stdout": "",
      "stderr": ""
    }
  ],
  "error_message": "",
  "started_at": "2026-05-13T10:00:00Z",
  "finished_at": "2026-05-13T10:00:02Z"
}
```

## 20. 安全要求

- Web 生产环境必须通过 HTTPS 暴露。
- Agent Token 必须支持 revoke。
- Agent Token 不在数据库中明文保存，只保存哈希。
- 证书私钥需要限制下载权限。
- SQLite3 数据库文件和 `config.json` 必须设置合理文件权限。
- Agent 只接受 Web 后端下发的当前机器证书配置。
- Agent 不提供远程 shell 接口。
- 部署后命令只来自 Web 端配置。
- 命令执行需要超时控制。
- Web 关键操作需要写入审计日志。
- 只读查看模式禁止调用任何写接口。

## 21. MVP 范围

### 21.1 第一阶段

- Golang Web 后端。
- Golang Agent。
- SQLite3。
- `config.json`。
- 初始化流程。
- 登录。
- 系统管理员和用户。
- 只读查看密码。
- 机器管理。
- 长效 Agent Token。
- Agent 安装命令。
- Agent 心跳和在线状态。
- 手动上传证书。
- 证书解析。
- 证书列表。
- 机器页面添加证书部署。
- Agent 同步证书。
- 写入证书路径和私钥路径。
- 执行部署后命令。
- 部署日志最近 30 条。
- 证书 15 天过期提醒。
- Lark 或 Telegram 至少一种告警。

### 21.2 第二阶段

- Certbot 签发。
- Certbot + Cloudflare DNS 自动认证。
- 系统调度器自动续签。
- 手动 DNS 验证签发。
- Cloudflare A、AAAA、CNAME 同步。
- 域名 SSL 状态监控。
- Lark 和 Telegram 双渠道完整支持。

### 21.3 第三阶段

- Agent 自升级。
- 更细的审计日志。
- 更多 DNS Provider。
- 更多证书输出格式。
- 证书私钥加密存储。
- 高可用部署方案。

## 22. 验收标准

- 首次启动 Web 后，能进入初始化页面创建系统管理员。
- 初始化完成后，能进入系统参数配置页面。
- 系统参数能写入 `config.json`。
- 管理员能添加用户，普通用户不能添加用户。
- 配置只读密码后，输入 `viewpassword` 可以进入只读页面。
- 添加机器后，Web 端能生成包含 Web 地址、machine_id、Agent Token 的安装命令。
- Linux 机器执行安装命令后，Web 端能显示机器在线。
- revoke Agent Token 后，Agent 不能继续拉取配置或下载证书。
- 上传证书后，系统能展示证书 ID、覆盖域名、来源、过期时间、是否自动续签。
- 再次上传更新证书时，系统直接覆盖原证书内容，不产生版本记录。
- 在机器详情中添加证书部署配置后，Agent 能自动写入证书路径和私钥路径。
- Agent 能执行部署后命令，并上报 stdout、stderr、exit code。
- Web 端能在机器 -> 证书 -> 部署信息 -> 日志下查看最近 30 条日志。
- 自动续签开启的证书，在过期前 15 天由系统调度器触发续签。
- Certbot + Cloudflare DNS 自动认证证书的续签由系统触发，不依赖 Certbot 自带自动续签。
- Cloudflare 同步能拉取 A、AAAA、CNAME 记录。
- 域名监控支持自定义检测端口，未配置时默认使用 443。
- 域名监控能展示公网 SSL 状态、过期时间、剩余天数和异常信息。
