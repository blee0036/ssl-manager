# SSL Manager

SSL Manager 是一个自托管的 SSL/TLS 证书全生命周期管理平台，用于集中完成证书签发、续期、部署、域名线上状态监控、DNS 同步、告警通知和审计追踪。

系统由 Web Backend 和 Agent 组成：

- Web Backend 负责 Web 管理界面、REST API、SQLite 数据存储、Certbot 调用、Cloudflare DNS 集成、调度器、告警和审计。
- Agent 部署在目标 Linux 或 macOS 机器上，负责心跳、拉取证书部署配置、下载证书、写入目标路径、执行部署后命令并回报部署日志。支持自动更新。

## 适用场景

- 多台 Linux 或 macOS 机器需要复用或分发同一批证书。
- 证书需要通过 Let's Encrypt + DNS-01 自动签发和自动续期。
- 希望知道线上域名实际使用的证书是否与系统内证书一致。
- 希望在证书过期、续签失败、Agent 离线、部署失败、DNS 同步失败时收到通知。
- 希望给运维大屏或外部人员只读访问能力，同时禁止任何敏感操作。

## 快速开始

### 本地构建运行

```bash
make build
./bin/ssl-manager-web
```

默认监听 `:8080`。浏览器访问：

```text
http://localhost:8080
```

首次访问会进入初始化页面。

### Docker 运行（推荐）

推荐直接使用 GHCR 预编译镜像：

```bash
docker run -d \
  --name ssl-manager \
  --restart unless-stopped \
  -p 8080:8080 \
  -v "$(pwd)/ssl-manager-data:/app/data" \
  ghcr.io/blee0036/ssl-manager:latest
```

镜像支持 `linux/amd64` 和 `linux/arm64`，Docker 会按宿主机架构自动拉取。`latest` 标签始终指向最新的 tag release（如 `v0.1.0`），确保 Agent 自动更新版本号递增。镜像内还包含 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64` 四个 Agent 二进制，安装脚本会按目标机器系统和架构下载对应版本。

不要把空 volume 挂载到 `/app/bin`，否则会覆盖镜像内置的 Agent 二进制。持久化只需要挂载 `/app/data`。

### 本地 Docker 构建

本机架构镜像：

```bash
docker build --build-arg VERSION=$(cat VERSION) -t ssl-manager .

docker run -d \
  --name ssl-manager \
  -p 8080:8080 \
  -v ssl-manager-data:/app/data \
  ssl-manager
```

多架构镜像：

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=$(cat VERSION) \
  -t ssl-manager:latest \
  .
```

如果要推送到镜像仓库，给镜像名加上仓库前缀并追加 `--push`。

### Agent 安装

在 Web 界面创建机器后，复制生成的安装命令到目标机器（Linux 或 macOS）执行。命令形式如下：

```bash
curl -fsSL http://<server>/api/agent/install.sh | bash -s -- \
  --server-url http://<server> \
  --machine-id <machine_id> \
  --agent-token <token>
```

安装脚本支持 Linux systemd 和 macOS launchd 环境，会：

- 自动识别 `linux` 或 `darwin`。
- 自动识别 `amd64` 或 `arm64`。
- 下载 `/api/agent/binary?os=<os>&arch=<arch>`。
- 写入 `/usr/local/bin/ssl-manager-agent`。
- Linux：创建 `/etc/ssl-manager-agent/config.yaml`。
- macOS：创建 `/Library/Application Support/ssl-manager-agent/config.yaml`。
- Linux 创建并启动 `ssl-manager-agent.service`（systemd）。
- macOS 创建并启动 `com.ssl-manager.agent`（launchd）。
- 默认启用自动更新（`auto_update: true`）。

安装完成后可使用以下 CLI 子命令管理 Agent：

```bash
ssl-manager-agent version       # 显示版本信息
ssl-manager-agent update        # 手动检查并更新
ssl-manager-agent auto-update   # 查看/设置自动更新 (enable/disable)
ssl-manager-agent restart       # 重启服务
ssl-manager-agent logs          # 查看日志 (--follow, --lines N)
ssl-manager-agent config        # 查看/修改配置 (--server-url, --token, --interactive)
ssl-manager-agent uninstall     # 完整卸载 (--yes 跳过确认)
```

Linux 非 systemd 环境会输出手动运行说明。

### 构建产物

