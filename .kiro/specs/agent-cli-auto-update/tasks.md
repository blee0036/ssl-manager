# 实施计划: Agent CLI 子命令与自动更新

## 概述

按照设计文档推荐的低回归风险顺序实施：先实现纯函数/可测试模块（SemVer 比较、MD5 校验、原子替换、版本缓存），再实现 CLI 子命令和 Web Handler，最后集成心跳自动更新和安装脚本。

## 任务

- [x] 1. 实现版本比较模块与属性测试
  - [x] 1.1 创建 `internal/agent/version/version.go`，实现 `Parse`、`Compare`、`IsNewer` 函数
    - 定义 `Version` 结构体（Major, Minor, Patch int）
    - `Parse` 解析 "MAJOR.MINOR.PATCH" 格式字符串，拒绝无效输入
    - `Compare` 返回 -1/0/1，按 major → minor → patch 逐级比较
    - `IsNewer` 判断 remote 是否比 local 新
    - _需求: 全局约束 2, 6.3_

  - [x] 1.2 编写属性测试 `internal/agent/version/version_property_test.go`
    - **Property 1: SemVer 比较正确性**
    - **验证: 需求 6.3**
    - 使用 gopter 生成随机 (major, minor, patch) 三元组
    - 标签: `Feature: agent-cli-auto-update, Property 1: SemVer comparison correctness`

  - [x] 1.3 编写单元测试 `internal/agent/version/version_test.go`
    - 测试 Parse 对有效/无效输入的处理
    - 测试 Compare 的边界情况（相等、major 不同、minor 不同、patch 不同）
    - 测试 IsNewer 的各种组合
    - _需求: 全局约束 2_

- [x] 2. 实现 MD5 校验与原子替换模块
  - [x] 2.1 创建 `internal/agent/updater/verify.go`，实现 `VerifyMD5` 函数
    - 读取文件内容计算 MD5 哈希
    - 与期望值比较，不匹配时返回描述性错误
    - _需求: 5.4, 6.4_

  - [x] 2.2 编写属性测试 `internal/agent/updater/md5_property_test.go`
    - **Property 2: MD5 校验正确性**
    - **验证: 需求 5.4, 6.4**
    - 使用 gopter 生成随机字节切片，验证正确 MD5 通过、错误 MD5 拒绝
    - 标签: `Feature: agent-cli-auto-update, Property 2: MD5 verification correctness`

  - [x] 2.3 创建 `internal/agent/updater/atomic.go`，实现 `AtomicReplace` 函数
    - 复制目标文件权限到新文件
    - 使用 `os.Rename` 原子替换
    - 失败时删除临时文件并返回错误
    - _需求: 5.6, 6.6_

  - [x] 2.4 编写属性测试 `internal/agent/updater/atomic_property_test.go`
    - **Property 3: 原子文件替换完整性**
    - **验证: 需求 5.6, 6.6**
    - 使用 gopter 生成随机文件内容，验证替换后内容正确、失败时原文件不变
    - 标签: `Feature: agent-cli-auto-update, Property 3: Atomic file replace integrity`

- [x] 3. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 4. 实现 CLI 辅助函数与属性测试
  - [x] 4.1 创建 `internal/agent/cli/helpers.go`，实现 Token 脱敏和 URL 验证函数
    - `MaskToken(token string) string`: 长度 >= 8 时保留最后 8 位、前面替换为 `*`；长度 < 8 时全部为 `*`
    - `ValidateURL(s string) bool`: 仅接受 `http://` 或 `https://` 开头的字符串
    - _需求: 8.1, 8.5_

  - [x] 4.2 编写属性测试 `internal/agent/cli/mask_property_test.go`
    - **Property 4: Token 脱敏正确性**
    - **验证: 需求 8.1**
    - 使用 gopter 生成随机长度字符串
    - 标签: `Feature: agent-cli-auto-update, Property 4: Token masking correctness`

  - [x] 4.3 编写属性测试 `internal/agent/cli/validate_property_test.go`
    - **Property 5: URL 验证正确性**
    - **验证: 需求 8.5**
    - 使用 gopter 生成随机字符串（含有效/无效 URL）
    - 标签: `Feature: agent-cli-auto-update, Property 5: URL validation correctness`

