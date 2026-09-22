// Package install 提供 install.lock 文件读写（开源版精简实现）
//
// 背景：hivemtk 全面开源后不再有 License / 授权流程。
// install.lock 仅用于记录"本机安装"的最小状态：
//   - install_id     一次安装 = 一个唯一标识（UUID）
//   - install_time   安装时间（UTC RFC3339）
//   - admin_username 超级管理员账号（创建后写入）
//   - initialized    是否已创建超管（"true"/"false"）
//   - version        客户端版本号（用于统计）
//
// 不再包含：license_key / expire_at / company_name / contact_email 等授权相关字段。
package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Lock install.lock 文件结构（精简版）
type Lock struct {
	InstallID     string `json:"install_id"`
	InstallTime   string `json:"install_time"`
	AdminUsername string `json:"admin_username"`
	Initialized   bool   `json:"initialized"`
	Version       string `json:"version"`
}

// DefaultInstallLockPath 默认 install.lock 路径
const DefaultInstallLockPath = "./install.lock"

// GetInstallLockPath 解析实际 install.lock 路径
//
// 优先级：
//  1. 环境变量 INSTALL_LOCK_PATH
//  2. ./install.lock
func GetInstallLockPath() string {
	if p := os.Getenv("INSTALL_LOCK_PATH"); p != "" {
		return p
	}
	return DefaultInstallLockPath
}

var (
	mu       sync.RWMutex
	memoLR   *Lock
	memoPath string
	memoExp  time.Time

	adminProbeMu sync.RWMutex
	adminProbe   func(ctx context.Context) (string, error)
)

const memoTTL = 2 * time.Second

// SetAdminProbe 注入数据库超管探测函数（main 启动时调用）。
// fn 返回首个超管用户名；若库中无超管返回 ("", nil) 或 gorm.ErrRecordNotFound。
func SetAdminProbe(fn func(ctx context.Context) (string, error)) {
	adminProbeMu.Lock()
	defer adminProbeMu.Unlock()
	adminProbe = fn
}

// Load 读取 install.lock（带 2 秒内存缓存，文件 IO 在 InitGuard 高频路径上避免抖动）
//
// 缓存按生效路径记账：INSTALL_LOCK_PATH 是每次调用现读的，缓存若不跟着走，
// 路径一换就会把上一份 lock 端过来。
func Load() (*Lock, error) {
	path := GetInstallLockPath()
	mu.RLock()
	if memoLR != nil && memoPath == path && time.Now().Before(memoExp) {
		lr := *memoLR
		mu.RUnlock()
		return &lr, nil
	}
	mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lr Lock
	if err := json.Unmarshal(data, &lr); err != nil {
		return nil, err
	}
	mu.Lock()
	memoLR = &lr
	memoPath = path
	memoExp = time.Now().Add(memoTTL)
	mu.Unlock()
	out := lr
	return &out, nil
}

// Save 写回 install.lock（同时刷新内存缓存）
func Save(lr *Lock) error {
	if lr == nil {
		return errors.New("install lock is nil")
	}
	if lr.InstallID == "" {
		lr.InstallID = newInstallID()
	}
	if lr.InstallTime == "" {
		lr.InstallTime = time.Now().UTC().Format(time.RFC3339)
	}
	path := GetInstallLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lr, "", "  ")
	if err != nil {
		return err
	}
	// 先写临时文件再 rename：这个文件是 InitGuard 每个请求都要读的，
	// 直接覆写一旦撞上进程被杀／盘满，留下的半截 JSON 会让安装态永久判不出来。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	cp := *lr
	mu.Lock()
	memoLR = &cp
	memoPath = path
	memoExp = time.Now().Add(memoTTL)
	mu.Unlock()
	return nil
}

// 损坏文件的正确处置只有一种：还读得出来的键保下来，其余重建。
// 敢用正则捞是因为 MarshalIndent 按结构体字段序写，install_id / install_time 永远排在最前，
// 而最常见的损坏是"写到一半被杀"——尾部的键丢了，开头的它还在。
var (
	salvageInstallID   = regexp.MustCompile(`"install_id"\s*:\s*"([^"]{8,64})"`)
	salvageInstallTime = regexp.MustCompile(`"install_time"\s*:\s*"([^"]{10,40})"`)
	salvageAdminUser   = regexp.MustCompile(`"admin_username"\s*:\s*"([^"]{1,64})"`)
	salvageVersion     = regexp.MustCompile(`"version"\s*:\s*"([^"]{1,64})"`)
)

// loadForWrite 是给"准备改写这个文件"的调用方用的读法。
// 与 Load 的区别：解析失败时不把错误抛回去——抛回去＝调用方一次都不写，
// 于是损坏的 install.lock 永远修不好，每个请求都得重读一遍、再查一遍库。
func loadForWrite() *Lock {
	data, err := os.ReadFile(GetInstallLockPath())
	if err != nil {
		return nil
	}
	var lr Lock
	if json.Unmarshal(data, &lr) == nil {
		return &lr
	}
	body := string(data)
	return &Lock{
		InstallID:     salvage(salvageInstallID, body),
		InstallTime:   salvage(salvageInstallTime, body),
		AdminUsername: salvage(salvageAdminUser, body),
		Version:       salvage(salvageVersion, body),
	}
}