```bash
# 构建 Web、本机 Agent、Linux/macOS amd64/arm64 Agent
make build

# 构建 Linux amd64/arm64 Web 和 Agent
make build-linux

# 构建 macOS amd64/arm64 Web 和 Agent
make build-darwin

# 发布构建：Linux + macOS
make release

# 运行测试
make test
```

macOS 不需要额外兼容层，按 Go 原生交叉编译生成 `darwin/amd64` 和 `darwin/arm64` 二进制即可。macOS 安装脚本使用 launchd 管理 Agent。

如果手动部署 Web Backend，请确保 `bin/ssl-manager-agent-linux-amd64`、`bin/ssl-manager-agent-linux-arm64`、`bin/ssl-manager-agent-darwin-amd64`、`bin/ssl-manager-agent-darwin-arm64` 位于 Web 进程工作目录的 `./bin` 下，否则对应平台的 Agent 安装脚本下载二进制时会返回 404。

## 功能概览

### 初始化与系统配置

首次启动时，如果数据库中还没有管理员用户，系统会进入初始化流程：

1. 创建管理员账户。
2. 保存全局运行参数到 `./data/config.json`。
3. 创建和迁移 SQLite 表结构。

初始化后，配置可在系统设置页面继续调整。配置保存后会更新运行时配置，调度器和接口会读取新的配置值。

运行数据默认保存在 `./data`：

- `./data/config.json`：全局配置，包含只读密码等敏感参数。
- `./data/data.sqlite3`：业务数据库，包含用户、机器、证书元数据、DNS 上游、告警、审计等数据。
- `./data/jwt_secret`：JWT 签名密钥，持久化后 Web 重启不会使所有会话默认失效。
- `./data/certificates/<certificate_id>/`：证书文件目录，保存 `cert.pem`、`chain.pem`、`fullchain.pem`、`privkey.pem`。

### 用户、权限与只读模式

系统支持三类访问身份：

- `admin`：可管理用户、系统配置、机器、证书、DNS、告警等所有资源。
- `user`：可使用主要业务功能，但不能管理用户和部分管理员能力。
- `readonly`：通过只读密码登录，仅允许访问白名单内的查看接口。

只读模式不是简单放行所有 `GET` 请求。以下能力会被显式阻止：

- 查看或重新生成 Agent Token。
- 下载证书私钥。
- 手动续签。
- 手动部署。
- 手动域名探测。
- Cloudflare DNS 同步。
- 告警测试发送。
- 任意 `POST`、`PUT`、`DELETE` 写操作。

所有返回配置的接口会对密码、Token、Webhook URL 等字段做脱敏。用户密码使用 bcrypt 存储，Agent Token 只保存哈希值。

### 机器与 Agent

在 Web 端创建机器后，系统会生成：

- `machine_id`
- 一次性展示的 `agent_token`
- 一键安装命令

Agent 安装后会：

1. 立即发送首次心跳。
2. 定期上报 hostname、IP、OS、Arch、Agent 版本。
3. 定期拉取该机器的证书部署配置。
4. 在需要同步时下载证书并部署。
5. 心跳响应中包含最新版本信息时，自动下载、校验、替换并重启（可通过 `auto-update disable` 关闭）。

机器状态由心跳驱动：

- 从未心跳：`pending`
- 最近心跳未超时：`online`
- 超过 `agent.heartbeat_timeout_seconds` 未心跳：`offline`
- Token 被吊销后继续请求：接口返回 401，并触发告警

### 证书管理

证书支持三种来源：

- `upload`：上传已有 PEM 证书和私钥。
- `certbot_cloudflare_dns`：使用 Certbot + Cloudflare DNS-01 自动签发。
- `certbot_manual_dns`：使用 Certbot 手动 DNS 验证签发。

上传或签发证书时，系统会解析并保存：

- 覆盖域名。
- 到期时间和剩余天数。
- 颁发者。
- SHA256 指纹。
- 证书链完整性。
- 是否自动续期。
- 关联的 DNS 上游配置。

普通 Web API 不返回私钥内容。私钥只会在 Agent 证书下载接口中返回，并且后端会校验 Agent Token 对应的机器是否拥有该 `machine_certificate_id`。

### 自动续期

调度器会按证书到期时间判断是否需要续期：

- 证书开启 `auto_renew`。
- 证书距离过期天数小于等于 `alert.default_before_days`。
- 证书来源为 `certbot_cloudflare_dns`。