- [x] 5. 实现平台服务管理器抽象
  - [x] 5.1 创建 `internal/agent/platform/service.go`，定义 `ServiceManager` 接口
    - 接口方法: `Stop`, `Start`, `Restart`, `Disable`, `Enable`, `IsActive`, `Uninstall`, `GetLogs`
    - 创建 `NewServiceManager()` 工厂函数，根据 `runtime.GOOS` 返回对应实现
    - _需求: 2.2, 3.1, 4.1_

  - [x] 5.2 创建 `internal/agent/platform/systemd.go`，实现 Linux systemd 服务管理
    - `Restart()` → `systemctl restart ssl-manager-agent`
    - `Stop()` → `systemctl stop ssl-manager-agent`
    - `Disable()` → `systemctl disable ssl-manager-agent`
    - `Uninstall()` → 删除 unit 文件 + `systemctl daemon-reload`
    - `GetLogs()` → `journalctl -u ssl-manager-agent --no-pager -n <lines>` 或 `-f`
    - _需求: 2.3, 3.2, 4.2_

  - [x] 5.3 创建 `internal/agent/platform/launchd.go`，实现 macOS launchd 服务管理
    - `Restart()` → `launchctl kickstart -k system/com.ssl-manager.agent`
    - `Stop()` → `launchctl bootout system/com.ssl-manager.agent`（回退 `launchctl unload`）
    - `Uninstall()` → bootout + 删除 plist + 删除日志文件
    - `GetLogs()` → 读取 `/var/log/ssl-manager-agent.log` 最后 N 行或 `tail -f`
    - _需求: 2.4, 3.3, 4.3, 全局约束 4, 全局约束 5_

  - [x] 5.4 编写单元测试 `internal/agent/platform/service_test.go`
    - Mock exec.Command 验证正确的系统命令被调用
    - 测试 Linux 和 macOS 的命令参数
    - _需求: 2.2-2.4, 3.1-3.3_

- [x] 6. 实现 Agent 配置扩展
  - [x] 6.1 修改 `internal/agent/config/` 中的配置结构体，新增 `auto_update` 字段
    - 添加 `AutoUpdate *bool` 字段（yaml tag: `auto_update,omitempty`）
    - 实现 `IsAutoUpdateEnabled()` 方法（nil 视为 true）
    - 添加配置文件写入功能（用于 config 和 auto-update 子命令修改配置）
    - _需求: 6.1, 7.1-7.2_

  - [x] 6.2 编写单元测试验证配置读写和默认值逻辑
    - 测试 nil AutoUpdate 默认为 true
    - 测试显式 true/false 的序列化和反序列化
    - _需求: 6.1, 7.1-7.2_

