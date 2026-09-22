package middleware

import (
	"hivemtk-user/internal/system/install"
	"sync"
)

// InstallStatus 安装态检查器。
//
// 本类型只承载 install.lock 状态与版本/超管账号读取。历史上它叫 LicenseChecker
// 并向平台换取授权；hivemtk 开源后那段已整体移除，线上平台域亦已于 2026-09 下线。
// 判断"是否已初始化"的唯一依据是 install.lock.Initialized == true。
// 无字段：安装态全部就地读 install.lock，没有任何需要跨调用保有的可变状态。
type InstallStatus struct{}

var (
	globalInstallStatus *InstallStatus
	installStatusMu     sync.RWMutex
)

// InitInstallStatus 装配全局安装态检查器。
//
// 无参：安装态只来自本地 install.lock，没有任何"向平台确认"的地址需要记录。
// 曾经这里收过 serverURL / licenseKey 两个形参并写进结构体，实测全仓无一处读取。
func InitInstallStatus() {
	installStatusMu.Lock()
	defer installStatusMu.Unlock()
	globalInstallStatus = &InstallStatus{}
}

// GetInstallStatus 返回全局安装态检查器，未初始化时返回 nil
func GetInstallStatus() *InstallStatus {
	installStatusMu.RLock()
	defer installStatusMu.RUnlock()
	return globalInstallStatus
}

// GetInitStatus 返回初始化状态 DTO
func (c *InstallStatus) GetInitStatus() *install.Status {
	if c == nil {
		return &install.Status{State: "NOT_INSTALLED", Initialized: false, HasAdmin: false}
	}
	return install.GetStatus()
}

// HasInstallLockAdmin 判定 install.lock 是否已记录超管账号
func (c *InstallStatus) HasInstallLockAdmin() bool {
	return install.GetAdminUsername() != ""
}

// SetAdminInit 写入 install.lock 并标记为 INITIALIZED
func (c *InstallStatus) SetAdminInit(username string) error {
	return install.MarkAdminInitialized(username)
}

// GetInstallLock 返回当前 install.lock（拷贝）
func (c *InstallStatus) GetInstallLock() *install.Lock {
	lr, err := install.Load()
	if err != nil || lr == nil {
		return nil
	}
	return lr
}

// CurrentVersion 返回 install.lock 记录的版本号；缺失时返回 unknown
func (c *InstallStatus) CurrentVersion() string {
	lr, err := install.Load()
	if err != nil || lr == nil {
		return "unknown"
	}
	if lr.Version == "" {
		return "unknown"
	}
	return lr.Version
}

// GetAdminUsername 返回 install.lock 中的超管账号
func (c *InstallStatus) GetAdminUsername() string {
	return install.GetAdminUsername()
}

// MarkAdminInitialized 标记 install.lock 已初始化
func (c *InstallStatus) MarkAdminInitialized() error {
	return install.MarkAdminInitializedStandalone()
}
