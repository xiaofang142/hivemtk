package sms

import (
	"context"
	"errors"
	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/service/dnc"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

var smsUnsubscribeKeywords = []string{
	"TD", "td", "Td", "tD",
	"退订", "退定", "取消订阅", "拒绝接收",
	"T退", "t退",
	"取消", "不想接收", "停止",
	"N", "n",
	"Q", "q",
	"0",
	"Unsubscribe", "unsubscribe", "STOP", "stop",
}

// SmsUnsubscribeService 短信退订服务
//
// 合规要点：
//   - UnsubscribePhone：记录退订请求，关键词命中留痕
//   - IsUnsubscribed：发送前必须调用，命中则跳过发送
//   - ProcessUnsubscribeReply：解析上行短信，识别退订关键词
//   - ResubscribePhone：允许用户重新订阅（合规要求）
type SmsUnsubscribeService struct {
	repo repository.SmsUnsubscribeRepository

	dnc *dnc.DoNotContactService
}

// SetDoNotContact 注入全局退订标志位服务（不注入时按需懒加载默认实现）
func (s *SmsUnsubscribeService) SetDoNotContact(dnc *dnc.DoNotContactService) {
	s.dnc = dnc
}

// NewSmsUnsubscribeService 创建短信退订服务
func NewSmsUnsubscribeService(repo repository.SmsUnsubscribeRepository) *SmsUnsubscribeService {
	if repo == nil {
		repo = repository.NewSmsUnsubscribeRepository(nil)
	}
	return &SmsUnsubscribeService{repo: repo}
}

// UnsubscribePhone 记录手机号退订请求
// 重复退订幂等：若已存在退订记录，更新退订时间和原因
func (s *SmsUnsubscribeService) UnsubscribePhone(ctx context.Context, phone, reason, msgID, keyword string) error {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return errors.New("phone 不能为空")
	}

	existing, err := s.repo.GetByPhone(ctx, phone)
	if err != nil {
		logger.Errorf("查询短信退订记录失败 phone=%s: %v", phone, err)
		return err
	}

	now := time.Now()
	if existing != nil {
		existing.Reason = reason
		existing.SourceMessageID = msgID
		existing.KeywordMatched = keyword
		existing.UnsubscribedAt = now
		if err := s.repo.Update(ctx, existing); err != nil {
			return err
		}
		s.syncGlobalDoNotContact(ctx, phone)
		return nil
	}

	record := &model.SmsUnsubscribe{
		Phone:           phone,
		Reason:          reason,
		UnsubscribedAt:  now,
		SourceMessageID: msgID,
		KeywordMatched:  keyword,
	}
	if err := s.repo.Create(ctx, record); err != nil {
		return err
	}
	s.syncGlobalDoNotContact(ctx, phone)
	return nil
}

func (s *SmsUnsubscribeService) syncGlobalDoNotContact(ctx context.Context, phone string) {
	svc := s.dnc
	if svc == nil {
		svc = dnc.NewDoNotContactService(nil)
		s.dnc = svc
	}
	if err := svc.BlockFromPhone(ctx, phone, model.DNCSourceSMSKeyword); err != nil {
		logger.Errorf("[DNC] 短信退订同步全局标志位失败 phone=%s: %v", phone, err)
	}
}

// IsUnsubscribed 检查手机号是否已退订（发送前必须调用）
func (s *SmsUnsubscribeService) IsUnsubscribed(ctx context.Context, phone string) bool {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return false
	}
	exists, err := s.repo.ExistsByPhone(ctx, phone)
	if err != nil {
		logger.Errorf("查询短信退订状态失败 phone=%s: %v", phone, err)
		return false
	}
	return exists
}

// ResubscribePhone 允许用户重新订阅（合规要求）
//
// 幂等：号码本来就不在退订名单里时返回 nil —— 「重新订阅」要达成的状态
// （这个号码可以继续收到短信）此时**已经成立**，重复调用必须成功，否则合规
// 动作会因为「你已经不在名单里」而报错。
//
// 仓储层对此照实回 gorm.ErrRecordNotFound（删一条不存在的记录就是没删到，
// 静默回成功会让真正的空操作无从辨别），由这一层把「0 行」翻译成幂等成功。
func (s *SmsUnsubscribeService) ResubscribePhone(ctx context.Context, phone string) error {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return errors.New("phone 不能为空")
	}
	if err := s.repo.DeleteByPhone(ctx, phone); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// ProcessUnsubscribeReply 处理上行短信回复
//
// 解析用户回复内容，若命中退订关键词则自动加入退订名单
// 返回值 matchedKeyword：命中的关键词（未命中时为空字符串）
func (s *SmsUnsubscribeService) ProcessUnsubscribeReply(ctx context.Context, phone, replyContent, msgID string) (matchedKeyword string, err error) {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return "", errors.New("phone 不能为空")
	}
	if replyContent == "" {
		return "", errors.New("reply_content 不能为空")
	}

	keyword := MatchUnsubscribeKeyword(replyContent)
	if keyword == "" {
		return "", nil
	}

	reason := "用户回复关键词：" + replyContent
	if err := s.UnsubscribePhone(ctx, phone, reason, msgID, keyword); err != nil {
		return keyword, err
	}
	return keyword, nil
}

// ListUnsubscribes 分页查询退订名单
func (s *SmsUnsubscribeService) ListUnsubscribes(ctx context.Context, page, limit int, keyword string) ([]*model.SmsUnsubscribe, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 20
	}
	return s.repo.List(ctx, page, limit, keyword)
}

// ListAllUnsubscribes 查询全部退订名单（导出使用）
func (s *SmsUnsubscribeService) ListAllUnsubscribes(ctx context.Context) ([]*model.SmsUnsubscribe, error) {
	return s.repo.ListAll(ctx)
}

// MatchUnsubscribeKeyword 检查回复内容是否包含退订关键词
//
// 返回命中的关键词（原始大小写），未命中返回空字符串
// 匹配策略：
//  1. 完全匹配（去除前后空格后）优先，避免误判"TD-RFID"等
//  2. 单字符关键词（N/Q/0/TD）必须独立词，避免误判"NT999"
//  3. 中文关键词支持子串匹配（"TD退订"）
func MatchUnsubscribeKeyword(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	for _, kw := range smsUnsubscribeKeywords {
		if strings.EqualFold(content, kw) {
			return kw
		}
	}

	lower := strings.ToLower(content)
	for _, kw := range smsUnsubscribeKeywords {
		if isAsciiKeyword(kw) {
			if isStandaloneWord(lower, strings.ToLower(kw)) {
				return kw
			}
		} else {
			if strings.Contains(content, kw) {
				return kw
			}
		}
	}
	return ""
}

func isAsciiKeyword(kw string) bool {
	for _, r := range kw {
		if r > 127 {
			return false
		}
	}
	return true
}

func isStandaloneWord(content, kw string) bool {
	if kw == "" {
		return false
	}
	idx := strings.Index(content, kw)
	for idx >= 0 {
		beforeOK := idx == 0
		if !beforeOK {
			r := rune(content[idx-1])
			beforeOK = !isAlnum(r)
		}
		afterIdx := idx + len(kw)
		afterOK := afterIdx >= len(content)
		if !afterOK {
			r := rune(content[afterIdx])
			afterOK = !isAlnum(r)
		}
		if beforeOK && afterOK {
			return true
		}
		next := idx + 1
		if next >= len(content) {
			break
		}
		idx = strings.Index(content[next:], kw)
		if idx < 0 {
			break
		}
		idx += next
	}
	return false
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
