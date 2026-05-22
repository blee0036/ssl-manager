# 需求文档

## 简介

本功能为 SSL Manager Agent 添加完整的命令行管理工具和自动更新能力。安装脚本完成后，用户可通过 `ssl-manager-agent` 命令的子命令管理 Agent 生命周期（卸载、重启、查看日志、手动更新、自动更新开关、交互式配置修改）。Web Backend 启动时扫描各 release 二进制的 MD5 校验和，并通过接口下发版本信息；Agent 在心跳响应中获取最新版本信息后，可自动下载新二进制、校验完整性、替换自身并重启服务，实现无人值守的版本升级。

## 术语表

- **Agent**: 部署在目标机器上的 `ssl-manager-agent` 二进制程序，负责心跳、证书同步和部署
- **CLI**: Agent 二进制提供的命令行接口，包含多个子命令
- **Web_Backend**: SSL Manager 的 Web 服务端，提供 REST API 和管理界面
- **自动更新**: Agent 自动检测新版本并完成下载、校验、替换、重启的无人值守升级机制
- **服务管理器**: 操作系统的服务管理器（Linux 为 systemd，macOS 为 launchd）
- **版本信息**: Web Backend 下发的版本元数据，包含版本号、MD5 校验和、文件大小和下载地址
- **配置文件**: Agent 配置文件，位于 `/etc/ssl-manager-agent/config.yaml`（Linux）或 `/Library/Application Support/ssl-manager-agent/config.yaml`（macOS）
- **二进制路径**: Agent 二进制文件路径，默认为 `/usr/local/bin/ssl-manager-agent`
- **版本号**: 编译时注入的语义化版本字符串（如 `1.2.3`），通过 Go ldflags `-X main.Version=...` 设置

## 支持平台

| OS | 架构 | 服务管理器 | 配置目录 | 日志方式 |
|----|------|-----------|---------|---------|
| linux | amd64 | systemd | /etc/ssl-manager-agent/ | journalctl -u ssl-manager-agent |
| linux | arm64 | systemd | /etc/ssl-manager-agent/ | journalctl -u ssl-manager-agent |
| darwin | amd64 | launchd | /Library/Application Support/ssl-manager-agent/ | /var/log/ssl-manager-agent.log + /var/log/ssl-manager-agent.err.log |
| darwin | arm64 | launchd | /Library/Application Support/ssl-manager-agent/ | /var/log/ssl-manager-agent.log + /var/log/ssl-manager-agent.err.log |

## 全局约束

1. **心跳上报字段**: Agent 心跳请求体必须包含 `version`（当前 Agent 版本号）、`os`（运行时操作系统）、`arch`（运行时架构），Web Backend 据此匹配下发版本信息。
2. **版本比较规则**: 使用语义化版本（SemVer）比较。版本字符串格式为 `MAJOR.MINOR.PATCH`（如 `1.2.3`），不带 `v` 前缀。比较时按 major → minor → patch 数值逐级比较。不支持预发布版本标签。
3. **版本固定下载**: 下载接口 `GET /api/agent/binary` 返回的二进制必须与 `GET /api/agent/version` 返回的 MD5 一致。实现方式：版本缓存和二进制文件使用同一快照（扫描时同时记录文件路径和 MD5），确保版本检查和下载之间不会因 release 切换导致 MD5 不匹配。
4. **macOS launchd 命令**: 安装使用 `launchctl bootstrap system <plist_path>`，卸载使用 `launchctl bootout system/<label>`，重启使用 `launchctl kickstart -k system/<label>`。如果系统不支持新语法（macOS < 10.10），回退到 `launchctl load`/`launchctl unload`。
5. **macOS 日志文件清理**: 卸载时应删除 `/var/log/ssl-manager-agent.log` 和 `/var/log/ssl-manager-agent.err.log`（如果存在）。

## 需求

### 需求 1: CLI 子命令框架

**用户故事:** 作为系统管理员，我希望通过子命令管理 Agent 生命周期，这样我不需要记住复杂的命令就能完成常见操作。

#### 验收标准

1. 当用户执行 `ssl-manager-agent` 不带子命令时，程序应启动 Agent 守护进程（保持与现有安装的向后兼容）
2. 当用户执行 `ssl-manager-agent --help` 时，程序应显示所有可用子命令及简要说明
3. 程序应提供以下子命令：`uninstall`、`restart`、`logs`、`update`、`auto-update`、`config`、`version`
4. 当用户执行未知子命令时，程序应显示错误信息并列出可用子命令
5. 当用户执行 `ssl-manager-agent version` 时，程序应显示当前版本号、编译时间、OS/架构信息

### 需求 2: 卸载子命令

**用户故事:** 作为系统管理员，我希望能完整卸载 Agent，这样机器上不会残留任何文件或服务。

#### 验收标准