满足条件时，Web Backend 会读取该证书关联的 Cloudflare DNS 上游 Token，调用 Certbot 执行 DNS-01 验证并生成新证书。续签成功后会：

- 覆盖原证书文件。
- 更新证书元数据和指纹。
- 将所有关联的机器证书部署配置标记为待同步。
- 解除相关续签失败告警。

如果证书来源是 `certbot_manual_dns`，系统不会自动完成续期，只会在到期前发送提醒。

自动续签失败后，系统会保留证书及失败状态，并在距离失败时间满 24 小时后再次尝试。首次签发或上传在本地保存失败时，会清理未完成的证书记录。

系统不依赖 Certbot 自带的 systemd timer、cron 或 `certbot renew`。续签由 SSL Manager 的调度器根据数据库中的证书状态主动触发。

### 证书部署

证书部署由“机器证书配置”控制。每条配置包含：

- 目标机器。
- 证书 ID。
- `cert_path`：目标机器上的 fullchain 写入路径。
- `private_key_path`：目标机器上的私钥写入路径。
- `post_deploy_commands`：部署完成后按行执行的命令。

Agent 每次轮询都会校验目标证书文件的实际指纹、配置版本和上次部署状态。目标证书与源证书不一致、目标文件被外部替换，或上次部署失败时，Agent 会在后续轮询中自动重试；每次失败都会记录部署日志并触发部署失败告警，成功后自动恢复告警。
- `config_revision`：配置版本，用于触发 Agent 重新部署。

以下情况会触发 Agent 部署：

- 本地证书文件不存在。
- 本地证书指纹与 Web Backend 不一致。
- 本地记录的 `last_synced_revision` 与 Web Backend 的 `config_revision` 不一致。
- Web Backend 将部署状态标记为 `pending`。
- 用户点击手动部署。

部署过程遵循安全写入策略：

- 下载证书后先校验证书和私钥是否匹配。
- 先写入临时文件，再原子替换目标文件。
- 目标目录不存在时自动创建，目录权限为 `0755`。
- 证书文件权限为 `0644`，私钥文件权限为 `0600`。
- 文件写入失败时保留原文件，不执行部署后命令。
- 部署后命令按行顺序执行，每条命令超时 60 秒；任意命令失败后停止执行后续命令。

Agent 会把部署状态、证书指纹、路径、命令 stdout/stderr/exit code 和错误信息回报给 Web Backend。每个机器证书配置只保留最近 30 条部署日志。

### 域名 TLS 监控

域名监控用于检查公网线上证书是否正常。系统会使用 SNI 对域名和端口执行 TLS 探测，并记录：

- DNS 解析结果。
- TLS 握手是否成功。
- 线上证书指纹。
- 线上证书到期时间和剩余天数。
- 域名是否匹配证书 SAN/CN。
- 证书链是否完整。
- 颁发者。
- 错误信息。

域名可以手动添加，也可以由 Cloudflare DNS 同步自动创建。域名可关联系统内证书、机器或机器证书配置；当线上证书指纹与关联证书不一致时会被标记为异常。

### 第三方 DNS 上游

当前支持 Cloudflare。DNS 上游配置用于两个地方：

- Certbot Cloudflare DNS-01 签发和续期。
- 从 Cloudflare 拉取 A、AAAA、CNAME 记录并创建本地域名监控对象。

`main_domains` 控制同步范围：

- 为空数组：同步该 Cloudflare Token 可见的所有 zone 和记录。
- 非空：只同步指定主域名范围内的 zone 和 A/AAAA/CNAME 记录。

每次同步都会写入同步日志，记录同步状态、记录数量和错误信息。同步失败会触发告警，成功后会解除对应失败告警。

### 告警通知

系统支持 Lark 和 Telegram 通知渠道。会触发告警的典型事件包括：

- 证书即将过期。
- 证书已过期。
- 自动续签失败。
- Agent 离线。
- 已吊销 Agent Token 继续请求。
- 证书部署失败。
- 域名 TLS 探测失败。
- 线上证书与系统证书不一致。
- Cloudflare DNS 同步失败。

同一目标的同类活跃告警会被抑制，避免重复刷屏。异常恢复后，系统会将告警标记为已恢复，并发送恢复通知。

### 审计日志

系统会记录写操作审计日志，包括：

- 操作者类型：用户或 Agent。
- 操作者 ID。
- 操作方法和路径。
- 目标类型和目标 ID。
- 来源 IP。
- 响应状态。

