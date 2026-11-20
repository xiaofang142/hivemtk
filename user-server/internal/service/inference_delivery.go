package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type InferenceDeliveryService interface{ delivery.Gateway }

type inferenceDeliveryService struct {
	repo   repository.InferenceDeliveryRepository
	sender delivery.Sender
	policy delivery.Policy
}

func NewInferenceDeliveryService(repo repository.InferenceDeliveryRepository, sender delivery.Sender, policy delivery.Policy) InferenceDeliveryService {
	return &inferenceDeliveryService{repo, sender, policy}
}

func (s *inferenceDeliveryService) Submit(ctx context.Context, intent delivery.Intent) (*delivery.Record, error) {
	key, err := delivery.Identity(ctx, intent)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(intent.Content) == "" || len(intent.Content) > 65536 {
		return nil, delivery.ErrIdentity
	}
	if s.repo == nil {
		return nil, delivery.ErrUnavailable
	}
	id, _ := tenant.FromContext(ctx)
	body, _ := json.Marshal(intent)
	row, err := s.repo.Put(ctx, &model.InferenceDelivery{TenantID: id, Key: key, Fingerprint: delivery.Fingerprint(intent), IntentJSON: string(body), Kind: intent.Kind, State: string(delivery.Intended)})
	if err != nil {
		return nil, err
	}
	return s.advance(ctx, row)
}

func (s *inferenceDeliveryService) Get(ctx context.Context, key string) (*delivery.Record, error) {
	if s.repo == nil {
		return nil, delivery.ErrUnavailable
	}
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return deliveryRecord(row), nil
}

func (s *inferenceDeliveryService) Retry(ctx context.Context, key string) (*delivery.Record, error) {
	if s.repo == nil {
		return nil, delivery.ErrUnavailable
	}
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.advance(ctx, row)
}

func deliveryRecord(row *model.InferenceDelivery) *delivery.Record {
	r := &delivery.Record{Key: row.Key, State: delivery.State(row.State), Kind: row.Kind, SendAttempts: row.SendAttempts, ReceiptID: row.ReceiptID, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt, AcceptedAt: row.AcceptedAt, ConfirmedAt: row.ConfirmedAt}
	if row.AcceptedAt != nil {
		r.AcceptedCount = 1
	}
	if row.ConfirmedAt != nil {
		r.ConfirmedCount = 1
	}
	return r
}

func (s *inferenceDeliveryService) advance(ctx context.Context, row *model.InferenceDelivery) (*delivery.Record, error) {
	if err := ctx.Err(); err != nil {
		return deliveryRecord(row), err
	}
	if row.State == string(delivery.Confirmed) || row.State == string(delivery.Rejected) {
		return deliveryRecord(row), nil
	}
	var intent delivery.Intent
	if err := json.Unmarshal([]byte(row.IntentJSON), &intent); err != nil {
		return nil, delivery.ErrConflict
	}
	key, err := delivery.Identity(ctx, intent)
	if err != nil || key != row.Key || delivery.Fingerprint(intent) != row.Fingerprint {
		return nil, delivery.ErrConflict
	}
	now := time.Now().UTC()
	token := uuid.NewString()
	claimed, ok, err := s.repo.Claim(ctx, row.Key, token, now, now.Add(time.Minute))
	if err != nil {
		return deliveryRecord(row), err
	}
	if !ok {
		return s.Get(ctx, row.Key)
	}
	row = claimed
	storeFailure := func(cause error) (*delivery.Record, error) {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		// 只返回数据库里实际可查的状态，不把内存中的回执当成已持久确认。
		record, readErr := s.Get(readCtx, row.Key)
		if readErr != nil {
			return &delivery.Record{Key: row.Key, Kind: row.Kind, State: delivery.Unknown, ErrorCode: "delivery_store_unavailable"}, cause
		}
		return record, cause
	}
	finish := func(state delivery.State, code, receipt string) (*delivery.Record, error) {
		// 请求取消不代表外部动作撤销；用独立短上下文保存未知/受理状态。
		storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := s.repo.Finish(storeCtx, row.Key, token, string(state), code, receipt, row.AcceptedAt, row.ConfirmedAt); err != nil {
			return storeFailure(err)
		}
		return s.Get(storeCtx, row.Key)
	}
	if s.sender == nil || s.policy == nil {
		return finish(delivery.Blocked, "delivery_not_configured", row.ReceiptID)
	}
	allowed, err := s.policy.AuthorizeDelivery(ctx, intent)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[delivery] 授权失败")
		return finish(delivery.Blocked, "delivery_authorization_failed", row.ReceiptID)
	}
	if !allowed {
		if row.AcceptedAt != nil || row.SendAttempts > 0 {
			return finish(delivery.Unknown, "delivery_authorization_rejected", row.ReceiptID)
		}
		return finish(delivery.Rejected, "delivery_authorization_rejected", row.ReceiptID)
	}
	if ctx.Err() != nil {
		return finish(delivery.Unknown, "delivery_canceled", row.ReceiptID)
	}
	// 即使首次尝试也先查证；故障、重启和不明回执绝不能触发无条件重发。
	receipt, err := s.sender.Lookup(ctx, row.Key, intent)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[delivery] 回执查询失败")
		return finish(delivery.Unknown, "delivery_lookup_failed", row.ReceiptID)
	}
	if receipt.State == delivery.Absent {
		if row.AcceptedAt != nil {
			return finish(delivery.Unknown, "delivery_receipt_missing", row.ReceiptID)
		}
		if err := ctx.Err(); err != nil {
			return finish(delivery.Unknown, "delivery_canceled", row.ReceiptID)
		}
		if err := s.repo.MarkSend(ctx, row.Key, token); err != nil {
			return storeFailure(err)
		}
		receipt, err = s.sender.Send(ctx, row.Key, intent)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("[delivery] 发送回执不明")
			return finish(delivery.Unknown, "delivery_send_unknown", row.ReceiptID)
		}
	}
	if receipt.State == delivery.Accepted || receipt.State == delivery.Confirmed {
		if receipt.ID == "" || len(receipt.ID) > 512 || (row.ReceiptID != "" && row.ReceiptID != receipt.ID) {
			return finish(delivery.Unknown, "delivery_receipt_invalid", row.ReceiptID)
		}
		newAccepted, newConfirmed := row.AcceptedAt == nil, receipt.State == delivery.Confirmed && row.ConfirmedAt == nil
		at := time.Now().UTC()
		if row.AcceptedAt == nil {
			row.AcceptedAt = &at
		}
		if receipt.State == delivery.Confirmed {
			row.ConfirmedAt = &at
		}
		record, err := finish(receipt.State, "", receipt.ID)
		if err == nil {
			record.NewAccepted, record.NewConfirmed = newAccepted, newConfirmed
		}
		return record, err
	}
	if receipt.State == delivery.Rejected && row.AcceptedAt == nil {
		return finish(delivery.Rejected, "delivery_rejected", row.ReceiptID)
	}
	return finish(delivery.Unknown, "delivery_receipt_unknown", row.ReceiptID)
}
