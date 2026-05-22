# 需求文档

## 简介

SSL 证书管理系统是一个轻量、可自部署的 Web 系统，用于集中管理 SSL 证书、机器、部署路径、域名监控和告警。系统由 Go Web 后端和 Go Agent 两部分组成，运行于 Linux 环境，使用 SQLite3 存储数据，通过 config.json 管理全局配置。

## 术语表

- **Web_Backend**: Go 语言开发的 Web 管理后端服务，提供 API、调度器、证书管理、告警发送等功能
- **Agent**: 部署在 Linux 机器上的 Go 语言客户端程序，负责心跳、证书同步、文件部署和命令执行
- **Certificate**: SSL/TLS 证书对象，包含 PEM 格式的证书内容、私钥、证书链
- **Machine**: 系统中注册的 Linux 服务器实体
- **Machine_Certificate**: 机器与证书的部署配置关联，定义证书在特定机器上的部署路径和部署后命令
- **Deployment_Log**: Agent 执行证书部署后生成的日志记录
- **Domain_Monitor**: 域名 SSL 状态监控对象，用于探测公网域名的 TLS 状态
- **Scheduler**: Web_Backend 内置的任务调度器，负责触发自动续签和定期监控
- **Certbot**: Let's Encrypt 官方客户端工具，用于签发和续签免费 SSL 证书
- **Cloudflare_DNS**: Cloudflare 提供的 DNS 服务和 API，用于 DNS-01 验证和域名记录同步
- **Agent_Token**: 每台机器唯一的长效认证令牌，用于 Agent 与 Web_Backend 之间的身份验证
- **Fullchain_PEM**: 包含服务器证书和中间证书链的完整 PEM 文件
- **SNI**: Server Name Indication，TLS 扩展协议，允许在同一 IP 上托管多个 SSL 证书
- **Fingerprint_SHA256**: 证书的 SHA256 指纹哈希值，用于唯一标识证书内容
- **Post_Deploy_Command**: 证书部署完成后执行的 shell 命令，如 nginx reload

## 需求

### 需求 1：系统初始化

**用户故事：** 作为系统管理员，我希望首次部署后能通过初始化流程创建管理员账户并配置系统参数，以便系统可以正常运行。

#### 验收标准

1. WHEN Web_Backend 启动且 SQLite3 数据库不存在或数据库中不存在管理员用户 THEN THE Web_Backend SHALL 将所有 HTTP 请求重定向到 `/init` 初始化页面
2. WHEN 管理员在初始化页面提交用户名和密码 THEN THE Web_Backend SHALL 创建系统管理员用户并初始化所有数据库表
3. WHEN 初始化完成后用户访问 `/init` THEN THE Web_Backend SHALL 返回 403 禁止访问
4. WHEN 管理员完成用户创建 THEN THE Web_Backend SHALL 跳转到系统参数配置页面，允许配置 Web 外部访问地址、Agent 参数、告警参数和 Certbot 参数
5. WHEN 系统参数配置保存成功 THEN THE Web_Backend SHALL 将配置写入 config.json 并跳转到仪表盘页面
6. THE Web_Backend SHALL 使用固定的数据目录布局：`./data/config.json`（全局配置）、`./data/data.sqlite3`（SQLite3 数据库）、`./data/certificates/<certificate_id>/`（证书文件目录）；启动时如果 `./data` 目录不存在则自动创建

### 需求 2：用户认证与权限

**用户故事：** 作为系统管理员，我希望管理用户账户和访问权限，以便控制系统的使用范围和安全性。

#### 验收标准

1. WHEN 用户提交正确的用户名和密码 THEN THE Web_Backend SHALL 创建会话并返回认证令牌
2. WHEN 用户提交错误的用户名或密码 THEN THE Web_Backend SHALL 返回 401 认证失败且不泄露具体错误原因
3. WHEN 系统管理员创建新用户 THEN THE Web_Backend SHALL 保存用户信息并设置角色为 admin 或 user
4. WHEN 普通用户尝试访问用户管理接口 THEN THE Web_Backend SHALL 返回 403 禁止访问
5. WHEN config.json 中 readonly.enabled 为 true 且用户提交正确的只读密码 THEN THE Web_Backend SHALL 创建只读会话；只读密码在 config.json 中以明文形式存储（view_password 字段），通过文件权限保护
6. WHEN 只读会话用户尝试执行任何写操作 THEN THE Web_Backend SHALL 返回 403 禁止访问；只读模式使用接口白名单而非简单允许所有 GET 请求
7. THE Web_Backend SHALL 禁止只读会话访问以下接口：证书私钥下载、Agent Token 明文查看或重新生成、手动续签、手动部署、手动域名探测（会写入结果）、Cloudflare 同步、告警测试发送
8. THE Web_Backend SHALL 在所有返回配置的接口中对 Token、Webhook URL、密码字段进行脱敏处理
9. WHEN 系统管理员禁用某用户账户 THEN THE Web_Backend SHALL 立即使该用户的所有活跃会话失效

