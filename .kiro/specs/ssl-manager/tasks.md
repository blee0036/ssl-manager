# Implementation Plan: SSL 证书管理系统

## Overview

基于 Go 语言实现 SSL 证书管理系统，包含 Web Backend 和 Agent 两个独立组件。Web Backend 提供 RESTful API、调度器和告警功能；Agent 负责证书同步和部署。使用 SQLite3 存储数据，证书 PEM 文件以文件形式存储。

## Tasks

- [x] 1. 项目结构与基础设施搭建
  - [x] 1.1 创建项目目录结构和 Go module 初始化
    - 创建 Web Backend 和 Agent 的目录结构
    - 初始化 Go module，添加核心依赖（chi/mux 路由、SQLite3 驱动、JWT 库、bcrypt、gopter 等）
    - 创建 Makefile 用于构建两个二进制
    - _Requirements: 1.6_

  - [x] 1.2 定义核心数据模型和接口类型
    - 创建所有数据结构体（Certificate、Machine、MachineCertificate、DeploymentLog 等）
    - 创建 Service 输入/输出类型（CreateCertInput、CertFilter 等）
    - 创建 ErrorResponse 统一错误响应结构
    - _Requirements: 5.1, 3.1, 7.1_

  - [x] 1.3 实现 SQLite3 数据库初始化和迁移
    - 创建数据库连接管理
    - 实现所有表的 CREATE TABLE 语句（users、machines、certificates、machine_certificates、deployment_logs、domains、domain_monitor_results、alerts、notification_channels、audit_logs、thirdpart_dns、thirdpart_dns_sync_logs）
    - 实现 `./data` 目录自动创建逻辑
    - _Requirements: 1.1, 1.6_

  - [x] 1.4 实现 config.json 配置管理
    - 定义配置结构体（Web 外部访问地址、Agent 参数、告警参数、Certbot 参数、只读模式等）
    - 实现配置文件读取、写入和验证
    - 实现启动时文件权限检查（非 0600 输出安全警告）
    - _Requirements: 1.4, 1.5, 16.8_

  - [x] 1.5 编写属性测试：配置序列化往返一致性
    - **Property 1: 配置序列化往返一致性**
    - **Validates: Requirements 1.5**

- [x] 2. 用户认证与权限系统
  - [x] 2.1 实现用户 Repository 层
    - 实现用户 CRUD 操作（Create、GetByUsername、GetByID、List、Update、Disable）
    - 密码使用 bcrypt 哈希存储
    - _Requirements: 2.1, 2.3, 16.7_

  - [x] 2.2 实现认证 Service 层
    - 实现登录验证（用户名+密码 → JWT Token）
    - 实现只读密码验证（config.json 中 readonly.enabled + view_password）
    - 实现会话失效逻辑（禁用用户时使所有活跃会话失效）
    - 错误信息统一为通用消息，不泄露用户是否存在
    - _Requirements: 2.1, 2.2, 2.5, 2.9_

  - [x] 2.3 实现中间件层
    - 实现 JWT 认证中间件（AuthMiddleware）
    - 实现角色验证中间件（RoleMiddleware）
    - 实现只读模式拦截中间件（ReadonlyMiddleware，使用接口白名单）
    - 实现审计日志中间件（AuditMiddleware）
    - 实现 Agent Token 认证中间件（AgentAuthMiddleware，校验 machine_id 与 token 对应关系）
    - _Requirements: 2.4, 2.6, 2.7, 16.6_

  - [x] 2.4 编写属性测试：认证与权限
    - **Property 2: 无效凭证统一拒绝**
    - **Property 3: 非管理员用户管理接口拒绝**
    - **Property 4: 只读会话写操作拒绝**
    - **Property 29: 只读模式接口白名单**
    - **Validates: Requirements 2.2, 2.4, 2.6, 2.7**

  - [x] 2.5 编写属性测试：密码 bcrypt 哈希存储
    - **Property 26: 密码 bcrypt 哈希存储**
    - **Validates: Requirements 16.7**

