package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrPlatformNotConfigured 平台客户端未配置哨兵错误（轮询型端点静默降级判定用）
var ErrPlatformNotConfigured = errors.New("平台配置未初始化")

// DegradeReason 把降级原因分类给调用方：
// 「本机根本没接平台」(not_configured) 是私域独立部署的常态，「接了但挂了」(unreachable) 才是故障。
// 二者都只有一个 error 时，消费方要么把常态刷成 Error 日志，要么把故障静默掉，只能二选一错。
func DegradeReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrPlatformNotConfigured):
		return "not_configured"
	default:
		return "unreachable"
	}
}

type Client struct {
	merchantSecret string
	merchantKey    string
	httpClient     *http.Client
}

func NewPlatformClient(merchantKey string) *Client {
	c := &Client{
		merchantKey: merchantKey,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}

	if err := c.loadMerchantSecret(); err != nil {
		logger.Warn(err.Error())
	}
	return c
}

// SetMerchantSecret 注入注册响应下发的每商户独立 HMAC 密钥（契约 §1 落地）。
// 非空时优先于全局 MERCHANT_API_SECRET / PlatformCfg.Secret 参与签名。
//
// 注意它是**实例**状态，刻意不做成包级全局：签名密钥一旦成为进程级可变全局，
// 「谁在什么时候覆盖了它」就不可追，且单测之间会互相污染 —— 一条用例注入的值会让
// 另一条"三处皆空应报错"的用例变绿（本仓实测踩到过）。跨消费者的共享通道是**文件**
// （注册时 saveMerchantSecret 落盘，见 RegisterMerchant），那是显式且有权限位的。
func (c *Client) SetMerchantSecret(secret string) {
	if secret != "" {
		c.merchantSecret = secret
	}
}

// GetMerchantSecret 返回本商户的 HMAC 签名密钥 —— **唯一事实源**（SaaS 审计 A19）。
//
// 解析链（顺序即优先级，与原 `Client.sign` 的历史行为逐字一致，只是**收成一处**）：
//
//  1. 文件 <MERCHANT_STATE_DIR|config>/.merchant_api_secret（平台开户时下发、注册时落盘）
//  2. env MERCHANT_API_SECRET（全局回退，双读兼容期）
//  3. config.PlatformCfg.Secret（同上）
//
// 为什么必须收成一个函数：A19 新增了第二条**签名**通道（/llm-api/v1），而此前这套解析
// 散在 `Client.sign`、`Client.loadMerchantSecret`、`merchantSecretFilePath` 三处，
// 每个新消费者都要照抄一遍 —— 抄错一格（比如漏了文件那一档）的表现是
// "某些部署的 LLM 代理恒 401"，且与"平台没升级"完全无法区分。
func GetMerchantSecret() string {
	if b, err := os.ReadFile(merchantSecretFilePath()); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(os.Getenv("MERCHANT_API_SECRET")); v != "" {
		return v
	}
	if config.PlatformCfg != nil {
		return strings.TrimSpace(config.PlatformCfg.Secret)
	}
	return ""
}

// maxRespBodyBytes 出站响应体封顶（MA-06）：Timeout 限时不限长——对端慢慢吐
// GB 级响应会吃光进程内存。同仓口径：llm_proxy 32MB / openapi 64KB / api_logger
// 8-16KB，平台客户端此前是唯一无上限出站读法。
const maxRespBodyBytes = int64(32 << 20)

// readPlatformBody 读平台响应体，封顶 32MB。
// 读 cap+1 字节：超出即报错——**不静默截断**（截断会把坏数据当好数据用，
// 与 llm_proxy 同口径）。
func readPlatformBody(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxRespBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxRespBodyBytes {
		return nil, fmt.Errorf("响应体超 %dMB 上限，已停止读取", maxRespBodyBytes>>20)
	}
	return b, nil
}

