package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

var merchantKey string

func generateRandomKey(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	key := make([]byte, length)
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString(b[:length])
	}
	for i := range key {
		key[i] = charset[int(b[i])%len(charset)]
	}
	return string(key)
}

// merchantStateDir 本机身份状态（merchant key / 每商户签名密钥）的落盘目录。
// 默认沿用 config/（与 .merchant_api_secret 同处），MERCHANT_STATE_DIR 用于测试隔离
// 以及只读挂载部署把状态挪到可写卷。
func merchantStateDir() string {
	if d := os.Getenv("MERCHANT_STATE_DIR"); d != "" {
		return d
	}
	return "config"
}

func merchantKeyFilePath() string {
	return filepath.Join(merchantStateDir(), ".merchant_key")
}

// loadOrInitMerchantKey 读取本机 merchant key，不存在则生成并落盘。
//
// merchant key 是本部署在平台上的唯一身份锚点（同时决定贡献者账号 mtk_<key>），
// 所以所有失败分支一律返回空 key 而不回落到新生成的 key：一个「存在但读不出来」的文件
// 说明身份本来就有，重新生成等于把它永久丢在平台侧；静默生成正是重启漂移的成因。
func loadOrInitMerchantKey() (string, error) {
	p := merchantKeyFilePath()
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", fmt.Errorf("merchant key 文件内容为空(%s)，拒绝另起身份", p)
		}
		return key, nil
	case errors.Is(err, fs.ErrNotExist):
		// 首次安装：生成后落盘，后续启动复用
	default:
		return "", fmt.Errorf("读取 merchant key(%s) 失败: %w", p, err)
	}

	key := generateRandomKey(32)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", fmt.Errorf("创建 merchant key 目录失败: %w", err)
	}
	if err := os.WriteFile(p, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("写入 merchant key(%s) 失败: %w", p, err)
	}
	return key, nil
}

// InitSync 初始化平台同步
// 独立部署版本：商户端为单租户独立部署，不再向平台上报日志或同步授权。
func InitSync() error {
	var err error
	merchantKey, err = loadOrInitMerchantKey()
	if err != nil {
		return err
	}
	logger.Info("[独立部署模式] 平台同步已禁用（InitSync 仅上报商户身份）")
	key := merchantKey // 协程只读闭包副本：再读包级变量就会和下一次启动赋值构成数据竞争
	utils.SafeGo(context.Background(), "platform.register_merchant", func(ctx context.Context) {
		if err := registerMerchantAs(key, RegisterMerchantReq{
			Name:         "HiveMTK 本地商户",
			ContactEmail: key + "@local",
			DeviceInfo:   runtime.GOOS + " " + runtime.GOARCH,
		}); err != nil {
			logger.Warn("[独立部署模式] 商户注册到平台失败（可忽略，需平台侧 MERCHANT_API_SECRET 与本端一致）: " + err.Error())
		} else {
			logger.Info("[独立部署模式] 已自动向平台注册商户 key=" + key)
		}
	})
	return nil
}

// StopAllTasks 停止所有后台任务
// 独立部署版本：无需停止任何平台同步任务，保留为 no-op 以保持外部调用方不变。
func StopAllTasks() {}

// RegisterMerchant 向平台注册本地商户信息
func RegisterMerchant(req RegisterMerchantReq) error {
	return registerMerchantAs(GetMerchantKey(), req)
}

func registerMerchantAs(key string, req RegisterMerchantReq) error {
	if req.DeviceInfo == "" {
		req.DeviceInfo = runtime.GOOS + " " + runtime.GOARCH
	}
	return NewPlatformClient(key).RegisterMerchant(req)
}

// CheckConnection 探测平台连通性。三处调用点（app-config / 同步 / 健康检查）要的都是
// "平台在不在"，不是"授权状态"——后者属商业版契约，开源平台从未实现该端点（R12）。
func CheckConnection() error {
	return NewPlatformClient(merchantKey).CheckConnection()
}

// GetMerchantKey 返回当前部署实例的 merchant key
func GetMerchantKey() string {
	return merchantKey
}

// SetMerchantKeyForTest 测试时设置 merchant key
func SetMerchantKeyForTest(key string) {
	merchantKey = key
}