- [x] 3. 系统初始化流程
  - [x] 3.1 实现初始化 Handler 和 Service
    - 实现初始化状态检测（数据库不存在或无管理员用户 → 重定向到 /init）
    - 实现管理员用户创建接口
    - 实现系统参数配置保存接口（写入 config.json）
    - 初始化完成后访问 /init 返回 403
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

  - [x] 3.2 编写单元测试：初始化流程
    - 测试初始化状态检测逻辑
    - 测试重复初始化拒绝
    - _Requirements: 1.1, 1.3_

- [x] 4. Checkpoint - 确保基础设施测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 5. 机器管理
  - [x] 5.1 实现机器 Repository 层
    - 实现机器 CRUD 操作
    - Agent Token 哈希存储（不明文存储）
    - 实现心跳更新和状态查询
    - _Requirements: 3.1, 16.1_

  - [x] 5.2 实现机器 Service 层
    - 实现机器创建（生成唯一 Agent Token，存储哈希值）
    - 实现安装命令生成（包含 Web 外部访问地址、machine_id、agent_token）
    - 实现 Token 吊销和重新生成
    - 实现心跳更新逻辑
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

  - [x] 5.3 实现机器 HTTP Handler
    - 注册路由 /api/machines/*
    - 实现 CRUD 接口、Token 管理接口、安装命令接口
    - _Requirements: 3.1, 3.2, 3.5_

  - [x] 5.4 编写属性测试：机器管理
    - **Property 5: 机器创建生成唯一 Token**
    - **Property 6: 安装命令包含必要组件**
    - **Property 7: 已吊销 Token 全面拒绝**
    - **Property 24: Token 哈希存储**
    - **Validates: Requirements 3.1, 3.2, 3.4, 16.1**

- [x] 6. Agent 心跳与状态管理
  - [x] 6.1 实现心跳 Handler 和 Service
    - 实现 POST /api/agent/heartbeat 接口
    - 更新机器的心跳时间、Agent 版本、主机名、IP、OS、Arch
    - 已吊销 Token 返回 401 并触发告警
    - _Requirements: 4.1, 4.4, 4.5_

  - [x] 6.2 实现心跳超时检测逻辑
    - 在 Scheduler 中实现定期检查心跳超时
    - 超过 heartbeat_timeout_seconds 标记为 offline
    - 从未收到心跳显示为 pending
    - 离线机器重新心跳恢复为 online
    - _Requirements: 4.2, 4.3, 4.4_

  - [x] 6.3 编写属性测试：心跳超时状态转换
    - **Property 8: 心跳超时状态转换**
    - **Validates: Requirements 4.2**

- [x] 7. 证书管理
  - [x] 7.1 实现证书 Repository 层
    - 实现证书元数据 CRUD 操作
    - 实现证书文件存储管理（./data/certificates/<id>/）
    - _Requirements: 5.1, 5.8_

  - [x] 7.2 实现证书 Service 层
    - 实现 PEM 解析（提取域名、过期时间、颁发者、SHA256 指纹、证书链完整性）
    - 实现证书与私钥匹配验证
    - 实现证书上传（验证 → 保存文件 → 保存元数据）
    - 实现证书更新（覆盖文件 → 更新元数据 → 标记关联 Machine_Certificate 为待同步 → 递增 config_revision）
    - 实现证书删除
    - _Requirements: 5.1, 5.2, 5.3, 5.6, 5.7, 5.8_

  - [x] 7.3 实现证书 HTTP Handler
    - 注册路由 /api/certificates/*
    - 实现上传、更新、删除、列表、详情接口
    - API 响应使用 CertificateResponse DTO（不包含私钥和文件路径）
    - _Requirements: 5.1, 5.6, 16.9_

  - [x] 7.4 编写属性测试：证书管理
    - **Property 9: 证书 PEM 解析正确性**
    - **Property 10: 证书私钥不匹配拒绝**
    - **Property 11: 证书更新触发待同步标记**
    - **Property 30: 证书链完整性记录**
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.7**

- [x] 8. Certbot 集成与自动续签
  - [x] 8.1 实现 Certbot 封装层
    - 实现 Certbot CLI 调用封装（certonly 命令）
    - 实现 Cloudflare DNS-01 验证模式
    - 实现手动 DNS 验证模式（生成 TXT 记录要求）
    - 实现 Certbot 输出目录读取和证书文件提取
    - _Requirements: 5.4, 5.5, 6.2, 6.6, 6.7_

  - [x] 8.2 实现自动续签 Scheduler
    - 实现续签阈值检测（距过期 ≤ default_before_days）
    - 对 certbot_cloudflare_dns 来源证书自动续签
    - 对 certbot_manual_dns 来源证书发送提醒通知
    - 续签成功后覆盖证书文件、更新元数据、标记待同步
    - 续签失败记录错误、发送告警、按策略重试
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

  - [x] 8.3 编写属性测试：续签阈值检测
    - **Property 12: 续签阈值检测**
    - **Validates: Requirements 6.1, 12.1**

- [x] 9. 证书部署配置
  - [x] 9.1 实现 MachineCertificate Repository 和 Service
    - 实现部署配置 CRUD
    - 路径非空验证（cert_path、private_key_path）
    - 创建/更新时递增 config_revision
    - 实现手动部署触发（标记待同步 + 递增 config_revision）
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x] 9.2 实现 MachineCertificate HTTP Handler
    - 注册路由 /api/machines/{id}/certificates
    - 实现添加、编辑、删除部署配置接口
    - 实现手动部署触发接口
    - _Requirements: 7.1, 7.4, 7.5_

  - [x] 9.3 编写属性测试：部署配置
    - **Property 13: 部署路径非空验证**
    - **Property 27: config_revision 递增触发部署**
    - **Validates: Requirements 7.2, 7.4, 7.5, 8.1**

- [x] 10. Checkpoint - 确保 Web Backend 核心功能测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 11. Agent 核心实现
  - [x] 11.1 实现 Agent 配置和状态管理
    - 实现 YAML 配置文件读取（server_url、machine_id、agent_token、poll_interval_seconds）
    - 实现本地状态文件持久化（/etc/ssl-manager-agent/state.json）
    - 实现 Agent 启动和主循环
    - _Requirements: 14.5, 14.6_

  - [x] 11.2 实现 Agent 心跳 Worker
    - 按 poll_interval_seconds 定期发送心跳
    - 启动时立即发送首次心跳
    - 心跳被拒绝（401）时停止所有操作
    - _Requirements: 4.1, 14.4_

  - [x] 11.3 实现 Agent 证书同步 Worker
    - 拉取机器证书配置列表（GET /api/agent/machines/{machine_id}/certificates）
    - 判断是否需要部署（本地文件不存在、指纹不一致、config_revision 不同、状态为 pending）
    - _Requirements: 8.1_

  - [x] 11.4 实现 Agent 证书部署 Worker
    - 下载证书（GET /api/agent/machine-certificates/{id}/download）
    - 校验证书与私钥匹配
    - 创建目标目录（权限 0755）
    - 写入临时文件，双文件都成功后原子替换
    - 设置文件权限（证书 0644、私钥 0600）
    - 按顺序执行 post_deploy_commands（超时 60 秒，失败即停）
    - 上报 Deployment_Log
    - 更新本地 last_synced_revision
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9_

  - [x] 11.5 实现 Agent API 接口（Web Backend 侧）
    - 实现 GET /api/agent/machines/{machine_id}/certificates
    - 实现 GET /api/agent/machine-certificates/{machine_certificate_id}/download（校验 machine_id 与 token 对应关系）
    - 实现 POST /api/agent/deployment-logs
    - _Requirements: 8.1, 16.2_

  - [x] 11.6 编写属性测试：Agent 部署
    - **Property 14: 指纹不一致触发同步**
    - **Property 15: 命令有序执行与失败即停**
    - **Property 16: 写入失败保留原文件**
    - **Property 25: 命令超时强制终止**
    - **Property 28: 证书下载接口机器绑定校验**
    - **Property 31: 部署文件双文件一致性**
    - **Validates: Requirements 8.1, 8.4, 8.5, 8.7, 16.2, 16.5, 8.2, 8.8**

  - [x] 11.7 编写属性测试：Agent 配置 YAML 往返一致性
    - **Property 22: Agent 配置 YAML 往返一致性**
    - **Validates: Requirements 14.4**

- [x] 12. 部署日志管理
  - [x] 12.1 实现 DeploymentLog Repository 和 Service
    - 实现日志保存（Agent 上报时调用）
    - 实现日志保留上限（每个 Machine_Certificate 最多 30 条，超出删除最旧）
    - 实现按时间倒序查询
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [x] 12.2 实现 DeploymentLog HTTP Handler
    - 注册路由 /api/machines/{id}/deployment-logs
    - 实现日志列表查询接口
    - _Requirements: 9.3_

  - [x] 12.3 编写属性测试：部署日志
    - **Property 17: 部署日志保留上限**
    - **Property 18: 部署日志时间倒序**
    - **Validates: Requirements 9.2, 9.3**

- [x] 13. 域名 SSL 状态监控
  - [x] 13.1 实现域名监控 Repository 和 Service
    - 实现域名监控 CRUD
    - 实现 TLS 握手探测（使用 SNI）
    - 记录 DNS 解析结果、TLS 状态、证书信息、域名匹配、证书链完整性
    - 线上指纹与系统证书指纹不一致时标记异常
    - DNS 无法解析或 TLS 握手失败时触发告警
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

  - [x] 13.2 实现域名监控 HTTP Handler
    - 注册路由 /api/domains/*
    - 实现 CRUD 接口和手动探测接口
    - _Requirements: 10.1, 10.5_

  - [x] 13.3 实现域名监控 Scheduler 任务
    - 在 Scheduler 中注册定期域名监控任务
    - _Requirements: 10.2_

  - [x] 13.4 编写属性测试：域名指纹不一致标记异常
    - **Property 19: 域名指纹不一致标记异常**
    - **Validates: Requirements 10.4**

- [x] 14. 第三方 DNS 上游与域名同步
  - [x] 14.1 实现 ThirdpartDNS Repository 和 Service
    - 实现 DNS 上游配置 CRUD
    - 实现 Cloudflare API 客户端（验证 Token、拉取域名记录）
    - 实现同步逻辑（main_domains 为空全量抓取，非空按范围抓取 A/AAAA/CNAME）
    - 同步结果写入域名列表并可选创建 Domain_Monitor
    - 记录同步日志到 thirdpart_dns_sync_logs
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [x] 14.2 实现 ThirdpartDNS HTTP Handler
    - 注册路由 /api/thirdpart-dns/*
    - 实现配置管理接口和同步触发接口
    - _Requirements: 11.1, 11.2_

- [x] 15. Checkpoint - 确保监控和 DNS 同步测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [ ] 16. 告警通知系统
  - [x] 16.1 实现告警 Service 和通知渠道
    - 实现 Lark Webhook 发送器
    - 实现 Telegram Bot API 发送器
    - 实现告警抑制逻辑（同一事件未恢复时不重复发送）
    - 实现告警恢复通知
    - 实现告警测试发送
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 12.7_

  - [x] 16.2 实现告警 HTTP Handler
    - 注册路由 /api/alerts/*
    - 实现通知渠道配置接口、告警历史查询接口、测试发送接口
    - _Requirements: 12.3, 12.4, 12.5_

  - [x] 16.3 集成告警触发点
    - 在证书过期检测中触发告警
    - 在续签失败时触发告警
    - 在 Agent 离线时触发告警
    - 在已吊销 Token 请求时触发告警
    - 在部署失败时触发告警
    - 在域名探测失败时触发告警
    - 在 Cloudflare 同步失败时触发告警
    - _Requirements: 12.1, 12.2_

  - [x] 16.4 编写属性测试：重复告警抑制
    - **Property 20: 重复告警抑制**
    - **Validates: Requirements 12.6**

- [ ] 17. 审计日志
  - [x] 17.1 实现审计日志 Repository 和 Service
    - 实现审计日志记录（actor_type、actor_id、action、target_type、target_id、detail、ip）
    - detail 字段禁止记录 Token、私钥、Webhook URL 等敏感信息明文
    - 实现按时间倒序查询
    - _Requirements: 13.1, 13.2, 13.3, 16.10_

  - [-] 17.2 实现审计日志 HTTP Handler
    - 注册路由 /api/audit-logs/*
    - 实现日志列表查询接口
    - _Requirements: 13.3_

  - [-] 17.3 编写属性测试：写操作审计日志完整性
    - **Property 21: 写操作审计日志完整性**
    - **Validates: Requirements 13.1**

- [ ] 18. 仪表盘与系统配置
  - [-] 18.1 实现仪表盘 Service 和 Handler
    - 统计证书总数、15 天内过期数、已过期数
    - 统计在线/离线机器数量
    - 统计最近 24 小时部署失败数、续签失败数
    - 统计域名 SSL 异常数量
    - 异常指标醒目标记
    - _Requirements: 15.1, 15.2_

  - [x] 18.2 实现系统配置 Handler
    - 注册路由 /api/system/*
    - 实现配置读取和更新接口
    - 返回配置时对 Token、Webhook URL、密码字段脱敏
    - _Requirements: 1.4, 2.8_

  - [x] 18.3 编写属性测试：仪表盘统计准确性
    - **Property 23: 仪表盘统计准确性**
    - **Validates: Requirements 15.1**

- [x] 19. Agent 安装脚本
  - [x] 19.1 实现 Agent 安装脚本生成和二进制分发
    - 实现安装脚本模板（curl 下载、创建配置目录、写入配置、创建 systemd 服务）
    - 非 systemd 环境输出错误提示并给出手动运行方式
    - 实现 Agent 二进制下载接口
    - _Requirements: 14.1, 14.2, 14.3_

- [x] 20. 安全加固与接口脱敏
  - [x] 20.1 实现安全相关逻辑
    - 确保所有返回配置的接口对敏感字段脱敏
    - 确保证书下载接口校验 machine_id 与 token 对应关系
    - 确保 Agent 不提供远程 shell 接口
    - 确保 Agent 只执行 Web_Backend 配置的命令
    - _Requirements: 2.8, 16.2, 16.3, 16.4, 16.9_

- [x] 21. 路由注册与应用启动整合
  - [x] 21.1 整合所有路由和启动逻辑
    - 注册所有 HTTP Handler 和中间件
    - 实现 Scheduler 启动（续签检测、心跳超时检测、域名监控）
    - 实现优雅关闭
    - 实现 Agent 主程序入口
    - _Requirements: 1.1, 6.1, 10.2_

  - [x] 21.2 编写集成测试
    - 测试完整的认证流程
    - 测试证书上传和部署配置流程
    - 测试 Agent 心跳和证书同步流程
    - _Requirements: 2.1, 5.1, 8.1_

- [x] 22. Final Checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 每个任务引用了具体的需求编号以确保可追溯性
- Checkpoints 确保增量验证
- 属性测试验证设计文档中定义的通用正确性属性
- 单元测试验证具体示例和边界条件
- Web Backend 和 Agent 是两个独立的 Go 二进制，共享部分数据结构定义
- 证书 PEM 文件以文件形式存储在 ./data/certificates/ 下，SQLite3 只保存元数据

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "1.4"] },
    { "id": 2, "tasks": ["1.5", "2.1", "5.1", "7.1"] },
    { "id": 3, "tasks": ["2.2", "2.3", "5.2", "7.2"] },
    { "id": 4, "tasks": ["2.4", "2.5", "3.1", "5.3", "7.3"] },
    { "id": 5, "tasks": ["3.2", "5.4", "6.1", "7.4", "8.1"] },
    { "id": 6, "tasks": ["6.2", "6.3", "8.2", "8.3", "9.1"] },
    { "id": 7, "tasks": ["9.2", "9.3", "11.1", "12.1"] },
    { "id": 8, "tasks": ["11.2", "11.3", "11.5", "12.2", "12.3"] },
    { "id": 9, "tasks": ["11.4", "11.6", "11.7", "13.1"] },
    { "id": 10, "tasks": ["13.2", "13.3", "13.4", "14.1"] },
    { "id": 11, "tasks": ["14.2", "16.1"] },
    { "id": 12, "tasks": ["16.2", "16.3", "16.4", "17.1"] },
    { "id": 13, "tasks": ["17.2", "17.3", "18.1", "18.2"] },
    { "id": 14, "tasks": ["18.3", "19.1", "20.1"] },
    { "id": 15, "tasks": ["21.1"] },
    { "id": 16, "tasks": ["21.2"] }
  ]
}
```
