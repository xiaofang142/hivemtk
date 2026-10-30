package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type MessageQueueService struct {
	repo   *repository.MessageQueueRepository
	queues map[string][]model.QueuedMessage
	status map[string]model.QueueStatus
	mu     sync.RWMutex
}

// NewMessageQueueService 构造消息队列服务（句柄由 repository 自取全局库）
func NewMessageQueueService() *MessageQueueService {
	return &MessageQueueService{
		repo:   repository.NewMessageQueueRepository(),
		queues: make(map[string][]model.QueuedMessage),
		status: make(map[string]model.QueueStatus),
	}
}

func (mq *MessageQueueService) AddBatch(ctx context.Context, messages []model.QueuedMessage) (string, error) {
	queueID := generateQueueID()

	queueStatus := &model.WhatsAppQueueStatus{
		QueueID: queueID,
		Total:   len(messages),
		Sent:    0,
		Failed:  0,
		Status:  "pending",
	}
	if err := mq.repo.CreateQueueStatus(ctx, queueStatus); err != nil {
		return "", fmt.Errorf("持久化队列状态失败: %w", err)
	}

	dbMessages := make([]*model.WhatsAppMessageQueue, 0, len(messages))
	for _, msg := range messages {
		dbMessages = append(dbMessages, &model.WhatsAppMessageQueue{
			QueueID:   queueID,
			MessageID: msg.ID,
			Content:   msg.Content,
			Status:    "pending",
			Platform:  "whatsapp",
			Recipient: msg.PhoneNumber,
		})
	}
	if err := mq.repo.CreateMessages(ctx, dbMessages); err != nil {
		_ = mq.repo.DeleteQueueStatusByQueueID(ctx, queueID)
		return "", fmt.Errorf("持久化队列消息失败: %w", err)
	}

	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.queues[queueID] = messages
	mq.status[queueID] = model.QueueStatus{
		QueueID: queueID,
		Total:   len(messages),
		Sent:    0,
		Failed:  0,
		Status:  "pending",
		Created: time.Now(),
		Updated: time.Now(),
	}

	return queueID, nil
}

func (mq *MessageQueueService) GetQueue(ctx context.Context, queueID string) []model.QueuedMessage {
	mq.mu.RLock()
	queue, exists := mq.queues[queueID]
	mq.mu.RUnlock()
	if exists {
		return queue
	}

	dbMessages, err := mq.repo.ListMessagesByQueueID(ctx, queueID)
	if err != nil {
		logger.Errorf("从数据库加载队列消息失败 queueID=%s: %v", queueID, err)
		return []model.QueuedMessage{}
	}
	if len(dbMessages) == 0 {
		return []model.QueuedMessage{}
	}

	messages := make([]model.QueuedMessage, 0, len(dbMessages))
	for _, m := range dbMessages {
		messages = append(messages, model.QueuedMessage{
			ID:          m.MessageID,
			PhoneNumber: m.Recipient,
			Content:     m.Content,
			Status:      m.Status,
		})
	}
	return messages
}

func (mq *MessageQueueService) UpdateStatus(ctx context.Context, queueID, messageID string, success bool) {
	msgStatus := "sent"
	if !success {
		msgStatus = "failed"
	}

	if err := mq.repo.UpdateMessageStatus(ctx, queueID, messageID, msgStatus); err != nil {
		logger.Errorf("更新队列消息状态失败 queueID=%s messageID=%s: %v", queueID, messageID, err)
	}

	if err := mq.repo.UpdateQueueStatusAtomic(ctx, queueID, success); err != nil {
		logger.Errorf("更新队列聚合状态失败 queueID=%s: %v", queueID, err)
	}

	mq.mu.Lock()
	if qs, ok := mq.status[queueID]; ok {
		if success {
			qs.Sent++
		} else {
			qs.Failed++
		}
		qs.Updated = time.Now()
		if qs.Sent+qs.Failed >= qs.Total {
			qs.Status = "completed"
		}
		mq.status[queueID] = qs
	}
	mq.mu.Unlock()
}

func (mq *MessageQueueService) GetStatus(ctx context.Context, queueID string) model.QueueStatus {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	if qs, ok := mq.status[queueID]; ok {
		return qs
	}

	dbStatus, err := mq.repo.GetQueueStatusByQueueID(ctx, queueID)
	if err != nil || dbStatus == nil {
		return model.QueueStatus{}
	}

	return model.QueueStatus{
		QueueID: dbStatus.QueueID,
		Total:   dbStatus.Total,
		Sent:    dbStatus.Sent,
		Failed:  dbStatus.Failed,
		Status:  dbStatus.Status,
		Created: dbStatus.CreatedAt,
		Updated: dbStatus.UpdatedAt,
	}
}

func (mq *MessageQueueService) ListAllStatuses(ctx context.Context) []model.QueueStatus {
	dbStatuses, err := mq.repo.ListAllQueueStatuses(ctx)
	if err != nil {
		logger.Errorf("列出队列状态失败: %v", err)
		return []model.QueueStatus{}
	}

	result := make([]model.QueueStatus, 0, len(dbStatuses))
	for _, s := range dbStatuses {
		result = append(result, model.QueueStatus{
			QueueID: s.QueueID,
			Total:   s.Total,
			Sent:    s.Sent,
			Failed:  s.Failed,
			Status:  s.Status,
			// 时间戳必须透传：发送记录页直接渲染 createdAt/updatedAt，
			// 丢掉会显示成 0001-01-01 00:00:00。
			Created: s.CreatedAt,
			Updated: s.UpdatedAt,
		})
	}
	return result
}

func generateQueueID() string {
	return fmt.Sprintf("queue_%d", time.Now().UnixNano())
}

func (mq *MessageQueueService) RecordGroupMessage(ctx context.Context, message model.QueuedMessage, status, errMsg string) {
	record := &model.WhatsappGroupMessage{
		ID:         uuid.New().String(),
		Phone:      message.PhoneNumber,
		Content:    message.Content,
		TemplateID: message.TemplateID,
		Status:     status,
		ErrorMsg:   errMsg,
		SentAt:     time.Now(),
	}
	if err := mq.repo.CreateGroupMessage(ctx, record); err != nil {
		logger.Errorf("写入群发记录失败: %v", err)
	}
}