### 需求 3：机器管理

**用户故事：** 作为用户，我希望在 Web 端添加和管理 Linux 机器，以便后续在这些机器上部署证书。

#### 验收标准

1. WHEN 用户提交机器名称和 IP 地址 THEN THE Web_Backend SHALL 创建机器记录并生成唯一的 Agent_Token
2. WHEN 机器创建成功 THEN THE Web_Backend SHALL 生成包含 Web 外部访问地址、machine_id 和 Agent_Token 的安装命令
3. WHEN 用户请求 revoke 某台机器的 Agent_Token THEN THE Web_Backend SHALL 将该 Token 标记为已吊销并记录吊销时间
4. WHILE Agent_Token 处于已吊销状态 THE Web_Backend SHALL 拒绝该 Agent 的所有心跳、配置拉取和证书下载请求并返回 401
5. WHEN 用户请求重新生成 Agent_Token THEN THE Web_Backend SHALL 生成新的 Token 并更新安装命令

### 需求 4：Agent 心跳与状态

**用户故事：** 作为用户，我希望实时了解各机器上 Agent 的运行状态，以便及时发现离线或异常的机器。

#### 验收标准

1. WHEN Agent 发送心跳请求且 Agent_Token 有效 THEN THE Web_Backend SHALL 更新机器的最近心跳时间、Agent 版本、主机名、IP、OS 和 Arch 信息
2. WHEN 机器的最近心跳时间距当前时间超过 config.json 中配置的 heartbeat_timeout_seconds THEN THE Web_Backend SHALL 将机器状态标记为 offline
3. WHEN 机器从未收到过心跳 THEN THE Web_Backend SHALL 将机器状态显示为 pending
4. WHEN 离线机器重新发送心跳成功 THEN THE Web_Backend SHALL 将机器状态恢复为 online
5. WHEN Agent_Token 已被 revoke 的 Agent 发送心跳 THEN THE Web_Backend SHALL 返回 401 并触发告警

### 需求 5：证书管理

**用户故事：** 作为用户，我希望集中管理所有 SSL 证书，包括上传、签发和查看证书详情，以便统一掌握证书状态。

#### 验收标准

1. WHEN 用户上传证书 PEM 和私钥 PEM THEN THE Web_Backend SHALL 验证证书与私钥是否匹配、验证证书链是否完整，匹配则将证书文件保存到 `./data/certificates/<certificate_id>/` 目录并在 SQLite3 中保存元数据（覆盖域名、过期时间、颁发者、Fingerprint_SHA256、证书链完整性状态）
2. IF 上传的证书与私钥不匹配 THEN THE Web_Backend SHALL 拒绝保存并返回错误信息
3. WHEN 用户对已存在的证书重新上传新的 PEM 内容 THEN THE Web_Backend SHALL 直接覆盖原证书文件、重新解析元数据并将所有关联 Machine_Certificate 的部署状态标记为待同步并递增 config_revision
4. WHEN 用户请求使用 Certbot + Cloudflare_DNS 签发证书 THEN THE Web_Backend SHALL 调用 Certbot 执行 DNS-01 验证并在签发成功后将证书文件保存到 `./data/certificates/<certificate_id>/` 目录
5. WHEN 用户请求使用 Certbot + 手动 DNS 验证签发证书 THEN THE Web_Backend SHALL 生成 DNS TXT 记录要求并等待用户确认后完成签发
6. THE Web_Backend SHALL 在证书列表页面展示每张证书的 ID、覆盖域名、来源、过期时间、剩余有效天数、是否自动续签和关联机器数量
7. IF 证书链不完整 THEN THE Web_Backend SHALL 允许保存但在证书详情中标记 chain_valid 为 false 并显示警告
8. THE Web_Backend SHALL 将证书文件按以下结构存储：`./data/certificates/<certificate_id>/cert.pem`、`chain.pem`、`fullchain.pem`、`privkey.pem`；SQLite3 只保存元数据和文件路径，不保存 PEM 内容

### 需求 6：证书自动续签

**用户故事：** 作为用户，我希望系统能自动续签即将过期的证书，以避免证书过期导致服务中断。

