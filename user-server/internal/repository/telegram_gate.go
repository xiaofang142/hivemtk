package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TelegramGroupGateRepository TG 群组管控网关仓库
type TelegramGroupGateRepository struct {
	db *gorm.DB
}

func NewTelegramGroupGateRepository() *TelegramGroupGateRepository {
	return &TelegramGroupGateRepository{db: _db.GetDB()}
}

func NewTelegramGroupGateRepositoryWithDB(db *gorm.DB) *TelegramGroupGateRepository {
	return &TelegramGroupGateRepository{db: db}
}

func (r *TelegramGroupGateRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *TelegramGroupGateRepository) Create(ctx context.Context, gate *model.TelegramGroupGate) error {
	return r.db.WithContext(ctx).Create(gate).Error
}

func (r *TelegramGroupGateRepository) Update(ctx context.Context, gate *model.TelegramGroupGate) error {
	return r.db.WithContext(ctx).Save(gate).Error
}

func (r *TelegramGroupGateRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.TelegramGroupGate{}, id).Error
}

func (r *TelegramGroupGateRepository) GetByID(ctx context.Context, id uint) (*model.TelegramGroupGate, error) {
	var gate model.TelegramGroupGate
	if err := r.db.WithContext(ctx).First(&gate, id).Error; err != nil {
		return nil, err
	}
	return &gate, nil
}

func (r *TelegramGroupGateRepository) GetByChatID(ctx context.Context, accountID uint, chatID string) (*model.TelegramGroupGate, error) {
	var gate model.TelegramGroupGate
	if err := r.db.WithContext(ctx).Where("account_id = ? AND chat_id = ?", accountID, chatID).First(&gate).Error; err != nil {
		return nil, err
	}
	return &gate, nil
}

func (r *TelegramGroupGateRepository) List(ctx context.Context, accountID uint) ([]*model.TelegramGroupGate, error) {
	var gates []*model.TelegramGroupGate
	q := r.db.WithContext(ctx)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.Order("id DESC").Find(&gates).Error; err != nil {
		return nil, err
	}
	return gates, nil
}

func (r *TelegramGroupGateRepository) ListEnabled(ctx context.Context) ([]*model.TelegramGroupGate, error) {
	var gates []*model.TelegramGroupGate
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&gates).Error; err != nil {
		return nil, err
	}
	return gates, nil
}

// TelegramGroupMemberRepository TG 群成员验证台账仓库
type TelegramGroupMemberRepository struct {
	db *gorm.DB
}

func NewTelegramGroupMemberRepository() *TelegramGroupMemberRepository {
	return &TelegramGroupMemberRepository{db: _db.GetDB()}
}

func NewTelegramGroupMemberRepositoryWithDB(db *gorm.DB) *TelegramGroupMemberRepository {
	return &TelegramGroupMemberRepository{db: db}
}

func (r *TelegramGroupMemberRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Upsert 按 (account_id, chat_id, user_id) 幂等写入成员记录
//
// welcome_sent_at / welcome_resends 一并覆盖：重新入群＝一段新的成员关系，
// 上一段的提示送达状态不该继承（否则再进群的人永远收不到验证提示）。
func (r *TelegramGroupMemberRepository) Upsert(ctx context.Context, m *model.TelegramGroupMember) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "chat_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "full_name", "join_status", "join_mode", "verify_token", "expires_at", "welcome_sent_at", "welcome_resends", "updated_at"}),
	}).Create(m).Error
}

func (r *TelegramGroupMemberRepository) Update(ctx context.Context, m *model.TelegramGroupMember) error {
	return r.db.WithContext(ctx).Save(m).Error
}

// MarkWelcomeSent 记录群内验证提示已送达，并清零补发计数。
//
// 只 SET 相关列且带 authorized=false 守卫：整行 Save 会把 read→write 窗口内
// 刚刚完成的私聊激活覆盖回 false，而按 key 定位不依赖主键（Upsert 路径拿不到 ID）。
func (r *TelegramGroupMemberRepository) MarkWelcomeSent(ctx context.Context, accountID uint, chatID, userID string, sentAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).
		Where("account_id = ? AND chat_id = ? AND user_id = ? AND authorized = ?", accountID, chatID, userID, false).
		Updates(map[string]any{"welcome_sent_at": sentAt, "welcome_resends": 0, "updated_at": time.Now()}).Error
}