func (c *Client) sign(method, path string, body []byte) (string, string, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	pathNoQuery := path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		pathNoQuery = path[:i]
	}
	payload := method + "\n" + pathNoQuery + "\n" + timestamp + "\n" + string(body)

	// 实例值先手（它可能来自构造期注入的显式值），其余一律交给唯一事实源。
	secret := c.merchantSecret
	if secret == "" {
		secret = GetMerchantSecret()
	}
	if secret == "" {
		return "", "", fmt.Errorf("MERCHANT_API_SECRET 未配置: 请设置环境变量或 PlatformCfg.Secret")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return sig, timestamp, nil
}

// Do 公开的 HTTP 请求方法，供 controller 层代理调用平台 API 使用。
// 失败时返回 *PlatformError，调用方可按状态码/业务 code 结构化分支处理。
func (c *Client) Do(method, path string, reqData, respData any) error {
	return c.doRetry(context.Background(), method, path, reqData, respData, false)
}

// DoContext 与 Do 相同，但尊重调用方 ctx（取消/超时传播到 HTTP 请求）。
//
// 给「用户请求路径上的尽力回源」用：平台挂掉时页面加载最多等调用方给的时限，
// 而不是被客户端固定超时拖住。
func (c *Client) DoContext(ctx context.Context, method, path string, reqData, respData any) error {
	return c.doRetry(ctx, method, path, reqData, respData, false)
}

func (c *Client) do(method, path string, reqData, respData any) error {
	return c.doRetry(context.Background(), method, path, reqData, respData, false)
}

