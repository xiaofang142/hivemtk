package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListOnlineAgentIDs 返回当前在线坐席的 agent_id 列表（经 CAST 为字符串形式）。
func (r *InboxConversationRepository) ListOnlineAgentIDs(ctx context.Context) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var ids []string
	if err := r.db.WithContext(ctx).
		Table("agent_statuses").
		Where("status = ?", "online").
		Pluck("CAST(agent_id AS TEXT)", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// CountByAssignedToStatus 按 assigned_to + status 统计会话数（用于客服负载查询）
func (r *InboxConversationRepository) CountByAssignedToStatus(ctx context.Context, staffUserID string, statuses []string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("assigned_to = ? AND status IN ?", staffUserID, statuses).
		Count(&n).Error
	return n, err
}

type AssignTxInput struct {
	ConversationID uint
	Action         string
	ToType         string
	ToUserID       string
	ToSOPID        uint
	OperatorID     string
	Remark         string
}

// OldAssignedTo / NewAssignedTo 用于 service 层同步内存负载缓存（不属于 DB 操作）。
type AssignTxOutput struct {
	OldAssignedTo string
	NewAssignedTo string
	History       *model.InboxAssignment
}

// AssignTx 在单个事务内完成「更新会话 + 写入分配历史」。
// 会话不存在时返回 errors.New("conversation not found")，service 层据此映射 ErrInboxConversationMissing。
func (r *InboxConversationRepository) AssignTx(ctx context.Context, in AssignTxInput) (*AssignTxOutput, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	out := &AssignTxOutput{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conv model.InboxConversation
		// 锁内读取前态，避免并发改派从同一旧归属生成两条断裂历史。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, in.ConversationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("conversation not found")
			}
			return err
		}
		out.OldAssignedTo = conv.AssignedTo

		updates := map[string]any{}
		now := time.Now()
		switch in.Action {
		case "assign":
			updates["status"] = "assigned"
			updates["assigned_at"] = &now
		case "reassign":
			updates["status"] = "assigned"
			updates["assigned_at"] = &now
		case "release":
			updates["status"] = "open"
			updates["assigned_to"] = ""
			updates["assigned_to_sop"] = 0
		case "close":
			updates["status"] = "closed"
			updates["closed_at"] = &now
		case "reopen":
			updates["status"] = "unread"
			updates["assigned_to"] = ""
			updates["assigned_to_sop"] = 0
		}

		if in.Action == "assign" || in.Action == "reassign" {
			updates["assigned_to"] = ""
			updates["assigned_to_sop"] = 0
			switch in.ToType {
			case "human":
				updates["assigned_to"] = in.ToUserID
				out.NewAssignedTo = in.ToUserID
			case "sop":
				updates["assigned_to_sop"] = in.ToSOPID
			case "ai":

			}
		}

		switch in.Action {
		case "release":
			if err := tx.Model(&model.InboxConversation{}).
				Where("id = ?", in.ConversationID).
				UpdateColumn("assigned_at", gorm.Expr("NULL")).Error; err != nil {
				return err
			}
		case "reopen":
			if err := tx.Model(&model.InboxConversation{}).
				Where("id = ?", in.ConversationID).
				UpdateColumn("closed_at", gorm.Expr("NULL")).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&model.InboxConversation{}).
			Where("id = ?", in.ConversationID).
			Updates(updates).Error; err != nil {
			return err
		}

		hist := &model.InboxAssignment{
			ConversationID: conv.ID,
			Platform:       conv.Platform,
			AccountID:      conv.AccountID,
			CustomerID:     conv.CustomerID,
			Action:         in.Action,
			FromType:       inferFromType(conv.AssignedTo, conv.AssignedToSOP),
			FromUserID:     conv.AssignedTo,
			ToType:         in.ToType,
			ToUserID:       in.ToUserID,
			ToSOPID:        in.ToSOPID,
			OperatorID:     in.OperatorID,
			Remark:         in.Remark,
		}
		if err := tx.Create(hist).Error; err != nil {
			return err
		}
		out.History = hist
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type InboxAssignmentRepository struct {
	db *gorm.DB
}

func NewInboxAssignmentRepository() *InboxAssignmentRepository {
	return &InboxAssignmentRepository{db: _db.GetDB()}
}

// db 为 nil 时所有方法做无操作短路。
func NewInboxAssignmentRepositoryWithDB(db *gorm.DB) *InboxAssignmentRepository {
	return &InboxAssignmentRepository{db: db}
}

func (r *InboxAssignmentRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *InboxAssignmentRepository) ListByConversationID(ctx context.Context, conversationID uint, page, pageSize int) ([]*model.InboxAssignment, int64, error) {
	if r == nil || r.db == nil {
		return []*model.InboxAssignment{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	tx := r.db.WithContext(ctx).Model(&model.InboxAssignment{})
	if conversationID > 0 {
		tx = tx.Where("conversation_id = ?", conversationID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.InboxAssignment
	if err := tx.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

type AssignmentCount struct {
	AssignedTo string
	N          int64
}

// GroupCountByToUserID 按 to_user_id 聚合统计分配次数（用于轮询分配选最闲客服）
func (r *InboxAssignmentRepository) GroupCountByToUserID(ctx context.Context, candidates []string, action string) ([]AssignmentCount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var counts []AssignmentCount
	err := r.db.WithContext(ctx).Model(&model.InboxAssignment{}).
		Select("to_user_id AS assigned_to, COUNT(*) AS n").
		Where("to_user_id IN ? AND action = ?", candidates, action).
		Group("to_user_id").Scan(&counts).Error
	if err != nil {
		return nil, err
	}
	return counts, nil
}

func inferFromType(assignedTo string, assignedSOP uint) string {
	if assignedSOP > 0 {
		return "sop"
	}
	if assignedTo != "" {
		return "human"
	}
	return "system"
}
