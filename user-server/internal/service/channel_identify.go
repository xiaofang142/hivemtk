package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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

// ErrIdentifyAccountRequired 多账号渠道必须给出消歧账号 ID，拒绝取首条。
var ErrIdentifyAccountRequired = errors.New("识别码定位需要明确的账号 ID")

// ChannelIdentifyService 识别码服务。
//
// T058 已完成读路径一次性切换（凭证闸门只认渠道识别码，旧 Bridge token 0% 接受）；
// T061 起**停用 Phase2 的双写**——不再把识别码写进旧 Bridge token 的 KV 位。
// CH-06：识别码是渠道级唯一码（每渠道一枚），不再把同码冗余写到该渠道**所有**账号
// （那与 channel_accounts.identify_code 全局唯一索引冲突，且无法多账号消歧）。
// 冗余列只写给显式指定的一个账号；未指定时清空冗余列，读路径统一走权威码表 +
// 明确账号信息定位。
type ChannelIdentifyService interface {
	Get(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
	List(ctx context.Context, tenantID uint) ([]dto.ChannelIdentifyCodeView, error)
	// CreateOrRotate 生成/重置识别码（version+1），旧码进入短暂在途窗口。
	// 不写冗余列（多账号渠道由权威表 + 明确账号信息消歧）。
	CreateOrRotate(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
	// CreateOrRotateForAccount 生成/重置并只把冗余码写给指定的单一账号（原子一致）。
	CreateOrRotateForAccount(ctx context.Context, tenantID, channelID, accountID uint) (*dto.ChannelIdentifyCodeView, error)
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
	return s.rotate(ctx, tenantID, channelID, 0)
}

// CreateOrRotateForAccount 仅把冗余码写给显式指定的账号（消歧信息必须校验）。
func (s *channelIdentifyService) CreateOrRotateForAccount(ctx context.Context, tenantID, channelID, accountID uint) (*dto.ChannelIdentifyCodeView, error) {
	if accountID == 0 || s.accountRepo == nil || !s.accountRepo.Available() {
		return nil, ErrIdentifyAccountRequired
	}
	acc, err := s.accountRepo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.ChannelID != channelID {
		return nil, ErrIdentifyAccountRequired // 错账号/跨渠道：拒绝，不取首条
	}
	return s.rotate(ctx, tenantID, channelID, accountID)
}

// rotate 核心：先 CAS 写权威码（版本单调 + 并发受控），成功后才动冗余列；
// 冗余写失败不回滚权威码，但会返回明确错误供对账（不静默半写成功）。
func (s *channelIdentifyService) rotate(ctx context.Context, tenantID, channelID, accountID uint) (*dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 || channelID == 0 || s.codeRepo == nil || !s.codeRepo.Available() {
		return nil, ErrChannelAccountInvalid
	}
	existing, err := s.codeRepo.GetByChannel(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	code, err := generateIdentifyCode()
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
	// CAS 写权威码：生产仓储实现 CAS 端口；替身仓储不支持时退回 Upsert（测试用）。
	type casRotator interface {
		RotateCAS(ctx context.Context, tenantID, channelID uint, expectVersion int, rec *model.ChannelIdentifyCode) error
	}
	if cas, ok := s.codeRepo.(casRotator); ok {
		if err := cas.RotateCAS(ctx, tenantID, channelID, version-1, rec); err != nil {
			if errors.Is(err, repository.ErrIdentifyVersionConflict) {
				return nil, err // 并发重置明确冲突，不静默覆盖
			}
			return nil, err
		}
	} else if err := s.codeRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	// 冗余列：只写给显式账号；未指定时清空该渠道全部冗余码，避免多账号同码冲突。
	if s.accountRepo != nil && s.accountRepo.Available() {
		if accountID > 0 {
			if err := s.accountRepo.Update(ctx, tenantID, accountID, map[string]any{"identify_code": code}); err != nil {
				return nil, err
			}
		} else {
			if err := s.accountRepo.SetIdentifyCodeByChannel(ctx, tenantID, channelID, ""); err != nil {
				return nil, err
			}
		}
	}
	view := toIdentifyView(rec)
	return &view, nil
}