func (c *Client) doRetry(ctx context.Context, method, path string, reqData, respData any, retried bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.PlatformCfg == nil {
		err := fmt.Errorf("%w: 商户上报请求未发出", ErrPlatformNotConfigured)
		logger.Error(err, "商户上报请求失败")
		return err
	}
	url := config.PlatformURL() + path
	var body []byte
	if reqData != nil {
		var err error
		body, err = json.Marshal(reqData)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %w", err)
		}
	}

	reqLog := fmt.Sprintf("商户上报请求: %s %s", method, url)
	if len(body) > 0 {
		reqLog += fmt.Sprintf(" 请求数据: %s", string(body))
	}
	logger.Info(reqLog)

	req, err := newPlatformRequest(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("构造平台请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// JWT 代理通道已删（方案A，2026-11-11 L-1）：不再有 /platform/ 前缀的
	// 管理员 JWT 分支，所有出站路径统一走商户签名。
	sig, timestamp, err := c.sign(method, path, body)
	if err != nil {
		logger.Error(err, "商户签名失败")
		return err
	}
	req.Header.Set("X-Merchant-Key", c.merchantKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", sig)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		logger.Error(err, fmt.Sprintf("商户上报请求失败: %s %s, 耗时: %v", method, url, duration))
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized && !retried {
		// JWT 代理通道已删（方案A，2026-11-11 L-1）：不再有 token 缓存可清，
		// 只保留「401 重试一次」的通用自愈（TestClient_Do_401SelfHeal 钉住）。
		logger.Warn(fmt.Sprintf("平台返回 401，重试一次: %s %s", method, url))
		return c.doRetry(ctx, method, path, reqData, respData, true)
	}

	if resp.StatusCode != http.StatusOK {
		rawBody, readErr := readPlatformBody(resp.Body)
		bodyStr := ""
		if readErr == nil {
			bodyStr = string(rawBody)
		}
		var baseResp BaseResp
		if bodyStr != "" {
			_ = json.Unmarshal([]byte(bodyStr), &baseResp)
		}
		// 契约版本裁决（SaaS 审计 A4）：426 记进管理端可见的状态。
		// 放在日志之前/之后都行，但必须在 return 之前 —— 早返回的写法最容易被后来的
		// 改动插进一条 return 而把记账漏掉。
		noteContractVerdict(path, resp.StatusCode, rawBody)
		// 409 是"资源已存在"这类可收敛的业务状态（商户注册每次启动都会命中），
		// 按 error 打会让正常幂等路径长期占据错误日志；是否算失败由调用方按契约决定。
		if resp.StatusCode == http.StatusConflict {
			logger.Warn(fmt.Sprintf("平台返回 409（状态冲突，交由调用方判定）: %s %s, 耗时: %v, 响应: %s", method, url, duration, bodyStr))
		} else {
			logger.Error(fmt.Errorf("平台接口返回 %d", resp.StatusCode),
				fmt.Sprintf("商户上报请求失败: %s %s, 状态码: %d, 耗时: %v, 响应: %s", method, url, resp.StatusCode, duration, bodyStr))
		}
		return &PlatformError{StatusCode: resp.StatusCode, RawBody: bodyStr, Resp: &baseResp}
	}

	var respBody []byte
	respBody, err = readPlatformBody(resp.Body)
	if err != nil {
		logger.Error(err, "读取响应体失败")
		return err
	}
	// 200 ⇒ 闸放行了，说明契约匹配：清掉可能残留的 426 告警（仅对受闸路径生效）。
	noteContractVerdict(path, resp.StatusCode, respBody)
	if perr := envelopeRefusal(respBody); perr != nil {
		if perr.Resp.Code == http.StatusConflict {
			logger.Warn(fmt.Sprintf("平台业务码 409（状态冲突，交由调用方判定）: %s %s, 耗时: %v, 响应: %s", method, url, duration, perr.RawBody))
		} else {
			logger.Error(fmt.Errorf("平台业务码 %d", perr.Resp.Code),
				fmt.Sprintf("商户上报被拒: %s %s, 耗时: %v, 响应: %s", method, url, duration, perr.RawBody))
		}
		return perr
	}
	if respData != nil {
		logger.Info(fmt.Sprintf("商户上报请求成功: %s %s, 状态码: %d, 耗时: %v, 响应数据: %s", method, url, resp.StatusCode, duration, string(respBody)))
		return json.Unmarshal(respBody, respData)
	}
	return nil
}

// envelopeRefusal 认出"平台用 HTTP 200 承载的拒绝"。
//
// 平台侧 response.Success 与 response.Error 都写 HTTP 200，真值只在信封 code 里 ——
// 连 MerchantAuth 的 401/403、注册的 400/409 也一样。所以只看状态码会把
// "该邮箱已被注册""签名错误"读成成功，调用方连失败原因都拿不到。
//
// 两条边界：body 不是信封（非对象 / 无 code 键）时不凭空造失败，原样交回调用方解析；
// code=0 视为无业务码（既有口径，资产市场客户端同此），只有显式的非 200 码才算拒绝。
func envelopeRefusal(body []byte) *PlatformError {
	var probe struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Code == nil {
		return nil
	}
	switch *probe.Code {
	case 0, http.StatusOK:
		return nil
	}
	return &PlatformError{
		StatusCode: http.StatusOK,
		RawBody:    string(body),
		Resp:       &BaseResp{Code: *probe.Code, Msg: probe.Msg},
	}
}

// isPlatformConflict 平台 409：HTTP 状态码或信封业务码任一处为 409 都算
// （平台两种写法并存：直接回 HTTP 409，或 HTTP 200 + 信封 code=409）。
func isPlatformConflict(err error) bool {
	var perr *PlatformError
	if !errors.As(err, &perr) || perr == nil {
		return false
	}
	return perr.StatusCode == http.StatusConflict || (perr.Resp != nil && perr.Resp.Code == http.StatusConflict)
}

func platformErrMsg(err error) string {
	var perr *PlatformError
	if errors.As(err, &perr) && perr != nil {
		return perr.Msg()
	}
	return err.Error()
}

func (c *Client) RegisterMerchant(req RegisterMerchantReq) error {
	logger.Info(fmt.Sprintf("开始商户注册，请求数据: %+v", req))
	var resp BaseResp
	err := c.do("POST", "/merchant-api/merchant/register", req, &resp)
	if err != nil {
		// 409 = 本商户 key 已在平台注册过（重启/重复部署的常态）。注册是幂等的：
		// 已有身份与密钥不受影响，按成功返回，避免每次启动都打一条注册失败。
		if isPlatformConflict(err) {
			logger.Info(fmt.Sprintf("商户已注册（409 幂等，视为成功）: %s", platformErrMsg(err)))
			return nil
		}
		logger.Error(err, "商户注册失败")
		return err
	}

	if len(resp.Data) > 0 {
		var reg struct {
			Key    string `json:"key"`
			Secret string `json:"secret"`
		}
		if jerr := json.Unmarshal(resp.Data, &reg); jerr == nil && reg.Secret != "" {
			// 已有本地密钥文件时**绝不覆盖**：平台这条路 200 = 它按"key 不存在"
			// 分支新建了一个（随机 key 的）陌生商户并下发了那个商户的 secret ——
			// 收下它，本机后续所有签名对真正的商户行就是 401（本机
			// 实测踩中：启动协程先于平台 409 冲突分支，把 .merchant_api_secret
			// 换成了别家的，全站下单瞬断）。首次安装（文件不存在）才落盘。
			if _, serr := os.Stat(merchantSecretFilePath()); serr == nil {
				logger.Warn("平台注册返回新 secret，但本地密钥文件已存在，忽略（防身份漂移）")
			} else {
				c.SetMerchantSecret(reg.Secret)
				if werr := saveMerchantSecret(reg.Secret); werr != nil {
					logger.Warn(fmt.Sprintf("持久化 per-merchant secret 失败(本次进程内仍生效): %v", werr))
				}
				logger.Info("已接收并启用每商户独立签名密钥")
			}
		}
	}
	logger.Info("商户注册成功")
	return nil
}

func merchantSecretFilePath() string {
	return filepath.Join(merchantStateDir(), ".merchant_api_secret")
}

func saveMerchantSecret(secret string) error {
	p := merchantSecretFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(secret), 0o600)
}

// loadMerchantSecret 读回本商户独立的签名密钥。
// 文件不存在是首次安装的正常态，静默即可；其余读取失败意味着签名会用错密钥，
// 必须上报给调用方记日志——吞掉它会把故障推到平台侧的成片 401 上，事后无从定位。
func (c *Client) loadMerchantSecret() error {
	p := merchantSecretFilePath()
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		fromFile := strings.TrimSpace(string(b))
		c.SetMerchantSecret(fromFile)
		// 文件值与环境变量**不一致时打 info 说明**（不是故障告警）。
		//
		// 生产正态就是这样：文件 = 本商户独立的 API secret，env = 全局回退 secret，
		// 两者不同属**设计行为**（文件在解析链上优先，契约 §1）——只有当平台
		// 返回「签名错误 401」时才需要拿这条日志去核对文件是否残留旧值。
		//
		// 历史：环境变量配得完全正确，平台却持续返回
		// 「签名错误 401」—— 因为文件里留着旧值，而文件在解析链上
		// **优先于环境变量**，且这个优先级在代码里是隐式的（`if secret != ""` 挡在前）。
		// 两侧"看起来都配对了"，没有任何一处提示谁赢了 —— 这条日志把隐式优先级
		// 变成显式事实。措辞从 warning 降为 info：两边都有值且不同是正常部署形态，
		// 打 warning 会让运维把它当故障排查（它不是）。不改成"env 优先"是因为文件是
		// **每商户独立密钥**的落盘位置（契约 §1），语义上比全局 env 更具体 ——
		// 改优先级会反过来让文件失效。
		if fromFile != "" {
			if env := strings.TrimSpace(os.Getenv("MERCHANT_API_SECRET")); env != "" && env != fromFile {
				logger.Infof("[platform] 签名密钥来源提示：文件 %s 的值与环境变量 MERCHANT_API_SECRET 不同，"+
					"实际生效的是文件值（文件=本商户独立 API secret，优先于 env 全局 secret，属设计行为）。"+
					"仅当平台返回「签名错误」时才需核对本文件是否残留旧值。", p)
			}
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("读取 per-merchant secret(%s) 失败: %w", p, err)
	}
}

// CheckConnection 探测平台是否可达：只打平台真实存在的存活性端点 GET {APIURL}/health。
//
// 不走 doRetry —— 那条路会做商户签名，把一个连通性探针绑到鉴权链上等于
// 多造一类假故障（签名没配好会被判成"平台挂了"）。
// 也不要恢复成"查授权状态"：平台从未实现授权端点（开源版连 License 概念都移除了），
// 拿它做探针会让健康平台恒被判成 unreachable。
func (c *Client) CheckConnection() error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 未配置平台地址，连通性探测无从发出", ErrPlatformNotConfigured)
	}
	req, err := newPlatformRequest(context.Background(), http.MethodGet, config.PlatformURL()+"/health", nil)
	if err != nil {
		return fmt.Errorf("平台连通性探测请求构造失败: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("平台连通性探测失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &PlatformError{StatusCode: resp.StatusCode}
	}
	return nil
}

type BaseResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// PlatformError 结构化错误，携带 HTTP 状态码与平台响应体，便于调用方按 code/状态码
// 而非脆弱的字符串匹配(如 strings.Contains(err, "404"))进行分支处理。
type PlatformError struct {
	StatusCode int
	RawBody    string
	Resp       *BaseResp
}

func (e *PlatformError) Error() string {
	if e.Resp != nil && e.Resp.Msg != "" {
		return fmt.Sprintf("platform request failed: status=%d, code=%d, msg=%s", e.StatusCode, e.Resp.Code, e.Resp.Msg)
	}
	return fmt.Sprintf("platform request failed: status=%d, body=%s", e.StatusCode, e.RawBody)
}

// Msg 返回平台返回的业务错误信息（优先 Resp.Msg，其次原始响应体）。
func (e *PlatformError) Msg() string {
	if e.Resp != nil && e.Resp.Msg != "" {
		return e.Resp.Msg
	}
	if e.RawBody != "" {
		return e.RawBody
	}
	return e.Error()
}

type RegisterMerchantReq struct {
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	DeviceInfo   string `json:"device_info"`
}

// ReportInstallReq 安装信息上报请求（开源版：一个安装信息 = 一个商户）
type ReportInstallReq struct {
	InstallID         string `json:"install_id"`
	MerchantName      string `json:"merchant_name"`
	ContactEmail      string `json:"contact_email"`
	ContactPhone      string `json:"contact_phone"`
	ContactName       string `json:"contact_name"`
	DeviceFingerprint string `json:"device_fingerprint"`
	ClientIP          string `json:"client_ip"`
	Version           string `json:"version"`
}