审计日志按时间倒序展示。敏感内容会在写入前清理，避免记录私钥、Token、Webhook URL 等明文。

### 仪表盘

仪表盘用于快速判断系统是否健康，包含：

- 证书总数。
- 15 天内过期证书数量。
- 已过期证书数量。
- 在线/离线机器数量。
- 最近 24 小时部署失败数量。
- 最近 24 小时续签失败数量。
- 域名 SSL 异常数量。

存在异常指标时，页面会高亮显示。

## 配置说明与联动关系

配置文件位于 `./data/config.json`，也可以通过系统设置页面修改。

### `server`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `external_url` | `http://localhost:8080` | 生成 Agent 安装命令、安装脚本下载地址、Agent 配置中的 `server_url`。生产环境必须设置为 Agent 能访问到的公网或内网地址。 |
| `listen_addr` | `:8080` | Web Backend 实际监听地址。修改后通常需要重启服务才会改变监听端口。 |

`external_url` 和 `listen_addr` 不是同一个概念：

- `listen_addr` 决定服务绑定在哪个端口。
- `external_url` 决定外部客户端和 Agent 应该访问哪个 URL。

如果服务在反向代理后面运行，`listen_addr` 可能仍是 `:8080`，但 `external_url` 应设置为 `https://ssl.example.com`。

### `agent`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `poll_interval_seconds` | `60` | 写入 Agent 安装脚本生成的配置，决定 Agent 心跳和同步轮询频率。越小部署越及时，但 Web 请求更多。 |
| `heartbeat_timeout_seconds` | `120` | Web Backend 判断机器离线的阈值。超过该时间未收到心跳，机器会被标记为 `offline` 并触发告警。 |

建议保持：

```text
heartbeat_timeout_seconds >= poll_interval_seconds * 2
```

否则网络抖动或一次轮询延迟就可能导致机器被频繁标记为离线。

修改 `poll_interval_seconds` 后，只会影响之后生成的安装脚本和新 Agent 配置。已经安装的 Agent 需要更新配置文件（Linux: `/etc/ssl-manager-agent/config.yaml`，macOS: `/Library/Application Support/ssl-manager-agent/config.yaml`）并重启服务才会使用新值。也可以使用 `ssl-manager-agent config --interactive` 交互式修改。

### `alert`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `default_before_days` | `15` | 证书过期提醒阈值，也是自动续期判断阈值。 |

这个字段同时影响两条链路：

- 告警：证书剩余天数小于等于该值时发送临期告警。
- 自动续期：开启 `auto_renew` 且来源为 `certbot_cloudflare_dns` 的证书，会在该阈值内触发续签。

如果设置得太小，续签失败留给人工处理的时间会变短；如果设置得太大，证书会更早进入续期流程。

### `certbot`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `binary_path` | `certbot` | Web Backend 调用的 Certbot 可执行文件路径。 |
| `data_dir` | 空 | Certbot 的目录配置。为空时使用 `/etc/letsencrypt`；Docker 环境建议显式填写 `/app/data/certbot`。 |
| `email` | 空 | Let's Encrypt 注册和签发使用的邮箱。使用 Certbot 签发时应配置。 |

使用 Cloudflare DNS-01 签发时，运行环境需要满足：

- 已安装 `certbot`。
- 已安装 Certbot 的 Cloudflare DNS 插件，Certbot 命令需要支持 `--dns-cloudflare`。
- Cloudflare DNS 上游中保存了有效 API Token。
- 证书记录关联了对应的 `thirdpart_dns_id`，否则自动续期无法找到 Token。

`certbot.data_dir` 会影响 SSL Manager 从哪里读取 Certbot 生成的证书：

- 空：使用 Certbot 默认目录，读取 `/etc/letsencrypt/live/<domain>/`。
- 非空：读取 `<data_dir>/live/<domain>/`。

通配符证书的目录名使用 Certbot 的证书名规则。例如 `*.example.com` 的证书文件通常位于 `<data_dir>/live/example.com/`，而不是 `<data_dir>/live/*.example.com/`。

如果 Web Backend 使用非 root 用户运行，需要确保该用户对 Certbot 目录和 SSL Manager `./data/certificates` 目录有读写权限。

### `readonly`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `enabled` | `false` | 是否允许使用只读密码登录。 |
| `view_password` | 空 | 只读登录密码。启用只读模式时必填。 |

