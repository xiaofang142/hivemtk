package storage

// 文件访问 URL 的签名（SaaS 审计 A1 / A12，2026-10-23）。
//
// 要同时解决的两件事，它们是同一个根因的两面：
//
//	A12 —— URL 里别放可枚举的租户段。原先 on 模式的上传 URL 是
//	       `/files/tenant_5/materials/2026/10/<uuid>.pdf`：租户 id 明文可见，
//	       看一眼 URL 就知道"这家有几个租户、我是第几个"，导出/日志/转发都会带出去。
//	A1  —— 无凭证访问 `tenant_*` 路径必须交出**可验证凭证**，否则 404。原先
//	       `tenantPathAllowed` 对"ctx 无租户"是**放行**（访客 <img>、分享页），
//	       于是隔离只剩"UUID 不可枚举"一层 —— 而 URL 本身会外流（导出、日志、转发）。
//
// 一处修法：把"租户归属 + 过期时间"塞进一个**认证加密**的 token，放进查询参数，
// URL 路径只留相对路径（与 off 模式同形）：
//
//	off: /files/materials/2026/10/<uuid>.pdf                     （逐字节等价历史）
//	on : /files/materials/2026/10/<uuid>.pdf?s=<token>           （无租户段，A12）
//
// token = base64url(nonce ‖ AES-GCM({p: 相对路径, t: 租户, e: 过期戳}))，密钥由部署秘密
// 派生。它既是"凭证"（A1：验不过即 404），也是"租户归属的载体"（服务端据此决定去
// `tenant_{id}/` 那棵树里取文件）。加密而不是"明文 + HMAC"：base64 不是加密，明文
// payload 里写 `t: 5` 等于把租户 id 换个位置暴露，A12 就白改了。
//
// 时效：`FILE_URL_TTL`（Go duration 串，如 `168h`）可配；**默认不过期**（`e=0`）。
// 默认不过期是刻意的取舍：本地存储的 URL 会被**持久化**（materials.url、
// message_hub.media_url、卡片 image_url…），短时 token 会在用户翻历史记录时自己到期，
// 变成"图片某天开始全裂"。需要短时的部署配 `FILE_URL_TTL` 即可，过期校验在活体代码里
// （见 VerifyFileURL 与 local_fileurl_test.go 的过期用例），不是文档承诺。
//
// 撤销：轮换 `FILE_URL_SECRET`（或它回落到的那把部署密钥）即全量失效。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// fileURLVersion 既做密钥派生盐，也做 AEAD 的附加认证数据：
// 换版本 ⇒ 老 token 一律验不过（比"悄悄兼容"安全）。
const fileURLVersion = "hivemtk/file-url/v1"

// testFileURLSecret 只在 go test 进程里兜底（与 utils.testJWTSecret 同一取向：
// 测试不该依赖部署密钥，生产不该有硬编码密钥）。
const testFileURLSecret = "test-file-url-secret-do-not-use-in-prod-32+chars"

// ErrFileURLSignerUnavailable 没有可用密钥 ⇒ 无法签发/验签。
var ErrFileURLSignerUnavailable = errors.New("storage: 文件 URL 签名器未装配（FILE_URL_SECRET/JWT 秘密均未配置）")

// ErrFileURLExpired 签名有效但已过期。
var ErrFileURLExpired = errors.New("storage: 文件 URL 已过期")

// ErrFileURLInvalid 签名缺失/被篡改/格式不对。
var ErrFileURLInvalid = errors.New("storage: 文件 URL 签名无效")

// fileURLPayload token 的明文部分。
type fileURLPayload struct {
	// P 相对路径（不含 tenant_{id}/ 段）
	P string `json:"p"`
	// T 租户 id
	T uint `json:"t"`
	// E 过期时间戳（Unix 秒）；0 = 不过期
	E int64 `json:"e"`
}

type fileURLSigner struct {
	aead cipher.AEAD
	ttl  time.Duration
}

var (
	fileURLMu       sync.RWMutex
	fileURLActive   *fileURLSigner
	fileURLInitDone bool
	fileURLWarned   atomic.Bool
)