// ReportInstall 上报安装信息到平台（开源版：创建/更新一个商户）
// 该接口为公开统计接口，不要求 JWT / 商户签名。
func (c *Client) ReportInstall(req *ReportInstallReq) error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 上报请求未发出", ErrPlatformNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化安装信息失败: %w", err)
	}
	url := strings.TrimRight(config.PlatformURL(), "/") + "/api/platform/install"
	httpReq, err := newPlatformRequest(context.Background(), "POST", url, body)
	if err != nil {
		return fmt.Errorf("构造安装上报请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("上报安装信息失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, readErr := readPlatformBody(resp.Body)
		noteContractVerdict("/api/platform/install", resp.StatusCode, raw)
		if readErr != nil {
			return fmt.Errorf("上报安装信息返回 %d (响应体读取失败或超限: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("上报安装信息返回 %d: %s", resp.StatusCode, string(raw))
	}
	// 200 ⇒ 闸放行了（含"运维开了兼容窗口"这种情形）：清掉可能残留的 426 告警。
	// 只在受闸路径上有清除资格 —— install 就在闸下，所以它有。
	noteContractVerdict("/api/platform/install", resp.StatusCode, nil)
	raw, err := readPlatformBody(resp.Body)
	if err != nil {
		return fmt.Errorf("读取安装响应失败: %w", err)
	}
	if perr := envelopeRefusal(raw); perr != nil {
		return fmt.Errorf("上报安装信息被拒: code=%d, msg=%s", perr.Resp.Code, perr.Resp.Msg)
	}
	logger.Info("上报安装信息成功")
	return nil
}

// ReportInstallDefault 使用全局配置创建客户端并上报安装信息
// 安装信息上报为公开统计接口，不要求商户签名，故使用空 merchantKey。
func ReportInstallDefault(req *ReportInstallReq) error {
	return NewPlatformClient("").ReportInstall(req)
}

// ReportHeartbeatReq 心跳上报请求（开源版：每 3 分钟上报一次，仅统计用）
type ReportHeartbeatReq struct {
	InstallID string `json:"install_id"`
	// MerchantKey 本部署的商户标识。
	//
	// 带上它的目的是**把平台端两套并行身份连起来**：merchants 按 merchant_key 记
	// 商业关系，platform_installs 按 install_id 记运维信息，此前两者互不关联
	// （写 merchants.install_id 的唯一路径 /api/platform/install 在用户端零调用）。
	//
	// 用心跳带而不是新增一条上报：心跳是**已经跑通**的通道，加一个字段
	// 比新开一条链路便宜得多，也不会引入新的失败面。
	MerchantKey       string          `json:"merchant_key"`
	Version           string          `json:"version"`
	HostInfo          json.RawMessage `json:"host_info"`
	Metrics           json.RawMessage `json:"metrics"`
	DeviceFingerprint string          `json:"device_fingerprint"`
	ClientIP          string          `json:"client_ip"`
	Timestamp         time.Time       `json:"timestamp"`
}

// ReportHeartbeat 上报心跳到平台（公开统计接口，不要求签名/JWT）
func (c *Client) ReportHeartbeat(req *ReportHeartbeatReq) error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 上报请求未发出", ErrPlatformNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化心跳数据失败: %w", err)
	}
	url := strings.TrimRight(config.PlatformURL(), "/") + "/api/platform/heartbeat"
	// L-15：平台心跳闸（L-3 方案B）对「非空 merchant_key 声明」要求 HMAC 签名
	//（X-Merchant-Key/X-Timestamp/X-Signature，payload = method+"\n"+path+"\n"+timestamp+"\n"+body，
	// 口径与 Client.sign / 平台 VerifyHeartbeatSignature 三方一致）。secret 缺失时
	// 降级为纯心跳（剥离关联声明）保活统计面——带关联硬发会被平台 401 信封拒。
	headerKey, headerTS, headerSig := "", "", ""
	if req.MerchantKey != "" {
		if sig, ts, serr := c.sign("POST", "/api/platform/heartbeat", body); serr != nil {
			logger.Warnf("心跳签名失败（%v）：本次降级为纯心跳（剥离 merchant_key 关联声明）。"+
				"配置 PlatformCfg.Secret 后恢复关联上报", serr)
			req.MerchantKey = ""
			if body, err = json.Marshal(req); err != nil {
				return fmt.Errorf("序列化降级心跳数据失败: %w", err)
			}
		} else {
			headerKey, headerTS, headerSig = req.MerchantKey, ts, sig
		}
	}
	httpReq, err := newPlatformRequest(context.Background(), "POST", url, body)
	if err != nil {
		return fmt.Errorf("构造心跳上报请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if headerSig != "" {
		httpReq.Header.Set("X-Merchant-Key", headerKey)
		httpReq.Header.Set("X-Timestamp", headerTS)
		httpReq.Header.Set("X-Signature", headerSig)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("上报心跳失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, readErr := readPlatformBody(resp.Body)
		noteContractVerdict("/api/platform/heartbeat", resp.StatusCode, raw)
		if readErr != nil {
			return fmt.Errorf("上报心跳返回 %d (响应体读取失败或超限: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("上报心跳返回 %d: %s", resp.StatusCode, string(raw))
	}
	// 200 ⇒ 闸放行了：清掉告警。这一条尤其要紧 —— 心跳是周期性调用的，
	// 若只在 /merchant-api 上清，那么"运维已开窗口、平台不再拒我们"这件事
	// 会一直显示成 426，管理员只能看到一个不再更新的旧告警。
	noteContractVerdict("/api/platform/heartbeat", resp.StatusCode, nil)
	raw, err := readPlatformBody(resp.Body)
	if err != nil {
		return fmt.Errorf("读取心跳响应失败: %w", err)
	}
	if perr := envelopeRefusal(raw); perr != nil {
		return fmt.Errorf("上报心跳被拒: code=%d, msg=%s", perr.Resp.Code, perr.Resp.Msg)
	}
	logger.Info("上报心跳成功")
	return nil
}

// ReportHeartbeatDefault 使用全局配置创建客户端并上报心跳
// 心跳上报为公开统计接口，不要求商户签名，故使用空 merchantKey。
func ReportHeartbeatDefault(req *ReportHeartbeatReq) error {
	return NewPlatformClient("").ReportHeartbeat(req)
}