#### 验收标准

1. WHEN Certificate 开启自动续签且距离过期时间小于等于 config.json 中配置的 default_before_days THEN THE Scheduler SHALL 触发续签任务
2. WHEN 续签任务针对来源为 certbot_cloudflare_dns 的 Certificate THEN THE Web_Backend SHALL 读取 Cloudflare API 配置并调用 Certbot 使用 DNS-01 验证重新签发
3. WHEN 续签成功 THEN THE Web_Backend SHALL 覆盖原证书内容、更新元数据并将所有关联 Machine_Certificate 标记为待同步
4. IF 续签失败 THEN THE Web_Backend SHALL 记录错误信息、发送告警通知并按配置的重试策略重试
5. WHILE Certificate 来源为 certbot_manual_dns 且自动续签开启 THE Scheduler SHALL 在过期前发送提醒通知而不自动执行续签
6. THE Web_Backend SHALL NOT 启用或依赖 Certbot 自带的 systemd timer、cron 或 `certbot renew` 后台自动续签机制；每次续签必须由 Scheduler 判断证书过期时间后主动调用 Certbot certonly 命令签发新证书
7. WHEN Certbot 签发成功 THEN THE Web_Backend SHALL 从 Certbot 输出目录读取新证书文件并覆盖 `./data/certificates/<certificate_id>/` 下的证书文件（cert.pem、chain.pem、fullchain.pem、privkey.pem），同时更新 SQLite3 中的元数据（指纹、过期时间、颁发者等）

### 需求 7：证书部署配置

**用户故事：** 作为用户，我希望为每台机器配置证书的部署路径和部署后命令，以便 Agent 能自动完成证书部署。

#### 验收标准

1. WHEN 用户在机器详情页面添加证书部署配置 THEN THE Web_Backend SHALL 保存 certificate_id、cert_path、private_key_path 和 post_deploy_commands，并递增该 Machine_Certificate 的 config_revision
2. WHEN 用户配置的 cert_path 或 private_key_path 为空 THEN THE Web_Backend SHALL 拒绝保存并提示路径不能为空
3. THE Web_Backend SHALL 在证书选择下拉框中展示证书 ID、覆盖域名、过期时间和是否自动续签
4. WHEN 用户编辑已有的部署配置（包括路径变化或命令变化）THEN THE Web_Backend SHALL 更新配置、递增 config_revision 并将部署状态标记为待同步
5. WHEN 用户在机器证书配置上点击手动部署 THEN THE Web_Backend SHALL 将该 Machine_Certificate 标记为待同步并递增 config_revision，Agent 下次轮询时必须执行部署，即使证书指纹未变化

### 需求 8：Agent 证书同步与部署

**用户故事：** 作为用户，我希望 Agent 能自动同步和部署证书到目标机器，以减少人工操作和部署错误。

#### 验收标准

1. WHEN Agent 拉取机器证书配置后发现以下任一条件满足时 SHALL 触发部署：本地证书文件不存在、本地证书指纹与 Web_Backend 不一致、config_revision 与本地保存的 last_synced_revision 不同、Web_Backend 标记部署状态为 pending
2. WHEN Agent 下载证书成功 THEN THE Agent SHALL 校验证书与私钥匹配后，将 Fullchain_PEM 写入临时文件、将私钥写入临时文件，两个临时文件都准备成功后再依次原子替换到 cert_path 和 private_key_path
3. WHEN 目标文件所在目录不存在 THEN THE Agent SHALL 自动创建目录并设置目录权限为 0755
4. WHEN 证书文件写入成功 THEN THE Agent SHALL 设置证书文件权限为 0644、私钥文件权限为 0600
5. WHEN 文件部署完成 THEN THE Agent SHALL 按行顺序执行 post_deploy_commands 中的每条命令，每条命令超时时间为 60 秒
6. IF 任意 Post_Deploy_Command 执行失败（exit code 非 0）THEN THE Agent SHALL 标记本次部署为失败并停止执行后续命令
7. WHEN 部署流程完成 THEN THE Agent SHALL 向 Web_Backend 上报包含部署状态、证书指纹、每条命令的 stdout、stderr 和 exit code 的 Deployment_Log，并更新本地 last_synced_revision
8. IF 证书文件写入过程中发生错误（包括任一文件替换失败）THEN THE Agent SHALL 保留原有证书文件不变、不执行部署后命令并上报错误信息
9. IF 下载的证书与私钥不匹配 THEN THE Agent SHALL 拒绝写入文件并上报错误信息

### 需求 9：部署日志