// ConfigureFileURLSigner 显式装配签名器（装配期 / 测试）。
//
// secret 为空 ⇒ 直接报错（不静默退化：没有密钥就签不出凭证，on 模式下公开外链会全断，
// 那是必须让人看见的故障，不是可以"自动降级"的选项）。ttl<=0 ⇒ 不过期。
func ConfigureFileURLSigner(secret string, ttl time.Duration) error {
	s, err := newFileURLSigner(secret, ttl)
	if err != nil {
		return err
	}
	fileURLMu.Lock()
	fileURLActive = s
	fileURLInitDone = true
	fileURLMu.Unlock()
	return nil
}

// ResetFileURLSignerForTest / DisableFileURLSignerForTest 仅测试用（跨包用例要用）：
//   - Reset：清回"未装配"，下一次调用按环境惰性装配
//   - Disable：模拟"生产缺密钥"（active=nil 且不会被惰性装配填回来）
//
// 导出而不是让别的包自己摸全局状态：签名器是进程级状态，用例装完必须能装回去。
func ResetFileURLSignerForTest() { resetFileURLSignerForTest() }

func DisableFileURLSignerForTest() {
	fileURLMu.Lock()
	fileURLActive = nil
	fileURLInitDone = true
	fileURLMu.Unlock()
	fileURLWarned.Store(false)
}

// resetFileURLSignerForTest 仅测试用：清掉装配态，让下一次调用重新按环境装配。
// （同时清 fileURLWarned，否则"响亮报错一次"会在同进程的后续用例里被吞掉。）
func resetFileURLSignerForTest() {
	fileURLMu.Lock()
	fileURLActive = nil
	fileURLInitDone = false
	fileURLMu.Unlock()
	fileURLWarned.Store(false)
}