func salvage(re *regexp.Regexp, body string) string {
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// markAdminInitialized 落盘"超管已创建"，并告知本次是否铸了一枚全新的 install_id。
//
// minted 为 true 且调用方是"库里已有超管"那条回填腿，就意味着这台机器的身份换了：
// 平台侧的商户行纯按 install_id 建，重铸＝一个全新商户、旧历史再也接不上。
// 之所以单独立一个返回值而不是让调用方自己比 install_id：install_id 是在 Save 里
// 就地补铸的，调用方手里那份 lr 在 Save 之前根本没有这个信息。
func markAdminInitialized(username string) (minted bool, err error) {
	lr := loadForWrite()
	if lr == nil {
		lr = &Lock{}
	}
	hadID := lr.InstallID != ""
	lr.AdminUsername = strings.TrimSpace(username)
	lr.Initialized = true
	if err := Save(lr); err != nil {
		return false, err
	}
	// Save 成功后 InstallID 必非空（空值会在其中补铸），所以 hadID 就是唯一的判据。
	return !hadID, nil
}

// MarkAdminInitialized 标记"超管已创建"（开源版：创建超管即视为初始化完成）
func MarkAdminInitialized(username string) error {
	_, err := markAdminInitialized(username)
	return err
}

// MarkAdminInitializedStandalone 兼容旧调用方：标记 install.lock 已初始化
//
// 等价于 MarkAdminInitialized(currentAdminUsername)，
// 仅在 install.lock 已存在且 AdminUsername 非空时使用；
// 否则用空字符串调用，由 install.lock 自动写入新 AdminUsername。
func MarkAdminInitializedStandalone() error {
	lr := loadForWrite()
	username := ""
	if lr != nil {
		username = lr.AdminUsername
	}
	return MarkAdminInitialized(username)
}

// Status 初始化状态 DTO（与前端约定保持兼容）
type Status struct {
	State       string `json:"state"`
	Initialized bool   `json:"initialized"`
	HasAdmin    bool   `json:"has_admin"`
	InstallID   string `json:"install_id,omitempty"`
	Version     string `json:"version,omitempty"`
	// Reminted 表示这次响应里的 install_id 是刚铸的：磁盘上原先没有可用身份，
	// 而库里已经装着超管账号——即这台机器的安装身份丢了又重建。
	// 不参与序列化（`/api/system/init-status` 是公开可读接口，字段集不随内部告警改动），
	// 只给进程内的告警腿用（见 internal/platform 的心跳上报）。
	Reminted bool `json:"-"`
}

// GetStatus 返回当前初始化状态
//
// 判定真相源优先级（根治"重启后要求重新初始化"）：
//  1. install.lock 文件：initialized==true 且 admin_username 非空 → INITIALIZED（最快路径）。
//  2. 数据库兜底：文件缺失/**损坏**/未初始化时，若 DB 中已存在超管账号，
//     仍判定为 INITIALIZED，并回填 install.lock，使后续请求直接走文件缓存。
//     这样即便 install.lock 因卷异常/误删/写到一半被杀而丢失或坏掉，
//     只要库中有超管就不会要求重新初始化，也不会每请求都回查一次库。
//
// 回填有代价：旧身份没保下来时本次会铸一枚新 install_id（Status.Reminted），
// 平台侧会把它记成一个新装商户，所以这条必须能被调用方看见。
func GetStatus() *Status {
	lr, err := Load()
	minted := false
	if err != nil || lr == nil {
		name := probeDBAdmin()
		if name == "" {
			return &Status{
				State:       "NOT_INSTALLED",
				Initialized: false,
				HasAdmin:    false,
			}
		}
		// 回填成功就重新读一次：install_id 与 version 得跟着这次响应走，
		// 否则前端与心跳拿到的是空身份，而磁盘上明明已经有一份好的了。
		if m, merr := markAdminInitialized(name); merr == nil {
			minted = m
			if healed, healErr := Load(); healErr == nil && healed != nil {
				lr = healed
			}
		}
		if lr == nil {
			lr = &Lock{AdminUsername: name, Initialized: true}
		}
	}
	st := &Status{
		InstallID: lr.InstallID,
		Version:   lr.Version,
		HasAdmin:  lr.AdminUsername != "",
		Reminted:  minted,
	}
	if lr.Initialized && lr.AdminUsername != "" {
		st.State = "INITIALIZED"
		st.Initialized = true
	} else if lr.AdminUsername != "" {
		if name := probeDBAdmin(); name != "" {
			if m, merr := markAdminInitialized(name); merr == nil {
				st.Reminted = m
			}
			st.State = "INITIALIZED"
			st.Initialized = true
			st.HasAdmin = true
		} else {
			st.State = "HAS_ADMIN"
		}
	} else {
		if name := probeDBAdmin(); name != "" {
			if m, merr := markAdminInitialized(name); merr == nil {
				st.Reminted = m
			}
			st.State = "INITIALIZED"
			st.Initialized = true
			st.HasAdmin = true
		} else {
			st.State = "NOT_INSTALLED"
		}
	}
	return st
}

func probeDBAdmin() string {
	adminProbeMu.RLock()
	fn := adminProbe
	adminProbeMu.RUnlock()
	if fn == nil {
		return ""
	}
	name, err := fn(context.Background())
	if err != nil || name == "" {
		return ""
	}
	return strings.TrimSpace(name)
}

// GetAdminUsername 返回 install.lock 中记录的超管账号
func GetAdminUsername() string {
	lr, err := Load()
	if err != nil || lr == nil {
		return ""
	}
	return lr.AdminUsername
}

func newInstallID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := time.Now().UnixNano()
		return "ins-" + hex.EncodeToString([]byte{
			byte(ts >> 56), byte(ts >> 48), byte(ts >> 40), byte(ts >> 32),
			byte(ts >> 24), byte(ts >> 16), byte(ts >> 8), byte(ts),
		})
	}
	return "ins-" + hex.EncodeToString(b[:])
}
