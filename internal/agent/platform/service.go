package platform

// ServiceManager 抽象不同平台的服务管理操作
type ServiceManager interface {
	Stop() error                          // 用于: uninstall
	Start() error                         // 用于: restart（内部）
	Restart() error                       // 用于: restart, update, auto-update
	Disable() error                       // 用于: uninstall
	Enable() error                        // 用于: 安装脚本（非 CLI 直接调用）
	IsActive() (bool, error)              // 用于: restart（显示状态）
	Uninstall() error                     // 用于: uninstall（删除 unit/plist 文件 + daemon-reload/bootout）
	GetLogs(lines int, follow bool) error // 用于: logs（执行 journalctl/tail）
}

// NewServiceManager 根据 runtime.GOOS 返回对应实现。
// 具体实现在各平台文件中定义（systemd.go, launchd.go, service_other.go）。