**用户故事：** 作为用户，我希望查看每台机器上证书部署的历史日志，以便排查部署问题。

#### 验收标准

1. WHEN Agent 上报 Deployment_Log THEN THE Web_Backend SHALL 保存日志记录，包含部署时间、证书 ID、指纹、部署状态、路径、命令输出和总耗时
2. THE Web_Backend SHALL 为每个 Machine_Certificate 只保留最近 30 条 Deployment_Log，超出时自动删除最旧记录
3. WHEN 用户在机器详情页面查看部署日志 THEN THE Web_Backend SHALL 按时间倒序展示日志列表
4. THE Web_Backend SHALL 在每条 Deployment_Log 中展示部署状态为 success、failed 或 skipped

### 需求 10：域名 SSL 状态监控

**用户故事：** 作为用户，我希望监控域名的公网 SSL 证书状态，以便及时发现证书异常或不一致。

#### 验收标准

1. WHEN 用户手动添加域名监控对象 THEN THE Web_Backend SHALL 创建 Domain_Monitor 记录并设置默认检测端口为 config.json 中的 domain_monitor.default_port
2. WHEN Scheduler 触发域名监控任务 THEN THE Web_Backend SHALL 使用 SNI 对目标域名和配置端口执行 TLS 握手探测
3. WHEN TLS 探测完成 THEN THE Web_Backend SHALL 记录 DNS 解析结果、TLS 握手状态、线上证书过期时间、剩余有效天数、域名匹配状态、证书链完整性和颁发者信息
4. WHEN 线上证书指纹与系统内关联证书指纹不一致 THEN THE Web_Backend SHALL 标记该域名为异常状态
5. WHEN 用户手动触发域名探测 THEN THE Web_Backend SHALL 立即执行探测并返回结果
6. WHEN 域名 DNS 无法解析或 TLS 握手失败 THEN THE Web_Backend SHALL 记录错误信息并触发告警

### 需求 11：第三方 DNS 上游与域名同步

**用户故事：** 作为用户，我希望系统能从第三方 DNS 上游自动同步域名记录，以便自动创建域名监控对象并支持 DNS 验证签发证书。

#### 验收标准

1. WHEN 用户添加第三方 DNS 上游配置（当前仅支持 type=cloudflare）THEN THE Web_Backend SHALL 验证 API Token 有效性并保存配置到 SQLite3 的 thirdpart_dns 表
2. WHEN 用户触发 DNS 上游同步 THEN THE Web_Backend SHALL 根据上游 type 调用对应 API 拉取域名记录；如果 main_domains 为空则全量抓取，如果 main_domains 非空则只抓取指定 main_domain 范围内的 A、AAAA 和 CNAME 记录
3. WHEN 同步完成 THEN THE Web_Backend SHALL 将新发现的域名记录写入本地域名列表（关联 thirdpart_dns_id）并可选自动创建 Domain_Monitor 对象
4. IF DNS 上游 API 调用失败 THEN THE Web_Backend SHALL 记录错误信息并发送告警通知
5. THE Web_Backend SHALL 在 thirdpart_dns_sync_logs 表中记录每次同步的时间、上游配置 ID、同步记录数量、状态和错误信息
6. WHEN 证书来源为 certbot_cloudflare_dns 或其他需要 DNS 验证的方式 THEN THE Web_Backend SHALL 在证书记录中通过 thirdpart_dns_id 关联对应的 DNS 上游配置，以便续签时读取正确的 API 配置
7. THE Web_Backend SHALL 支持为每个 thirdpart_dns 配置多个 main_domain；main_domains 为空数组时表示不过滤、全量抓取该上游下所有域名记录

### 需求 12：告警通知

**用户故事：** 作为用户，我希望在证书过期、续签失败、Agent 离线等异常事件发生时收到告警通知，以便及时处理问题。

#### 验收标准

1. WHEN 证书距离过期时间小于等于 15 天 THEN THE Web_Backend SHALL 通过已启用的告警渠道发送过期预警通知
2. WHEN 以下事件发生时 THE Web_Backend SHALL 发送告警通知：证书已过期、自动续签失败、Agent 离线、Agent_Token 被 revoke 后仍有请求、证书部署失败、域名 SSL 探测失败、线上证书与系统证书不一致、Cloudflare 同步失败
3. WHEN 用户配置 Lark webhook 地址 THEN THE Web_Backend SHALL 支持通过 Lark 发送告警消息
4. WHEN 用户配置 Telegram bot token 和 chat_id THEN THE Web_Backend SHALL 支持通过 Telegram 发送告警消息
5. WHEN 用户触发告警测试发送 THEN THE Web_Backend SHALL 向指定渠道发送测试消息并返回发送结果
6. WHILE 同一告警事件未恢复 THE Web_Backend SHALL 抑制重复告警，不重复发送相同内容
7. WHEN 告警事件恢复正常 THEN THE Web_Backend SHALL 发送恢复通知

