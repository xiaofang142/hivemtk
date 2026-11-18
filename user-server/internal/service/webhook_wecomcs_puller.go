package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// webhook_wecomcs_puller.go 企业微信客服（微信客服）增量拉取。
//
// 与其它平台不同，客服回调只是一个「有新消息」的通知：真正的消息体要用
// sync_msg 按 cursor 拉取。游标必须落库（wecom_cs_accounts.sync_cursor），
// 否则进程重启后会重复拉取或漏消息。
//
// 触发点有两个：
//   - 回调到达时立即拉一次（低延迟）；
//   - 后台 ticker 兜底（回调丢失/延迟时的补偿）。
//
// 同一账号同时只允许一个拉取器在跑（pulling 集合），避免游标竞态。

const wecomCSPullInterval = 60 * time.Second

type wecomCSService struct {
	svc *WebhookService

	mu      sync.Mutex
	clients map[uint]*wecomcs.Client
	pulling map[uint]struct{}

	// newClient 便于测试注入 httptest 基址（生产为 wecomcs.NewClient()）。
	newClient func() *wecomcs.Client
}

func (s *WebhookService) ensureWeComCS() *wecomCSService {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wecomcs == nil {
		s.wecomcs = &wecomCSService{
			svc:     s,
			clients: map[uint]*wecomcs.Client{},
			pulling: map[uint]struct{}{},
		}
	}
	return s.wecomcs
}

func (w *wecomCSService) clientFor(id uint) *wecomcs.Client {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.clients[id]; ok {
		return c
	}
	c := wecomcs.NewClient()
	if w.newClient != nil {
		c = w.newClient()
	}
	w.clients[id] = c
	return c
}

// dispatchWeComCS 回调到达：触发一次异步拉取，消息由拉取器逐条 ingress。
func (s *WebhookService) dispatchWeComCS(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if !s.connected() {
		return nil, nil
	}
	w := s.ensureWeComCS()
	w.trigger(accountID)
	return nil, nil
}

// SetWeComCSClientFactoryForTest 注入客服客户端构造器（仅测试用：httptest 基址）。
func (s *WebhookService) SetWeComCSClientFactoryForTest(fn func() *wecomcs.Client) {
	if fn == nil {
		return
	}
	s.ensureWeComCS().newClient = fn
}

func (w *wecomCSService) trigger(accountID string) {
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return
	}
	utils.SafeGo(context.Background(), "wecomcs.pull", func(ctx context.Context) {
		if err := w.pullOnce(ctx, uint(id)); err != nil {
			logger.Warnf("[WeComCS] pull account=%d failed: %v", id, err)
		}
	})
}

// pullOnce 拉取一个账号的增量消息直到 has_more=0。
func (w *wecomCSService) pullOnce(ctx context.Context, id uint) error {
	w.mu.Lock()
	if _, busy := w.pulling[id]; busy {
		w.mu.Unlock()
		return nil
	}
	w.pulling[id] = struct{}{}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pulling, id)
		w.mu.Unlock()
	}()

	repo := w.svc.wecomCSRepo
	if repo == nil {
		return errors.New("wecomcs repo 未装配")
	}
	acc, err := repo.GetByID(ctx, id)
	if err != nil || acc == nil {
		return fmt.Errorf("读取客服账号 %d: %w", id, err)
	}
	client := w.clientFor(id)
	return w.pullAccount(ctx, acc, client)
}

// pullAccount 可注入 client 的纯拉取逻辑（httptest 回放用）。
func (w *wecomCSService) pullAccount(ctx context.Context, acc *model.WeComCSAccount, client *wecomcs.Client) error {
	if acc == nil || client == nil {
		return errors.New("wecomcs 账号或客户端为空")
	}
	token, err := client.GetToken(ctx, acc.CorpID, acc.Secret)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	cursor := acc.SyncCursor
	accountID := strconv.FormatUint(uint64(acc.ID), 10)
	for page := 0; page < 50; page++ {
		resp, err := client.SyncMsg(ctx, token, wecomcs.SyncMsgReq{
			Cursor:   cursor,
			Token:    acc.Token,
			OpenKfID: acc.OpenKfID,
			Limit:    1000,
		})
		if err != nil {
			return err
		}
		for i := range resp.MsgList {
			msg := resp.MsgList[i]
			in := msg.ToInbound(accountID)
			if in == nil {
				// origin 4/5（系统/客服自己）不是客户消息：不落库，避免自问自答。
				continue
			}
			if _, err := w.svc.ingressPlatformInbound(ctx, in); err != nil {
				logger.Warnf("[WeComCS] ingress msg=%s failed: %v", msg.MsgID, err)
			}
		}
		next := resp.NextCursor
		if next == "" || next == cursor {
			break
		}
		cursor = next
		// 每页落地一次游标：崩溃最多重拉一页，不会全量重来。
		if w.svc.wecomCSRepo != nil {
			if err := w.svc.wecomCSRepo.UpdateSyncCursor(ctx, acc.ID, cursor); err != nil {
				logger.Warnf("[WeComCS] update cursor account=%d failed: %v", acc.ID, err)
			}
		}
		acc.SyncCursor = cursor
		if resp.HasMore == 0 {
			break
		}
	}
	return nil
}

// startWeComCSSweeper 后台兜底拉取：回调丢失时仍能在下一个周期补上。
func (s *WebhookService) startWeComCSSweeper(ctx context.Context) {
	w := s.ensureWeComCS()
	s.wg.Add(1)
	utils.SafeGo(ctx, "wecomcs.sweeper", func(ctx context.Context) {
		defer s.wg.Done()
		ticker := time.NewTicker(wecomCSPullInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				if s.wecomCSRepo == nil {
					continue
				}
				accs, err := s.wecomCSRepo.ListActive(ctx)
				if err != nil {
					logger.Warnf("[WeComCS] list active accounts failed: %v", err)
					continue
				}
				for _, acc := range accs {
					if acc == nil {
						continue
					}
					if err := w.pullOnce(ctx, acc.ID); err != nil {
						logger.Warnf("[WeComCS] sweep account=%d failed: %v", acc.ID, err)
					}
				}
			}
		}
	})
}
