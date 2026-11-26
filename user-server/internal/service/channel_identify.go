package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ChannelIdentifyCodeGraceEnv 重置在途窗口长度（秒）。仅覆盖"重置瞬间仍在飞行"的
// 旧码请求，不是与旧 Bridge token 的兼容期——旧 token 自 T058 起 0% 被接受。
const ChannelIdentifyCodeGraceEnv = "CHANNEL_IDENTIFY_GRACE_SECONDS"

const defaultIdentifyGraceSeconds = 60

// ErrIdentifyAccountRequired 账号级识别码必须给出消歧账号 ID，拒绝取首条。
var ErrIdentifyAccountRequired = errors.New("识别码定位需要明确的账号 ID")

// ChannelIdentifyService 识别码服务。
//
// 语义（T07 修正）：识别码是**每个渠道账号一枚**的唯一凭证，权威列是
// channel_accounts.identify_code。channel_identify_codes 是存量渠道级兼容表：
// 老码继续被凭证闸门与解析器接受，管理面不再为渠道生成新码，避免同渠道
// 多账号互相顶掉（CH-06 旧口径）。
type ChannelIdentifyService interface {
	// Get 读取存量渠道级码（历史兼容；管理面主列表已改为账号级 List）。
	Get(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
	// List 账号级列表：每个有识别码的账号一行；无账号归属的存量渠道码附后。
	List(ctx context.Context, tenantID uint) ([]dto.ChannelIdentifyCodeView, error)
	// CreateOrRotate 历史渠道级轮转（仅兼容保留，管理面不再调用）。
	CreateOrRotate(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error)
	// CreateOrRotateForAccount 账号级唯一写入口：只写账号行（含在途窗口），不写渠道表。
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

// List 账号级识别码列表：权威的账号行在前，无账号归属的历史渠道码附后（只读兼容）。
// 按 code 去重：存量渠道码与账号码同值时只展示账号一行。
func (s *channelIdentifyService) List(ctx context.Context, tenantID uint) ([]dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if s.accountRepo == nil || !s.accountRepo.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	accounts, err := s.accountRepo.List(ctx, repository.ChannelAccountListOptions{TenantID: tenantID, Limit: 1000})
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelIdentifyCodeView, 0, len(accounts))
	seen := make(map[string]bool, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		code := strings.TrimSpace(acc.IdentifyCode)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, dto.ChannelIdentifyCodeView{
			ID: acc.ID, ChannelID: acc.ChannelID,
			AccountID: acc.ID, AccountName: acc.Name,
			Code: code, Version: 1,
		})
	}
	// 历史渠道级码：仅在没有任何账号持有同码时附后，避免重复展示。
	if s.codeRepo != nil && s.codeRepo.Available() {
		legacy, err := s.codeRepo.ListByTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for i := range legacy {
			code := strings.TrimSpace(legacy[i].Code)
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true
			view := toIdentifyView(&legacy[i])
			view.AccountName = "存量渠道码（兼容）"
			out = append(out, view)
		}
	}
	return out, nil
}

// ErrChannelIdentifyNotFound 渠道尚未生成识别码（Controller 映射 404）。
var ErrChannelIdentifyNotFound = fmt.Errorf("渠道识别码不存在")

// CreateOrRotate 历史渠道级轮转，仅保留给旧调用方；管理面已改走账号级入口。
func (s *channelIdentifyService) CreateOrRotate(ctx context.Context, tenantID, channelID uint) (*dto.ChannelIdentifyCodeView, error) {
	return s.rotate(ctx, tenantID, channelID, 0)
}

// CreateOrRotateForAccount 账号级唯一写入口：校验账号属于该渠道后只改账号行。
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
	return s.rotateAccount(ctx, tenantID, channelID, acc)
}

// rotateAccount 生成新码并原子写回账号行（identify_code + prev_code + prev_valid_until）。
// 不写 channel_identify_codes：每个账号各自持码，同渠道多账号互不顶替。
func (s *channelIdentifyService) rotateAccount(ctx context.Context, tenantID, channelID uint, acc *model.ChannelAccount) (*dto.ChannelIdentifyCodeView, error) {
	if tenantID == 0 || channelID == 0 || acc == nil || acc.ID == 0 {
		return nil, ErrIdentifyAccountRequired
	}
	code, err := generateIdentifyCode()
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"identify_code": code, "prev_code": "", "prev_valid_until": nil}
	if grace := identifyGraceWindow(); grace > 0 {
		old := strings.TrimSpace(acc.IdentifyCode)
		if old != "" && old != code {
			until := time.Now().Add(grace)
			updates["prev_code"] = old
			updates["prev_valid_until"] = &until
		}
	}
	if err := s.accountRepo.Update(ctx, tenantID, acc.ID, updates); err != nil {
		return nil, err
	}
	return &dto.ChannelIdentifyCodeView{
		ID: acc.ID, ChannelID: channelID,
		AccountID: acc.ID, AccountName: acc.Name,
		Code: code, Version: 1,
	}, nil
}

// rotate 历史渠道级轮转实现：CAS 写权威渠道码 + 清空/指定账号冗余码。
// 已被账号级 rotateAccount 取代，仅为旧调用方与兼容测试保留。
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
	type txProvider interface {
		WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	}
	if tp, ok := s.codeRepo.(txProvider); ok && s.codeRepo.Available() {
		err := tp.WithTransaction(ctx, func(txCtx context.Context) error {
			type casRotator interface {
				RotateCAS(ctx context.Context, tenantID, channelID uint, expectVersion int, rec *model.ChannelIdentifyCode) error
			}
			cas, ok := s.codeRepo.(casRotator)
			if !ok {
				return errors.New("事务仓储不支持 CAS")
			}
			if err := cas.RotateCAS(txCtx, tenantID, channelID, version-1, rec); err != nil {
				return err
			}
			if s.accountRepo != nil && s.accountRepo.Available() {
				if accountID > 0 {
					if err := s.accountRepo.Update(txCtx, tenantID, accountID, map[string]any{"identify_code": code}); err != nil {
						return err
					}
				} else {
					if err := s.accountRepo.SetIdentifyCodeByChannel(txCtx, tenantID, channelID, ""); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		type casRotator interface {
			RotateCAS(ctx context.Context, tenantID, channelID uint, expectVersion int, rec *model.ChannelIdentifyCode) error
		}
		if cas, ok := s.codeRepo.(casRotator); ok {
			if err := cas.RotateCAS(ctx, tenantID, channelID, version-1, rec); err != nil {
				return nil, err
			}
		} else if err := s.codeRepo.Upsert(ctx, rec); err != nil {
			return nil, err
		}
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
	}
	view := toIdentifyView(rec)
	return &view, nil
}
