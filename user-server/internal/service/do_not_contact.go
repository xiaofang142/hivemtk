package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// PhoneOneIDPrefix 无法反查 one_id 时，phone 维度归一键前缀。
//
// 说明：one_id（customer.unified_id）是全局退订的聚合键，必须优先通过
// customer 表按手机号反查真实 OneID。仅当客户档案中不存在该手机号
// （如陌生号码直接回复退订）时，才降级使用 "phone:"+phone 作为归一键，
// 保证该号码后续仍可被拦截；一旦该 phone 关联到客户，应以真实 OneID 为准。
const PhoneOneIDPrefix = "phone:"

// PhoneToOneIDResolver 手机号 → OneID 反查函数；返回空串表示无法反查
type PhoneToOneIDResolver func(ctx context.Context, phone string) string

// DoNotContactService 客户全局跨渠道退订标志位服务
//
// 合规语义：
//   - Block / Unblock：写入/移除全局标志位，所有动作写审计日志（结构化留痕）
//   - IsBlocked：发送前置检查，先查渠道精确行再查全局行(channel="")，任一命中即 blocked
//   - BackfillFromSMSUnsubscribe：存量 sms_unsubscribes 数据一次性回填到全局表（幂等）
type DoNotContactService struct {
	repo         repository.CustomerDoNotContactRepository
	smsUnsubRepo repository.SmsUnsubscribeRepository
	resolveOneID PhoneToOneIDResolver
}

// NewDoNotContactService 创建全局退订标志位服务
func NewDoNotContactService(repo repository.CustomerDoNotContactRepository) *DoNotContactService {
	if repo == nil {
		repo = repository.NewCustomerDoNotContactRepository(nil)
	}
	s := &DoNotContactService{repo: repo}
	return s
}

// SetSmsUnsubscribeRepo 注入短信退订仓库（BackfillFromSMSUnsubscribe 使用）
func (s *DoNotContactService) SetSmsUnsubscribeRepo(repo repository.SmsUnsubscribeRepository) {
	s.smsUnsubRepo = repo
}

// SetPhoneResolver 注入 phone→one_id 反查实现（默认走 customer 表）
func (s *DoNotContactService) SetPhoneResolver(fn PhoneToOneIDResolver) {
	s.resolveOneID = fn
}

func (s *DoNotContactService) defaultPhoneResolver() PhoneToOneIDResolver {
	customerRepo := repository.NewCustomerRepository()
	return func(ctx context.Context, phone string) string {
		customer, err := customerRepo.GetByPhone(ctx, phone)
		if err != nil {
			logger.Errorf("[DNC] 反查 customer 失败 phone=%s: %v", phone, err)
			return ""
		}
		if customer == nil || customer.UnifiedID == "" {
			return ""
		}
		return customer.UnifiedID
	}
}

// Block 写入全局退订标志位（幂等）并写审计日志
//
// channel 传空串表示全渠道退订；source 记录退订来源便于审计追溯。
func (s *DoNotContactService) Block(ctx context.Context, oneID, channel, source string) error {
	if strings.TrimSpace(oneID) == "" {
		return errors.New("one_id 不能为空")
	}
	if source == "" {
		source = model.DNCSourceManual
	}

	record := &model.CustomerDoNotContact{
		OneID:   oneID,
		Channel: channel,
		Source:  source,
	}
	created, err := s.repo.Create(ctx, record)
	if err != nil {
		logger.Errorf("[DNC] 写入全局退订标志位失败 one_id=%s channel=%q source=%s: %v", oneID, channel, source, err)
		return err
	}

	if created {
		logger.Infof("[DNC][审计] 全局退订标志位已写入 one_id=%s channel=%q source=%s", oneID, channel, source)
	} else {
		logger.Infof("[DNC][审计] 全局退订标志位已存在，幂等跳过 one_id=%s channel=%q source=%s", oneID, channel, source)
	}
	return nil
}

// BlockFromPhone 按手机号写入全局退订标志位
//
// 优先通过 customer 表反查 one_id；无法反查时降级用 "phone:"+phone 归一键
// （见 PhoneOneIDPrefix 注释），保证号码维度仍被拦截。
func (s *DoNotContactService) BlockFromPhone(ctx context.Context, phone, source string) error {
	phone = NormalizePhone(phone)
	if phone == "" {
		return errors.New("phone 不能为空")
	}
	oneID := s.resolvePhoneToOneID(ctx, phone)
	if oneID == "" {
		oneID = PhoneOneIDPrefix + phone
	}
	return s.Block(ctx, oneID, model.DoNotContactChannelAll, source)
}