func newFileURLSigner(secret string, ttl time.Duration) (*fileURLSigner, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, ErrFileURLSignerUnavailable
	}
	sum := sha256.Sum256([]byte(fileURLVersion + "\x00" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("storage: 构造文件 URL 密钥失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("storage: 构造文件 URL AEAD 失败: %w", err)
	}
	return &fileURLSigner{aead: aead, ttl: ttl}, nil
}

// currentFileURLSigner 取签名器；首次调用时按环境惰性装配。
//
// 惰性装配的原因：签发点是 `LocalDriver.PublicURL`（存储层），而入口有 api / seed /
// worker 等多个二进制。若改成"每个入口显式装配"，漏掉一个入口就是一条**静默**的
// 明文 URL 泄露路径（A1 的形状）。这里一次装配，所有入口同源。
func currentFileURLSigner() *fileURLSigner {
	fileURLMu.RLock()
	s := fileURLActive
	done := fileURLInitDone
	fileURLMu.RUnlock()
	if done {
		return s
	}
	fileURLMu.Lock()
	defer fileURLMu.Unlock()
	if fileURLInitDone {
		return fileURLActive
	}
	fileURLInitDone = true
	secret := fileURLSecretFromEnv()
	if secret == "" {
		// 保持 nil：on 模式下 PublicURL 会响亮报错并退回明文路径（此时无凭证访问
		// tenant_* 一律 404 —— 降级方向是"断链"而不是"泄漏"，见 files_guard.go）。
		fileURLActive = nil
		return nil
	}
	s, err := newFileURLSigner(secret, fileURLTTL())
	if err != nil {
		logger.Errorf("[STORAGE] 文件 URL 签名器装配失败（文件 URL 将退回明文路径）: %v", err)
		fileURLActive = nil
		return nil
	}
	fileURLActive = s
	return s
}

// fileURLSecretFromEnv 部署秘密优先级：FILE_URL_SECRET → USER_JWT_SECRET → JWT_SECRET。
//
// 回落到 JWT 密钥是为了"已有部署不必新增配置就能拿到签名"：三者都是部署级秘密，
// 派生时又叠了 fileURLVersion 做域分离，不会和 JWT 自己的签名混用。
func fileURLSecretFromEnv() string {
	for _, k := range []string{"FILE_URL_SECRET", "USER_JWT_SECRET", "JWT_SECRET"} {
		if v := strings.TrimSpace(os.Getenv(k)); len(v) >= 32 {
			return v
		}
	}
	if isTestProcess() {
		return testFileURLSecret
	}
	return ""
}

// fileURLTTLProvider 是签名 URL 时效的取值口（Go duration 秒数；0 = 不过期）。
// SaaS 配置面改造（B12，2026-11-17）：FILE_URL_TTL 环境变量退役，改由参数中心
// misc.file_url_ttl（秒）经 provider 注入（装配点 internal/app）。默认 0 不过期：
// 本地存储的 URL 会被持久化（materials.url、卡片 image_url…），短时 token 会在
// 用户翻历史记录时自己到期变成"图片某天开始全裂"——需要短时的部署在参数中心配。
var fileURLTTLProvider = func() int { return 0 }

// SetFileURLTTLProvider 注入时效取值口。传 nil 是空操作而不是复位。
func SetFileURLTTLProvider(fn func() int) {
	if fn != nil {
		fileURLTTLProvider = fn
	}
}

func fileURLTTL() time.Duration {
	if n := fileURLTTLProvider(); n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// isTestProcess 与 utils.isTestProcess 同一判据（两边都判一次是为了不让存储层依赖 utils）。
func isTestProcess() bool {
	return strings.HasSuffix(os.Args[0], ".test") || strings.HasSuffix(os.Args[0], ".test.exe")
}

// splitTenantPath 拆分存储路径的租户段：`tenant_{id}/rest`。
//
// 整段匹配（不是前缀匹配）：`tenant_51/...` 不能当成 `tenant_5` 的后代 —— 与
// router.tenantPathAllowed 的同一条判据同源，两边各写一遍是因为它们分属两包，
// 但判据必须一致（files_guard 的边界用例两边都跑）。
func splitTenantPath(storagePath string) (uint, string, bool) {
	p := strings.TrimPrefix(strings.ReplaceAll(storagePath, "\\", "/"), "/")
	seg, tail, found := strings.Cut(p, "/")
	if !found || !strings.HasPrefix(seg, "tenant_") || tail == "" {
		return 0, "", false
	}
	id, err := strconv.ParseUint(seg[len("tenant_"):], 10, 32)
	if err != nil || id == 0 {
		return 0, "", false
	}
	return uint(id), tail, true
}

// TenantDir 存储布局里租户目录的名字（`tenant_{id}`）。落盘/取盘两侧共用，
// 避免"一边拼 tenant_%d、一边拼 tenant-%d"这种只有上线才发现的错位。
func TenantDir(tenantID uint) string { return fmt.Sprintf("tenant_%d", tenantID) }

// TenantDirFromFileURL 从一条（可能带 `?s=` 的）文件 URL 里解出它所属的租户目录。
//
// 用途只有一处：邮件附件出口要把 URL 换回磁盘路径，而 on 模式下附件分域落盘在
// `{baseDir}/{tenant_dir}/attachments/…`（SaaS 审计 A18 —— 之前两个消费者都不知道这件事，
// 附件 100% 静默丢弃）。租户归属不在路径里（A12 之后 URL 不含 `tenant_{id}` 段），
// 只在签名 token 里，所以必须验签取租户，不能靠"平根 + 同名文件"去猜。
//
// 返回 "" 的四种情形（调用方随即退回平根，Lstat 不中 ⇒ 该条判"挂不上"，不挂错文件）：
// off 模式、值里没有 `s` 参数、签名验不过（篡改/过期/换密钥）、签名器未装配。
func TenantDirFromFileURL(value, relPath string) string {
	if !tenant.IsMultiTenant() {
		return ""
	}
	sig := fileURLSignature(value)
	if sig == "" {
		return ""
	}
	tenantID, _, err := VerifyFileURL(relPath, sig, time.Now())
	if err != nil {
		return ""
	}
	return TenantDir(tenantID)
}

// fileURLSignature 从一条 URL 里取 `s` 查询参数（签名）；没有则空串。
func fileURLSignature(value string) string {
	i := strings.IndexByte(value, '?')
	if i < 0 {
		return ""
	}
	query := value[i+1:]
	if j := strings.IndexByte(query, '#'); j >= 0 {
		query = query[:j]
	}
	for _, kv := range strings.Split(query, "&") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k != "s" {
			continue
		}
		if unescaped, err := url.QueryUnescape(v); err == nil {
			return unescaped
		}
		return v
	}
	return ""
}

// SignStoredPath 把存储路径拆成"不含租户段的相对路径 + 签名"。
//
// 返回 ok=false 的情形（都不改变行为，只是不签名）：
//   - off 模式：路径里没有 `tenant_{id}/` 段——2026-11-17 审计 P3-2 起改为同样签发
//     （默认租户归属）；此前离线单机 URL 与历史逐字节一致、匿名可读
//   - on 模式下路径里没有 `tenant_{id}/` 段：系统级上传 / 存量根域文件（本来就不分租户）
//   - 签名器未装配：调用方 PublicURL 会退回明文并已响亮报错
func SignStoredPath(storagePath string) (relPath string, sig string, ok bool) {
	s := currentFileURLSigner()
	if s == nil {
		if tenant.IsMultiTenant() && fileURLWarned.CompareAndSwap(false, true) {
			logger.Errorf("[STORAGE] 多租户模式下文件 URL 无法签名（缺 FILE_URL_SECRET/JWT 密钥）："+
				"公开外链将退回明文路径，无凭证访问 tenant_* 一律 404。URL=%q", storagePath)
		}
		return "", "", false
	}
	if !tenant.IsMultiTenant() {
		// off 模式：落盘无租户段，整条相对路径按默认租户签发（审计 P3-2）。
		rest := strings.TrimPrefix(strings.ReplaceAll(storagePath, "\\", "/"), "/")
		if rest == "" {
			return "", "", false
		}
		token, err := s.sign(rest, tenant.DefaultTenantID, time.Now())
		if err != nil {
			logger.Errorf("[STORAGE] off 模式文件 URL 签名失败（退回明文路径）: %v", err)
			return "", "", false
		}
		return rest, token, true
	}
	tid, rest, matched := splitTenantPath(storagePath)
	if !matched || rest == "" {
		return "", "", false
	}
	token, err := s.sign(rest, tid, time.Now())
	if err != nil {
		logger.Errorf("[STORAGE] 文件 URL 签名失败（退回明文路径）: %v", err)
		return "", "", false
	}
	return rest, token, true
}

// OffModeFilesRequireSign off 模式 /files 是否拒绝无签名请求
// （FILES_OFF_MODE_REQUIRE_SIGN=1）。默认 0：存量库里的无签名 URL 照常放行，
// 但每次匿名访问留审计日志；确认存量已消化后打开即全量收口。
func OffModeFilesRequireSign() bool {
	return os.Getenv("FILES_OFF_MODE_REQUIRE_SIGN") == "1"
}

func (s *fileURLSigner) sign(relPath string, tenantID uint, now time.Time) (string, error) {
	payload := fileURLPayload{P: relPath, T: tenantID}
	if s.ttl > 0 {
		payload.E = now.Add(s.ttl).Unix()
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := s.aead.Seal(nil, nonce, plain, []byte(fileURLVersion))
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// VerifyFileURL 校验 URL 上的签名，返回它指向的租户与相对路径。
//
// 三种失败：签名无效（篡改/伪造/格式错）、已过期、签名器未装配 —— 调用方一律 404，
// 不做"无效就放行"的分支（那正是 A1 要清掉的形状）。
func VerifyFileURL(relPath, sig string, now time.Time) (uint, string, error) {
	relPath = strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "/")
	if sig == "" {
		return 0, "", ErrFileURLInvalid
	}
	s := currentFileURLSigner()
	if s == nil {
		return 0, "", ErrFileURLSignerUnavailable
	}
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(raw) <= s.aead.NonceSize() {
		return 0, "", ErrFileURLInvalid
	}
	nonce, sealed := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, sealed, []byte(fileURLVersion))
	if err != nil {
		return 0, "", ErrFileURLInvalid
	}
	var payload fileURLPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return 0, "", ErrFileURLInvalid
	}
	// 过期判据用 "!Before"（now >= 过期时刻 即过期）而不是 ">"：过期戳是 Unix 秒，
	// 用 ">" 会让"签发后同一秒内"的边界状态依赖签发时刻落在秒内的哪一段，
	// 表现为"有时过期、有时不过期"的抽奖式用例（本仓最忌讳的假绿形态）。
	if payload.E != 0 && !now.Before(time.Unix(payload.E, 0)) {
		return 0, "", ErrFileURLExpired
	}
	// 签名覆盖的是"某个路径"，请求的必须是同一个：不比对就等于签名只证明
	// "我签过某个东西"，篡改路径段仍能通过。
	if payload.P != relPath || payload.T == 0 {
		return 0, "", ErrFileURLInvalid
	}
	return payload.T, payload.P, nil
}