1. 当用户执行 `ssl-manager-agent uninstall` 时，程序应提示确认后再执行
2. 确认后，程序应通过服务管理器停止并禁用 Agent 服务
3. **Linux**: 删除 systemd unit 文件（`/etc/systemd/system/ssl-manager-agent.service`），然后执行 `systemctl daemon-reload`
4. **macOS**: 通过 `launchctl bootout system/com.ssl-manager.agent` 卸载服务并删除 plist 文件（`/Library/LaunchDaemons/com.ssl-manager.agent.plist`）；如果系统不支持 `bootout`（macOS < 10.10），回退到 `launchctl unload <plist_path>`
5. 确认后，程序应删除配置文件目录（Linux: `/etc/ssl-manager-agent/`，macOS: `/Library/Application Support/ssl-manager-agent/`）
6. 确认后，程序应删除 Agent 二进制文件（`/usr/local/bin/ssl-manager-agent`）
7. 确认后，程序应删除日志文件（macOS: `/var/log/ssl-manager-agent.log` 和 `/var/log/ssl-manager-agent.err.log`；Linux journald 日志由系统管理，不主动删除）
8. 当用户执行 `ssl-manager-agent uninstall --yes` 时，程序应跳过确认直接执行
9. 如果因权限不足导致操作失败，程序应提示需要 root/sudo 权限
10. 卸载完成后，程序应显示已删除的所有文件和服务的摘要

### 需求 3: 重启子命令

**用户故事:** 作为系统管理员，我希望能重启 Agent 服务，这样配置变更或故障排查可以立即生效。

#### 验收标准

1. 当用户执行 `ssl-manager-agent restart` 时，程序应通过服务管理器重启 Agent 服务
2. **Linux**: 执行 `systemctl restart ssl-manager-agent`
3. **macOS**: 执行 `launchctl kickstart -k system/com.ssl-manager.agent`
4. 重启成功后，程序应显示确认信息和服务状态
5. 如果服务管理器报告重启失败，程序应显示错误详情
6. 如果用户没有 root 权限，程序应提示需要 root/sudo 权限

### 需求 4: 查看日志子命令

**用户故事:** 作为系统管理员，我希望能查看 Agent 日志，这样我可以诊断问题和监控 Agent 行为。

#### 验收标准

1. 当用户执行 `ssl-manager-agent logs` 时，程序应显示最近 50 行 Agent 服务日志
2. **Linux**: 通过 `journalctl -u ssl-manager-agent --no-pager -n 50` 获取日志
3. **macOS**: 通过读取 `/var/log/ssl-manager-agent.log` 最后 50 行获取日志
4. 当用户执行 `ssl-manager-agent logs --follow` 时，程序应实时流式输出日志（Linux: `journalctl -u ssl-manager-agent -f`，macOS: `tail -f /var/log/ssl-manager-agent.log`）
5. 当用户执行 `ssl-manager-agent logs --lines N` 时，程序应显示最近 N 行日志
6. 如果日志获取失败，程序应显示错误信息和排查建议

### 需求 5: 手动更新子命令

**用户故事:** 作为系统管理员，我希望能手动触发 Agent 更新，这样我可以按需升级到最新版本。

#### 验收标准

1. 当用户执行 `ssl-manager-agent update` 时，程序应向 Web_Backend 查询最新版本信息（`GET /api/agent/version?os=<当前OS>&arch=<当前架构>`）
2. 如果最新版本与当前版本相同，程序应提示 Agent 已是最新版本
3. 如果有新版本可用，程序应显示当前版本和可用新版本，并从版本信息中的 `download_url` 下载新二进制
4. 下载完成后，程序应验证文件 MD5 校验和与版本信息中的 `md5` 字段一致
5. 如果 MD5 校验失败，程序应丢弃下载文件并显示错误信息
6. 校验通过后，程序应使用原子文件替换（先写临时文件再 rename）替换当前二进制
7. 替换成功后，程序应通过服务管理器重启 Agent 服务
8. 更新完成后，程序应显示旧版本号、新版本号和服务重启确认
9. 如果无法连接 Web_Backend，程序应显示连接错误和配置的服务器地址

### 需求 6: 自动更新机制

**用户故事:** 作为系统管理员，我希望 Agent 能在新版本可用时自动更新，这样所有部署的 Agent 无需人工干预即可保持最新。

#### 验收标准

1. 配置文件中应包含 `auto_update` 字段，默认值为 `true`
2. 当 `auto_update` 启用时，Agent 应在每次心跳响应中检查是否包含新版本信息
3. 当心跳响应中的 `latest_version` 大于当前版本且 auto_update 启用时，Agent 应从响应中的 `download_url` 下载新二进制
4. 下载完成后，Agent 应验证 MD5 校验和与心跳响应中的 `md5` 字段一致
5. 如果 MD5 校验失败，Agent 应记录错误日志并跳过本次更新，保留旧二进制继续运行
6. 校验通过后，Agent 应使用原子文件替换替换当前二进制
7. 如果原子替换失败（如磁盘满、权限不足），Agent 应记录错误并保留旧二进制继续运行
8. 替换成功后，Agent 应通过服务管理器重启自身服务
9. 如果重启失败，旧二进制已被替换但服务未重启，Agent 应记录 critical 日志（下次手动重启时会使用新版本）
10. 自动更新完成后，Agent 应记录旧版本号、新版本号和重启确认
11. 当 `auto_update` 禁用时，Agent 应跳过版本检查和更新操作