// IsBlocked 发送前置检查：oneID 在指定渠道是否被全局退订拦截
//
// 查询顺序：channel 精确行优先，再查全局行(channel="")，任一命中即 blocked。
// DB 异常时返回 false 并记录错误日志（与项目现有 IsUnsubscribed 容错习惯对齐，
// 由调用方的其他合规检查兜底）。
func (s *DoNotContactService) IsBlocked(ctx context.Context, oneID, channel string) bool {
	if strings.TrimSpace(oneID) == "" {
		return false
	}
	channels := []string{model.DoNotContactChannelAll}
	if channel != "" {
		channels = append([]string{channel}, channels...)
	}
	exists, err := s.repo.ExistsByOneIDAndChannels(ctx, oneID, channels)
	if err != nil {
		logger.Errorf("[DNC] 查询全局退订状态失败 one_id=%s channel=%s: %v", oneID, channel, err)
		return false
	}
	return exists
}

// Unblock 移除全局退订标志位（重新订阅），并写审计日志
func (s *DoNotContactService) Unblock(ctx context.Context, oneID, channel string) error {
	if strings.TrimSpace(oneID) == "" {
		return errors.New("one_id 不能为空")
	}
	if err := s.repo.DeleteByOneIDAndChannel(ctx, oneID, channel); err != nil {
		logger.Errorf("[DNC] 移除全局退订标志位失败 one_id=%s channel=%q: %v", oneID, channel, err)
		return err
	}
	logger.Infof("[DNC][审计] 全局退订标志位已移除 one_id=%s channel=%q", oneID, channel)
	return nil
}

// BackfillFromSMSUnsubscribe 存量回填：读取 sms_unsubscribes 全表，
// 逐条转换写入全局退订表（幂等：唯一索引冲突跳过）。
//
// 返回值 added：本次实际新写入的行数。
// 手动触发即可，不做 cron。
func (s *DoNotContactService) BackfillFromSMSUnsubscribe(ctx context.Context) (int, error) {
	repo := s.smsUnsubRepo
	if repo == nil {
		repo = repository.NewSmsUnsubscribeRepository(nil)
	}
	records, err := repo.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("读取存量短信退订名单失败: %w", err)
	}

	added := 0
	for _, r := range records {
		if r == nil || r.Phone == "" {
			continue
		}
		created, berr := s.blockFromPhoneCounted(ctx, r.Phone, model.DNCSourceSMSKeyword)
		if berr != nil {

			logger.Errorf("[DNC] 回填单条失败 phone=%s: %v", r.Phone, berr)
			continue
		}
		if created {
			added++
		}
	}
	logger.Infof("[DNC][审计] 存量回填完成 总数=%d 新增=%d", len(records), added)
	return added, nil
}

func (s *DoNotContactService) blockFromPhoneCounted(ctx context.Context, phone, source string) (bool, error) {
	phone = NormalizePhone(phone)
	if phone == "" {
		return false, errors.New("phone 不能为空")
	}
	oneID := s.resolvePhoneToOneID(ctx, phone)
	if oneID == "" {
		oneID = PhoneOneIDPrefix + phone
	}
	record := &model.CustomerDoNotContact{
		OneID:   oneID,
		Channel: model.DoNotContactChannelAll,
		Source:  source,
	}
	created, err := s.repo.Create(ctx, record)
	if err != nil {
		return false, err
	}
	if created {
		logger.Infof("[DNC][审计] 回填写入全局退订标志位 one_id=%s channel=%q source=%s", oneID, model.DoNotContactChannelAll, source)
	}
	return created, nil
}

func (s *DoNotContactService) resolvePhoneToOneID(ctx context.Context, phone string) string {
	if s.resolveOneID != nil {
		return s.resolveOneID(ctx, phone)
	}
	return s.defaultPhoneResolver()(ctx, phone)
}

// ListBlocks 查询 one_id 的全部退订标志位（管理端排查用）
func (s *DoNotContactService) ListBlocks(ctx context.Context, oneID string) ([]*model.CustomerDoNotContact, error) {
	return s.repo.ListByOneID(ctx, oneID)
}