// BumpWelcomeResend 记一次未送达的补发尝试（上限由调用方判定）。
func (r *TelegramGroupMemberRepository) BumpWelcomeResend(ctx context.Context, accountID uint, chatID, userID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).
		Where("account_id = ? AND chat_id = ? AND user_id = ? AND authorized = ?", accountID, chatID, userID, false).
		Updates(map[string]any{"welcome_resends": gorm.Expr("welcome_resends + 1"), "updated_at": now}).Error
}

// ClaimStalledResend 原子认领一条"从未送达"的补偿任务。
//
// 同一个成员可能同时被 webhook 重试和补偿循环（甚至多个清扫器实例）盯上；
// 先查 List 再发，谁都拦不住对方——认领必须是单条 UPDATE 的原子语义，带上
// 乐观锁：只有 welcome_sent_at 仍为空、补发数未达上限、且补发数仍等于 List
// 出来时的值（expectedResends）的行才能被认领。第二个认领者必然看到计数已
// 变，RowsAffected=0 ⇒ 跳过，于是同一行一轮只会发出一条提示。
// 认领失败不代表丢任务：没被认领的行下轮清扫还会再来（at-least-once 不变，
// 重复发送被杀掉）。
//
// 认领顺手把本次尝试计入 welcome_resends：调用方在后续的发送失败支路上不
// 要再 bump，避免一次尝试被记两次。
func (r *TelegramGroupMemberRepository) ClaimStalledResend(ctx context.Context, memberID uint, expectedResends, maxResends int) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).
		Where("id = ? AND welcome_sent_at IS NULL AND welcome_resends = ? AND welcome_resends < ?", memberID, expectedResends, maxResends).
		Updates(map[string]any{"welcome_resends": gorm.Expr("welcome_resends + 1"), "updated_at": time.Now()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// RetimeVerification 只刷新验证截止时间（未验证成员重新计时、不清退用）。
// 同样带 authorized=false 守卫：这期间已过审的人不该被重新计时，也不该被清扫踢出。
func (r *TelegramGroupMemberRepository) RetimeVerification(ctx context.Context, memberID uint, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).
		Where("id = ? AND authorized = ?", memberID, false).
		Updates(map[string]any{"expires_at": expiresAt, "updated_at": time.Now()}).Error
}

// MarkKicked 把超时未验证的成员落成 kicked。
//
// 同样只 SET 状态列并带 authorized=false 守卫：踢人前读的台账和写回之间隔着两次 TG
// 调用，这期间他刚好 /start 过审的话，整行 Save 会把 authorized 覆盖回 false——
// 人已解禁却被打回未验证，还会在下一轮被再踢一次。
func (r *TelegramGroupMemberRepository) MarkKicked(ctx context.Context, memberID uint) error {
	return r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).
		Where("id = ? AND authorized = ?", memberID, false).
		Updates(map[string]any{"join_status": model.TGMemberKicked, "updated_at": time.Now()}).Error
}