- [x] 7. 实现 CLI Router 和子命令
  - [x] 7.1 重构 `cmd/agent/main.go`，添加 CLI 路由逻辑
    - 添加 `Version` 和 `BuildTime` 编译时注入变量
    - 在 `flag.Parse()` 之前检查 `os.Args` 是否包含子命令
    - 实现 `printUsage()` 帮助信息输出
    - 无子命令时保持现有守护进程启动逻辑（向后兼容）
    - _需求: 1.1, 1.2, 1.3, 1.4_

  - [x] 7.2 实现 `version` 子命令
    - 显示版本号、编译时间、`runtime.GOOS`/`runtime.GOARCH`
    - 支持 `--version` / `-v` 全局 flag
    - _需求: 1.5_

  - [x] 7.3 实现 `uninstall` 子命令
    - 默认提示确认，`--yes` 跳过确认
    - 调用 ServiceManager 停止、禁用、卸载服务
    - 删除配置目录、二进制文件、日志文件（macOS）
    - 显示已删除文件摘要
    - 权限不足时提示 root/sudo
    - _需求: 2.1-2.10_

  - [x] 7.4 实现 `restart` 子命令
    - 调用 ServiceManager.Restart()
    - 显示重启确认和服务状态
    - 权限不足时提示 root/sudo
    - _需求: 3.1-3.6_

  - [x] 7.5 实现 `logs` 子命令
    - 默认显示最近 50 行
    - 支持 `--follow` 实时流式输出
    - 支持 `--lines N` 指定行数
    - 调用 ServiceManager.GetLogs()
    - _需求: 4.1-4.6_

  - [x] 7.6 实现 `auto-update` 子命令
    - 无参数时显示当前状态
    - `enable` 设置 auto_update 为 true
    - `disable` 设置 auto_update 为 false
    - 修改配置文件并显示确认
    - _需求: 7.1-7.5_

  - [x] 7.7 实现 `config` 子命令
    - 无参数时显示当前配置（token 脱敏）
    - `--server-url <url>` 更新 server_url（验证 URL 格式）
    - `--token <token>` 更新 agent_token（验证非空）
    - `--interactive` 交互式依次提示输入
    - 保存后建议重启服务
    - _需求: 8.1-8.8_

- [x] 8. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 9. 实现 Updater 模块（下载与更新执行）
  - [x] 9.1 创建 `internal/agent/updater/updater.go`，实现 Updater 结构体和核心方法
    - 定义 `Updater` 结构体（ServerURL, CurrentPath, HTTPClient）
    - 定义 `VersionInfo`、`VersionResponse`、`ReleaseItem` 数据结构
    - 实现 `CheckVersion(os, arch string)`: GET /api/agent/version，按 os/arch 筛选
    - 实现 `Download(downloadURL string)`: 下载到同目录临时文件
    - 实现 `Execute(currentVersion, os, arch string, svcMgr)`: 完整更新流程
    - _需求: 5.1-5.9_

  - [x] 9.2 实现 `update` CLI 子命令，调用 Updater 执行手动更新
    - 查询最新版本，比较当前版本
    - 已是最新时提示用户
    - 有新版本时下载、校验 MD5、原子替换、重启服务
    - 显示旧版本号、新版本号和重启确认
    - 网络错误时显示服务器地址
    - _需求: 5.1-5.9_

  - [x] 9.3 编写单元测试 `internal/agent/updater/updater_test.go`
    - 使用 httptest Mock HTTP 服务器
    - 测试 CheckVersion 解析和筛选逻辑
    - 测试 Download 成功和失败场景
    - 测试 Execute 完整流程（Mock ServiceManager）
    - _需求: 5.1-5.9_

- [x] 10. 实现 Web Backend 版本缓存
  - [x] 10.1 创建 `internal/web/service/version_cache.go`，实现 VersionCache
    - 定义 `ReleaseInfo` 结构体（OS, Arch, MD5, Size, DownloadURL, FilePath）
    - 实现 `NewVersionCache(binDir, scanInterval)`: 创建并启动定时扫描
    - 实现 `Scan()`: 读取 agent-version.txt + 遍历二进制文件计算 MD5
    - 实现 `GetVersion()`, `GetReleases()`, `GetRelease(os, arch)`, `Stop()`
    - 使用 sync.RWMutex 保证并发安全
    - _需求: 9.1, 9.2, 9.5_

  - [x] 10.2 编写单元测试 `internal/web/service/version_cache_test.go`
    - 使用临时目录模拟 bin 目录
    - 测试扫描逻辑、缓存更新、并发读写安全
    - 测试 agent-version.txt 不存在时的行为
    - _需求: 9.1, 9.2, 9.5_