只读密码明文保存在 `config.json` 中，通过文件权限保护。系统保存配置时会使用 `0600` 权限；启动时如果发现权限过宽，会输出安全警告。

开启只读模式后，普通用户和管理员不受只读白名单限制，只有通过只读登录拿到的会话会被限制。

### `domain_monitor`

| 字段 | 默认值 | 影响 |
|------|--------|------|
| `default_port` | `443` | 新建域名监控对象时的默认探测端口。 |
| `interval_minutes` | `60` | 调度器执行域名 TLS 探测的周期。 |

`default_port` 只影响新建域名的默认值，不会自动改写已有域名监控对象的端口。

`interval_minutes` 越小，线上异常发现越及时，但 DNS 查询和 TLS 握手次数越多。手动探测不受该周期限制，但手动探测会写入结果，因此只读会话不能执行。

## 典型工作流

### 上传已有证书并部署到机器

1. 初始化系统并创建管理员。
2. 创建机器，复制安装命令到目标 Linux systemd 机器执行。
3. 上传证书 PEM、私钥 PEM 和证书链。
4. 在机器详情中添加证书部署配置，填写目标 `cert_path`、`private_key_path` 和部署后命令，例如 `systemctl reload nginx`。
5. Agent 下次轮询后自动下载和部署证书。
6. 在部署日志中确认状态为 `success`。

### 使用 Cloudflare 自动签发和续期

1. 在第三方 DNS 中添加 Cloudflare 上游配置。
2. 配置 `certbot.email`，并确认运行环境已安装 Certbot Cloudflare DNS 插件。
3. 创建证书，选择 Cloudflare DNS-01 签发方式并关联 DNS 上游。
4. 开启 `auto_renew`。
5. 当证书进入 `alert.default_before_days` 阈值内，调度器会自动续签。
6. 续签成功后，关联机器证书配置会进入待同步状态，Agent 自动部署新证书。

### 从 Cloudflare 同步域名并监控线上证书

1. 添加 Cloudflare DNS 上游。
2. 设置 `main_domains` 控制同步范围；留空表示全量同步。
3. 执行 DNS 同步。
4. 系统将发现的 A/AAAA/CNAME 记录写入本地域名列表，并可创建监控对象。
5. 关联系统内证书后，域名监控会比较线上证书指纹和系统证书指纹。

## API 概览

| 模块 | 端点 | 说明 |
|------|------|------|
| 初始化 | `/init/*` | 首次创建管理员和保存系统配置 |
| 认证 | `/api/auth/*` | 管理员/用户登录、只读登录 |
| 用户 | `/api/users/*` | 用户创建、更新、禁用 |
| 机器 | `/api/machines/*` | 机器 CRUD、Token、安装命令 |
| 证书 | `/api/certificates/*` | 上传、签发、续签、删除、详情 |
| 机器证书 | `/api/machines/{id}/certificates/*` | 部署配置、手动部署 |
| 部署日志 | `/api/machines/{id}/deployment-logs` | 按机器或部署配置查看部署历史 |
| 域名监控 | `/api/domains/*` | 域名 CRUD、手动探测、监控结果 |
| DNS 上游 | `/api/thirdpart-dns/*` | Cloudflare 配置、同步和同步日志 |
| 告警 | `/api/alerts/*` | 告警历史、恢复、通知渠道和测试 |
| 审计 | `/api/audit-logs` | 审计日志查询 |
| 仪表盘 | `/api/dashboard` | 系统状态聚合 |
| 系统配置 | `/api/system/config` | 查看和更新全局配置 |
| Agent | `/api/agent/*` | 心跳、拉取配置、下载证书、上报部署日志、安装脚本 |

## 安全注意事项

- 生产环境建议通过 HTTPS 暴露 Web Backend，避免 Agent Token 和登录 Token 在明文网络中传输。
- 保护 `./data` 目录权限；SQLite、证书私钥、配置文件和 JWT secret 都在该目录下。
- `config.json` 应保持 `0600` 权限，尤其是启用只读模式后其中会包含只读密码。
- 通知渠道 Webhook、Telegram Bot Token、Cloudflare API Token 存储在 SQLite 中，不做额外加密，依赖数据库文件权限保护。
- Agent 不提供远程 shell；只会执行 Web 中配置的部署后命令。配置部署命令时应避免拼接不可信输入。
- 吊销或重新生成 Agent Token 后，需要重新安装或更新目标机器上的 Agent 配置。

## License

MIT