func (r *TelegramGroupMemberRepository) Get(ctx context.Context, accountID uint, chatID, userID string) (*model.TelegramGroupMember, error) {
	var m model.TelegramGroupMember
	if err := r.db.WithContext(ctx).Where("account_id = ? AND chat_id = ? AND user_id = ?", accountID, chatID, userID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetByToken 按 /start <token> 查找待验证成员
func (r *TelegramGroupMemberRepository) GetByToken(ctx context.Context, token string) (*model.TelegramGroupMember, error) {
	var m model.TelegramGroupMember
	if err := r.db.WithContext(ctx).Where("verify_token = ? AND authorized = ?", token, false).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListExpired 找出超时未验证的成员（用于 TTL 清扫）
func (r *TelegramGroupMemberRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]*model.TelegramGroupMember, error) {
	var members []*model.TelegramGroupMember
	q := r.db.WithContext(ctx).Where("authorized = ? AND join_status IN ? AND expires_at IS NOT NULL AND expires_at < ?",
		false, []string{model.TGMemberPending, model.TGMemberRestricted}, now)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListStalledRestricted 找出需要补偿循环补发入群提示的成员：
// 禁言中、未验证、welcome_sent_at 为空（提示从未送达）、验证窗口还没到期，
// 并且这一行当前没有正被入群请求内路径处理（idleBefore 之前就没再被写过，
// 或者已经留下一次失败尝试的痕迹）。
//
// 三组条件各拦一种线上故障：
//   - welcome_sent_at IS NULL 是必要条件。已送达提示的人该由 SweepExpired 按到期口径
//     处置，把他捞进来就会一遍遍给他续期，TTL 形同虚设——"同一名成员一天被重播 60+ 次
//     入群提示"就是这么来的（旧版用"临近到期"当提示没送达的代理，两个信号根本无关）。
//   - expires_at 还没到期（没有窗口视为待补，人工/迁移留下的行不该被静默漏掉）。窗口一旦
//     走完就交给 SweepExpired：他按"从未送达不处置"放过，补偿循环不再空转。
//   - 这一行不在请求内路径手里。HandleNewMembers 从写台账到收尾（登记送达或计一次失败）
//     之间要夹两次 TG 往返（实测 8–10 秒，受渠道层超时约束上界到分钟级），这段时间里这一行
//     恰好也满足"从未送达 + 窗口开着"，补偿循环跳进去就是给同一个人第二次禁言 + 第二次
//     播报（线上实测同一次入群收到三条提示）。归属信号是 updated_at：写台账（Upsert 覆盖列）
//     与每一次尝试的收尾（失败计数、认领、送达登记）都会把它顶到现在，所以
//     "updated_at 早于 idleBefore"＝这一行已经没人管了，可以补。
//     welcome_resends > 0 单独放行：计数 +1 是请求内路径在这一行上的最后一笔写入，
//     看见它就等于看见"这次尝试已经失败并交棒了"，补偿该立刻接手——否则安静窗口比
//     verify_ttl_min 还长的那些群会整批丢掉补偿能力。
//     这条是**延迟**判据而不是"年轻就永久跳过"：只要一直没人登记送达，跨过窗口就补；
//     updated_at 为 NULL（手工/迁移写入的行）一律视为早已安静，照补。
//
// 安静窗口取多长由调用方算成 idleBefore 传进来。补发次数的上限不在这一条 SQL 上执行：
// 它在 ClaimStalledResend 的认领谓词里（读到写之间的竞态只有落库那一侧拦得住），
// service 侧的预检只是省一次注定失败的认领。
func (r *TelegramGroupMemberRepository) ListStalledRestricted(ctx context.Context, now, idleBefore time.Time, limit int) ([]*model.TelegramGroupMember, error) {
	var members []*model.TelegramGroupMember
	q := r.db.WithContext(ctx).Where(
		"join_status = ? AND authorized = ? AND welcome_sent_at IS NULL AND (expires_at IS NULL OR expires_at > ?)"+
			" AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at <= ?)",
		model.TGMemberRestricted, false, now, idleBefore)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListByChat 群成员台账列表（管理端）
func (r *TelegramGroupMemberRepository) ListByChat(ctx context.Context, accountID uint, chatID string, status string, limit, offset int) ([]*model.TelegramGroupMember, int64, error) {
	var members []*model.TelegramGroupMember
	var total int64
	q := r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).Where("account_id = ? AND chat_id = ?", accountID, chatID)
	if status != "" {
		q = q.Where("join_status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&members).Error; err != nil {
		return nil, 0, err
	}
	return members, total, nil
}

// GetMemberByID 按主键读取成员台账（管理端人工放行兜底用）
func (r *TelegramGroupMemberRepository) GetMemberByID(ctx context.Context, memberID uint) (*model.TelegramGroupMember, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var member model.TelegramGroupMember
	if err := r.db.WithContext(ctx).First(&member, memberID).Error; err != nil {
		return nil, err
	}
	return &member, nil
}