- [x] 11. 实现 Web Backend 版本信息 Handler 和心跳增强
  - [x] 11.1 扩展 `internal/web/handler/install_handler.go`，新增 `GetVersionInfo` 路由
    - 注册 `GET /api/agent/version` 路由
    - 支持可选 `?os=<os>&arch=<arch>` 查询参数筛选
    - 无参数时返回全量 releases 列表
    - 注入 VersionCache 依赖
    - _需求: 9.3, 9.6, 9.7_

  - [x] 11.2 改造 `DownloadBinary` handler，通过 VersionCache 获取文件路径
    - 从 VersionCache.GetRelease 获取 ReleaseInfo
    - 使用 ReleaseInfo.FilePath 提供文件（确保 MD5 一致性）
    - 未找到时返回 404
    - _需求: 全局约束 3, 9.7_

  - [x] 11.3 修改 `internal/web/handler/agent_handler.go` 的 Heartbeat handler
    - 注入 VersionCache 依赖
    - 从心跳请求中获取 OS/Arch
    - 在响应中附加 `latest_version`、`md5`、`download_url`
    - 无匹配 release 时不包含版本信息
    - _需求: 9.4, 9.6_

  - [x] 11.4 编写集成测试验证版本接口和心跳响应
    - 使用 httptest 模拟完整请求流程
    - 验证 GET /api/agent/version JSON 结构
    - 验证心跳响应包含版本信息
    - _需求: 9.3, 9.4_

- [x] 12. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 13. 实现心跳自动更新 Worker
  - [x] 13.1 创建 `internal/agent/worker/autoupdate.go`，实现 AutoUpdateWorker
    - 定义 `AutoUpdateWorker` 结构体
    - 实现 `HandleHeartbeatResponse`: 检查版本 → 下载 → 校验 → 替换 → 重启
    - 失败安全策略：任何步骤失败记录日志但不影响当前运行
    - _需求: 6.2-6.11_

  - [x] 13.2 修改现有心跳 Worker，集成 AutoUpdateWorker
    - 扩展 `HeartbeatResponse` 结构体新增版本字段
    - 心跳成功后检查 auto_update 配置
    - 启用时调用 AutoUpdateWorker.HandleHeartbeatResponse
    - _需求: 6.1-6.3, 6.11_

  - [x] 13.3 编写单元测试验证自动更新逻辑
    - Mock HTTP 和 ServiceManager
    - 测试版本比较触发更新
    - 测试 auto_update 禁用时跳过
    - 测试各种失败场景的错误恢复
    - _需求: 6.2-6.11_

- [x] 14. 集成安装脚本和 Makefile 更新
  - [x] 14.1 更新 Makefile `build-agent-release` 目标
    - 添加 `VERSION` 变量（从 VERSION 文件或默认 "0.0.0"）
    - 添加 `BUILD_TIME` 变量
    - 构建时通过 ldflags 注入 `-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)`
    - 构建时写入 `$(BIN_DIR)/agent-version.txt`
    - _需求: 9.2_

  - [x] 14.2 修改安装脚本模板，集成自动更新默认配置
    - 确保生成的 config.yaml 包含 `auto_update: true`
    - 安装完成后显示可用 CLI 子命令列表
    - _需求: 10.2, 10.5_

  - [x] 14.3 集成 VersionCache 到 Web Backend 启动流程
    - 在 Web Backend 启动时创建 VersionCache（binDir 从配置获取）
    - 注入到 InstallHandler 和 AgentHandler
    - 确保 graceful shutdown 时调用 VersionCache.Stop()
    - _需求: 9.1, 9.5_

- [x] 15. 最终检查点 - 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

## 备注

- 标记 `*` 的任务为可选，可跳过以加速 MVP 交付
- 每个任务引用具体需求条款以确保可追溯性
- 检查点确保增量验证，避免问题累积
- 属性测试验证通用正确性属性，单元测试验证具体示例和边界情况
- 实施顺序遵循设计文档推荐：纯函数 → CLI/Handler → 集成