### 需求 13：审计日志

**用户故事：** 作为系统管理员，我希望记录系统中的关键操作，以便追溯操作历史和排查安全问题。

#### 验收标准

1. WHEN 用户执行创建、更新、删除证书、机器、用户或部署配置等写操作 THEN THE Web_Backend SHALL 记录审计日志，包含操作者类型、操作者 ID、操作类型、目标类型、目标 ID、操作详情和来源 IP
2. WHEN Agent 执行部署操作 THEN THE Web_Backend SHALL 记录审计日志，actor_type 为 agent
3. THE Web_Backend SHALL 在审计日志页面按时间倒序展示所有审计记录

### 需求 14：Agent 安装与配置

**用户故事：** 作为用户，我希望通过简单的安装命令在 Linux 机器上部署 Agent，以便快速接入证书管理系统。

#### 验收标准

1. WHEN 用户在机器详情页面点击获取安装命令 THEN THE Web_Backend SHALL 生成包含 curl 下载安装脚本、Web 外部访问地址、machine_id 和 Agent_Token 的一键安装命令
2. WHEN 安装脚本在 Linux systemd 环境中执行 THEN THE Agent SHALL 下载 Agent 二进制文件、创建配置目录 /etc/ssl-manager-agent/、写入配置文件、创建 systemd 服务并启动
3. WHEN 安装脚本检测到非 systemd 环境 THEN THE Agent SHALL 输出明确错误信息，提示当前仅支持 systemd Linux 环境，并给出手动运行 Agent 二进制的方式
4. WHEN Agent 启动成功 THEN THE Agent SHALL 立即发送首次心跳到 Web_Backend
5. THE Agent SHALL 在配置文件中保存 server_url、machine_id、agent_token 和 poll_interval_seconds
6. THE Agent SHALL 在本地状态文件（/etc/ssl-manager-agent/state.json）中持久化每个 machine_certificate_id 的 last_synced_revision、last_synced_fingerprint、last_deploy_status 和 last_deploy_at，Agent 重启后从该文件恢复状态

### 需求 15：仪表盘

**用户故事：** 作为用户，我希望在仪表盘页面快速了解系统整体状态，以便发现需要关注的问题。

#### 验收标准

1. THE Web_Backend SHALL 在仪表盘页面展示证书总数、15 天内过期证书数量、已过期证书数量、在线机器数量、离线机器数量、最近 24 小时内部署失败数量、最近 24 小时内续签失败数量和域名 SSL 异常数量
2. WHEN 仪表盘数据中存在异常指标 THEN THE Web_Backend SHALL 以醒目样式高亮显示异常数据

### 需求 16：安全要求

**用户故事：** 作为系统管理员，我希望系统满足基本安全要求，以保护证书私钥和系统数据安全。

#### 验收标准

1. THE Web_Backend SHALL 只保存 Agent_Token 的哈希值，不明文存储 Token
2. THE Web_Backend SHALL 对证书下载接口进行权限验证：下载接口基于 machine_certificate_id 访问，后端必须校验 Agent Token 对应的 machine_id 与该 machine_certificate 的 machine_id 一致，且关联证书存在
3. THE Agent SHALL 不提供任何远程 shell 接口
4. THE Agent SHALL 只执行来自 Web_Backend 配置的 Post_Deploy_Command，不接受外部命令输入
5. WHEN Post_Deploy_Command 执行时间超过 60 秒 THEN THE Agent SHALL 终止该命令并标记为超时失败
6. THE Web_Backend SHALL 对所有写操作接口验证用户认证状态和权限
7. THE Web_Backend SHALL 将用户密码使用 bcrypt 算法哈希后存储
8. THE Web_Backend SHALL 在启动时检查 config.json 文件权限，如果权限过宽（非 0600）则输出安全警告日志；通知渠道配置（Lark webhook、Telegram bot token）存储在 SQLite3 中，不做加密，通过数据库文件权限保护
9. THE Web_Backend SHALL 区分内部数据模型和 API 响应 DTO，普通 Web API 的证书响应不包含 private_key_pem 字段，私钥仅在受控的 Agent 下载接口中返回
10. THE Web_Backend SHALL 在审计日志 detail 字段中禁止记录 Token、私钥、Webhook URL 等敏感信息的明文