### 需求 7: 自动更新开关子命令

**用户故事:** 作为系统管理员，我希望能启用或禁用自动更新，这样我可以控制 Agent 是否自行升级。

#### 验收标准

1. 当用户执行 `ssl-manager-agent auto-update enable` 时，程序应将配置文件中的 `auto_update` 设为 `true`
2. 当用户执行 `ssl-manager-agent auto-update disable` 时，程序应将配置文件中的 `auto_update` 设为 `false`
3. 当用户执行 `ssl-manager-agent auto-update` 不带参数时，程序应显示当前自动更新状态（启用/禁用）
4. 设置变更后，程序应显示确认信息和新状态
5. 如果配置文件因权限无法写入，程序应提示需要 root/sudo 权限

### 需求 8: 交互式配置修改子命令

**用户故事:** 作为系统管理员，我希望能交互式修改 Agent 的服务器地址和 Token，这样我不需要手动编辑 YAML 文件。

#### 验收标准

1. 当用户执行 `ssl-manager-agent config` 时，程序应显示当前配置值（agent_token 部分脱敏，仅显示最后 8 位）
2. 当用户执行 `ssl-manager-agent config --server-url <url>` 时，程序应更新配置文件中的 `server_url`
3. 当用户执行 `ssl-manager-agent config --token <token>` 时，程序应更新配置文件中的 `agent_token`
4. 当用户执行 `ssl-manager-agent config --interactive` 时，程序应依次提示输入 server_url 和 agent_token，显示当前值作为默认值
5. 修改配置时，程序应验证 server_url 是合法 URL（以 http:// 或 https:// 开头）且 agent_token 非空
6. 如果验证失败，程序应显示具体错误信息且不修改配置文件
7. 配置保存成功后，程序应显示更新后的值并建议重启 Agent 服务使变更生效
8. 如果配置文件无法读写，程序应显示错误信息、文件路径和权限详情

### 需求 9: Web Backend 版本信息接口

**用户故事:** 作为平台运维人员，我希望 Web Backend 提供 Agent 二进制的版本元数据，这样 Agent 可以判断是否有可用更新。

#### 验收标准

1. Web_Backend 启动时，应扫描二进制目录（`./bin/`）中所有符合命名规则 `ssl-manager-agent-<os>-<arch>` 的文件，计算 MD5 校验和和文件大小
2. 版本号来源：从二进制文件同目录下的 `agent-version.txt` 文件读取（内容为单行版本字符串如 `1.2.3`），构建时由 CI/Makefile 写入
3. Web_Backend 应暴露 `GET /api/agent/version` 接口，返回 JSON：
   ```json
   {
     "version": "1.2.3",
     "releases": [
       {"os": "linux", "arch": "amd64", "md5": "abc123...", "size": 12345678, "download_url": "/api/agent/binary?os=linux&arch=amd64"},
       {"os": "linux", "arch": "arm64", "md5": "def456...", "size": 12345678, "download_url": "/api/agent/binary?os=linux&arch=arm64"},
       {"os": "darwin", "arch": "amd64", "md5": "ghi789...", "size": 12345678, "download_url": "/api/agent/binary?os=darwin&arch=amd64"},
       {"os": "darwin", "arch": "arm64", "md5": "jkl012...", "size": 12345678, "download_url": "/api/agent/binary?os=darwin&arch=arm64"}
     ]
   }
   ```
4. 当向 Agent 发送心跳响应时，Web_Backend 应在响应体中包含 `latest_version`、`md5` 和 `download_url`（匹配该 Agent 上报的 OS/架构）
5. Web_Backend 应每 5 分钟重新扫描二进制目录，检测新文件并更新缓存的 MD5 校验和
6. 如果请求的 OS/架构组合没有对应的 Agent 二进制，`GET /api/agent/version` 响应中应省略该组合；心跳响应中不包含版本信息
7. `GET /api/agent/binary` 接口应支持 `os` 和 `arch` 查询参数，支持 `linux`/`darwin` + `amd64`/`arm64` 组合

### 需求 10: 安装脚本集成

**用户故事:** 作为平台运维人员，我希望安装脚本能设置 CLI 子命令和自动更新默认值，这样新安装的 Agent 开箱即具备完整管理能力。

#### 验收标准

1. 安装脚本应将 Agent 二进制安装到 `/usr/local/bin/ssl-manager-agent`，确保该路径在系统 PATH 中，用户可直接执行 `ssl-manager-agent` 命令
2. 安装脚本完成后，应在配置文件中写入 `auto_update: true`
3. **Linux**: 安装脚本应创建 systemd unit 文件并启用服务
4. **macOS**: 安装脚本应创建 launchd plist 文件（`/Library/LaunchDaemons/com.ssl-manager.agent.plist`）并加载服务
5. 安装成功后，安装脚本应在安装摘要中显示可用的 CLI 子命令列表
6. 安装脚本应检测当前 OS 和架构，从 Web_Backend 下载对应的二进制（`/api/agent/binary?os=<os>&arch=<arch>`）
