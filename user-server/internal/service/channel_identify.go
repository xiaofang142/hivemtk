package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ChannelIdentifyCodeGraceEnv 重置在途窗口长度（秒）。仅覆盖"重置瞬间仍在飞行"的
// 旧码请求，不是与旧 Bridge token 的兼容期——旧 token 自 T058 起 0% 被接受。
const ChannelIdentifyCodeGraceEnv = "CHANNEL_IDENTIFY_GRACE_SECONDS"

const defaultIdentifyGraceSeconds = 60

// ChannelIdentifyService 识别码服务。
//
// T058 已完成读路径一次性切换（凭证闸门只认渠道识别码，旧 Bridge token 0% 接受）；
// T061 起**停用 Phase2 的双写**——不再把识别码写进旧 Bridge token 的 KV 位
// （连续重置会挤掉 _prev 位最初那枚旧 token，且切换完成后该 KV 已无读方）。
type ChannelIdentifyService interface {
	Get(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
	List(ctx context.Context, tenantID uint) ([]dto.ChannelIdentifyCodeView, error)
	// CreateOrRotate 生成/重置识别码（version+1），旧码进入短暂在途窗口。
	CreateOrRotate(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
}

type channelIdentifyService struct {
	codeRepo    repository.ChannelIdentifyCodeRepository
	accountRepo repository.ChannelAccountRepository
}

// NewChannelIdentifyService 创建识别码服务。
func NewChannelIdentifyService() ChannelIdentifyService {
	return &channelIdentifyService{
		codeRepo:    repository.NewChannelIdentifyCodeRepository(),
		accountRepo: repository.NewChannelAccountRepository(),
	}
}

// NewChannelIdentifyServiceWithRepos 用显式依赖构造（装配点/测试用）。
func NewChannelIdentifyServiceWithRepos(
	codes repository.ChannelIdentifyCodeRepository,
	accounts repository.ChannelAccountRepository,
) ChannelIdentifyService {
	return &channelIdentifyService{codeRepo: codes, accountRepo: accounts}
}

func toIdentifyView(rec *model.ChannelIdentifyCode) dto.ChannelIdentifyCodeView {
	return dto.ChannelIdentifyCodeView{
		ID: rec.ID, ChannelID: rec.ChannelID, Code: rec.Code,
		Version: rec.Version, ResetAt: rec.ResetAt,
	}
}

func generateIdentifyCode() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "cid_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// identifyGraceWindow 读取在途窗口（秒），非法值回退默认 60s；<=0 表示关闭窗口。
func identifyGraceWindow() time.Duration {
	raw := os.Getenv(ChannelIdentifyCodeGraceEnv)
	if raw == "" {
		return defaultIdentifyGraceSeconds * time.Second
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultIdentifyGraceSeconds * time.Second
	}
	return time.Duration(n) * time.Second
}

func (s *channelIdentifyService) Get(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 || channelID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	rec, err := s.codeRepo.GetByChannel(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrChannelIdentifyNotFound
	}
	view := toIdentifyView(rec)
	return &view, nil
}

func (s *channelIdentifyService) List(ctx context.Context, tenantID uint) ([]dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	rows, err := s.codeRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelIdentifyCodeView, 0, len(rows))
	for i := range rows {
		out = append(out, toIdentifyView(&rows[i]))
	}
	return out, nil
}

// ErrChannelIdentifyNotFound 渠道尚未生成识别码（Controller 映射 404）。
var ErrChannelIdentifyNotFound = fmt.Errorf("渠道识别码不存在")

func (s *channelIdentifyService) CreateOrRotate(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 || channelID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	code, err := generateIdentifyCode()
	if err != nil {
		return nil, err
	}
	existing, err := s.codeRepo.GetByChannel(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	version := 1
	var resetAt *time.Time
	var prevCode string
	var prevUntil *time.Time
	if existing != nil {
		version = existing.Version + 1
		now := time.Now()
		resetAt = &now
		// 在途窗口：旧码短时继续被凭证闸门接受，窗口过后立即失效。
		if grace := identifyGraceWindow(); grace > 0 && existing.Code != "" {
			until := now.Add(grace)
			prevCode = existing.Code
			prevUntil = &until
		}
	}
	rec := &model.ChannelIdentifyCode{
		TenantID: tenantID, ChannelID: channelID, Code: code,
		Version: version, ResetAt: resetAt,
		PrevCode: prevCode, PrevValidUntil: prevUntil,
	}
	if err := s.codeRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	// 双写①：渠道账号冗余列（供 GetByIdentifyCode 第一优先级命中）。
	if err := s.accountRepo.SetIdentifyCodeByChannel(ctx, tenantID, channelID, code); err != nil {
		return nil, err
	}
	// 双写②（旧 Bridge token KV）已在 T061 停用：切换完成，旧 KV 无读方。
	view := toIdentifyView(rec)
	return &view, nil
}
