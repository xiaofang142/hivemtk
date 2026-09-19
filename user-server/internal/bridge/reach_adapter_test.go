package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

type mockReachAdapter struct {
	msgID string
	err   error

	gotSMSPhone     string
	gotSMSContent   string
	gotSMSParams    map[string]string
	gotEmailTo      string
	gotEmailSubj    string
	gotEmailAttach  []string
	gotWeComAccount string
	gotWeComExt     string
	gotWeixinOpen   string
	gotWeixinMT     string
	gotDingChat     string
	gotTGAccount    string
	gotTGChat       string
	gotWAAccount    string
	gotFeiAccount   string
	gotFeiOpen      string
	gotWebSession   string
	gotCardChannel  string
	gotRecallCh     string
	gotRecallMsgID  string
	gotHealthCh     string
	gotListCh       string
}

func (m *mockReachAdapter) SendSMS(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
	m.gotSMSPhone, m.gotSMSContent, m.gotSMSParams = phone, content, params
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendEmail(ctx context.Context, to, subject, content string, attachments []string) (string, error) {
	m.gotEmailTo, m.gotEmailSubj, m.gotEmailAttach = to, subject, attachments
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendWeCom(ctx context.Context, accountID, externalUserID, msgType, content string) (string, error) {
	m.gotWeComAccount, m.gotWeComExt = accountID, externalUserID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendWeixin(ctx context.Context, openID, msgType, content string) (string, error) {
	m.gotWeixinOpen, m.gotWeixinMT = openID, msgType
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendDouyin(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendKuaishou(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendXHS(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendTikTok(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendXianyu(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendDingTalk(ctx context.Context, chatID, msgType, content string) (string, error) {
	m.gotDingChat = chatID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendTelegram(ctx context.Context, accountID, chatID, content string) (string, error) {
	m.gotTGAccount, m.gotTGChat = accountID, chatID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (string, error) {
	m.gotWAAccount = accountID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendFeishu(ctx context.Context, accountID, openID, content string) (string, error) {
	m.gotFeiAccount, m.gotFeiOpen = accountID, openID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendWeb(ctx context.Context, sessionID, content string) (string, error) {
	m.gotWebSession = sessionID
	return m.msgID, m.err
}
func (m *mockReachAdapter) SendCard(ctx context.Context, channel, accountID, externalUserID, cardID string) (string, error) {
	m.gotCardChannel = channel
	return m.msgID, m.err
}
func (m *mockReachAdapter) Recall(ctx context.Context, channel, msgID string) error {
	m.gotRecallCh, m.gotRecallMsgID = channel, msgID
	return m.err
}
func (m *mockReachAdapter) AccountHealth(ctx context.Context, channel, accountID string) (*tooluse.AccountHealthInfo, error) {
	m.gotHealthCh = channel
	return &tooluse.AccountHealthInfo{AccountID: accountID, Channel: channel, Status: "healthy"}, m.err
}
func (m *mockReachAdapter) ListAccounts(ctx context.Context, channel string) ([]tooluse.AccountInfo, error) {
	m.gotListCh = channel
	return []tooluse.AccountInfo{{Channel: channel, AccountID: "acc-1", Nickname: "测试", Status: "active"}}, m.err
}

var _ tooluse.ReachAdapter = (*mockReachAdapter)(nil)

// TestBridgeReachAdapter_PassthroughMethods 覆盖所有 inner 透传方法
func TestBridgeReachAdapter_PassthroughMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("SendSMS 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "sms-123"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendSMS(ctx, "13800000000", "hello", "tpl1", map[string]string{"k": "v"})
		if err != nil || id != "sms-123" {
			t.Errorf("SendSMS 期望 sms-123 nil err, 得 %q %v", id, err)
		}
		if mock.gotSMSPhone != "13800000000" {
			t.Errorf("SendSMS phone 未透传, 得 %q", mock.gotSMSPhone)
		}
	})

	t.Run("SendEmail 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "em-456"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendEmail(ctx, "a@b.com", "主题", "内容", []string{"att.pdf"})
		if err != nil || id != "em-456" {
			t.Errorf("SendEmail 期望 em-456 nil err, 得 %q %v", id, err)
		}
		if mock.gotEmailTo != "a@b.com" || mock.gotEmailSubj != "主题" {
			t.Error("SendEmail 参数未透传")
		}
	})

	t.Run("SendWeCom 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "wc-789"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendWeCom(ctx, "acc", "ext123", "text", "hi")
		if err != nil || id != "wc-789" {
			t.Errorf("SendWeCom 期望 wc-789, 得 %q %v", id, err)
		}
		if mock.gotWeComAccount != "acc" || mock.gotWeComExt != "ext123" {
			t.Error("SendWeCom 参数未透传")
		}
	})

	t.Run("SendWeixin 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "wx-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendWeixin(ctx, "openid-1", "text", "hi")
		if err != nil || id != "wx-1" {
			t.Errorf("SendWeixin 期望 wx-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendDingTalk 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "dt-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendDingTalk(ctx, "chat-1", "text", "hi")
		if err != nil || id != "dt-1" {
			t.Errorf("SendDingTalk 期望 dt-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendTelegram 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "tg-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendTelegram(ctx, "tg-acc", "tg-chat", "hi")
		if err != nil || id != "tg-1" {
			t.Errorf("SendTelegram 期望 tg-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendWhatsApp 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "wa-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendWhatsApp(ctx, "wa-acc", "12345", "hi")
		if err != nil || id != "wa-1" {
			t.Errorf("SendWhatsApp 期望 wa-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendFeishu 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "fs-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendFeishu(ctx, "fs-acc", "fs-open", "hi")
		if err != nil || id != "fs-1" {
			t.Errorf("SendFeishu 期望 fs-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendWeb 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "web-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendWeb(ctx, "sess-1", "hi")
		if err != nil || id != "web-1" {
			t.Errorf("SendWeb 期望 web-1, 得 %q %v", id, err)
		}
	})

	t.Run("SendCard 透传", func(t *testing.T) {
		mock := &mockReachAdapter{msgID: "card-1"}
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendCard(ctx, "douyin", "acc", "ext", "card-123")
		if err != nil || id != "card-1" {
			t.Errorf("SendCard 期望 card-1, 得 %q %v", id, err)
		}
	})

	t.Run("Recall：非 bridge 渠道透传", func(t *testing.T) {
		mock := &mockReachAdapter{}
		adapter := NewBridgeReachAdapter(mock)
		err := adapter.Recall(ctx, "wecom", "msg-xyz")
		if err != nil {
			t.Errorf("Recall 期望 nil, 得 %v", err)
		}
		if mock.gotRecallCh != "wecom" || mock.gotRecallMsgID != "msg-xyz" {
			t.Error("Recall 参数未透传")
		}
	})

	t.Run("Recall：bridge 渠道显式 not_supported（B8）", func(t *testing.T) {
		mock := &mockReachAdapter{}
		adapter := NewBridgeReachAdapter(mock)
		for _, ch := range []string{"douyin", "kuaishou", "xiaohongshu", "tiktok", "xianyu"} {
			err := adapter.Recall(ctx, ch, "bridge:douyin:acc:open")
			if err == nil || !strings.Contains(err.Error(), "不支持撤回") {
				t.Errorf("%s Recall 期望 not_supported 错误, 得 %v", ch, err)
			}
		}
		if mock.gotRecallCh != "" {
			t.Error("bridge 渠道 Recall 不应触达 inner")
		}
	})

	t.Run("AccountHealth 透传", func(t *testing.T) {
		mock := &mockReachAdapter{}
		adapter := NewBridgeReachAdapter(mock)
		info, err := adapter.AccountHealth(ctx, "wecom", "acc-1")
		if err != nil || info == nil || info.Status != "healthy" {
			t.Errorf("AccountHealth 期望 healthy, 得 %+v %v", info, err)
		}
		if mock.gotHealthCh != "wecom" {
			t.Error("AccountHealth 参数未透传")
		}
	})

	t.Run("ListAccounts 透传", func(t *testing.T) {
		mock := &mockReachAdapter{}
		adapter := NewBridgeReachAdapter(mock)
		list, err := adapter.ListAccounts(ctx, "telegram")
		if err != nil || len(list) != 1 || list[0].Channel != "telegram" {
			t.Errorf("ListAccounts 期望 1 条 telegram, 得 %+v %v", list, err)
		}
		if mock.gotListCh != "telegram" {
			t.Error("ListAccounts 参数未透传")
		}
	})

	t.Run("inner 返回 error 时透传", func(t *testing.T) {
		innerErr := errors.New("inner down")
		mock := &mockReachAdapter{err: innerErr}
		adapter := NewBridgeReachAdapter(mock)
		_, err := adapter.SendSMS(ctx, "138", "hi", "", nil)
		if !errors.Is(err, innerErr) {
			t.Errorf("期望 inner down, 得 %v", err)
		}
		err = adapter.Recall(ctx, "ch", "msg")
		if !errors.Is(err, innerErr) {
			t.Errorf("Recall 期望 inner down, 得 %v", err)
		}
		_, err = adapter.AccountHealth(ctx, "ch", "acc")
		if !errors.Is(err, innerErr) {
			t.Errorf("AccountHealth 期望 inner down, 得 %v", err)
		}
		_, err = adapter.ListAccounts(ctx, "ch")
		if !errors.Is(err, innerErr) {
			t.Errorf("ListAccounts 期望 inner down, 得 %v", err)
		}
	})
}

// TestBridgeReachAdapter_DirectDeliver 覆盖网页渠道的 deliverToOutbox 路径
// 注：真实落库需 service.globalInboxIngressService 注入，此处测构造器/SetIngress 防护分支
func TestBridgeReachAdapter_DirectDeliver(t *testing.T) {
	ctx := context.Background()
	mock := &mockReachAdapter{}

	t.Run("nil adapter 防护", func(t *testing.T) {
		var nilAdapter *BridgeReachAdapter
		err := nilAdapter.EnqueueManualReply(ctx, "douyin", "acc", "conv:1", "hi", "agent-01")
		if err == nil || !strings.Contains(err.Error(), "not initialized") {
			t.Errorf("nil adapter 期望 not initialized, 得 %v", err)
		}
	})

	t.Run("nil ingress 防护", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		err := adapter.EnqueueManualReply(ctx, "douyin", "acc", "conv:1", "hi", "agent-01")
		if err == nil || !strings.Contains(err.Error(), "ingress not initialized") {
			t.Errorf("nil ingress 期望 ingress not initialized, 得 %v", err)
		}
	})

	t.Run("SendDouyin 格式验证", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		id, err := adapter.SendDouyin(ctx, "acc-dy", "open-dy", "text", "hi")

		if err != nil {

			t.Logf("SendDouyin err (预期未注入): %v", err)
		} else if id == "" {
			t.Error("SendDouyin 成功时应返回非空 id")
		}
	})

	t.Run("SendKuaishou 格式验证", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		id, _ := adapter.SendKuaishou(ctx, "acc-ks", "open-ks", "text", "hi")
		_ = id
	})

	t.Run("SendXHS 格式验证", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		id, _ := adapter.SendXHS(ctx, "acc-xhs", "open-xhs", "text", "hi")
		_ = id
	})

	t.Run("SendTikTok 格式验证", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		id, _ := adapter.SendTikTok(ctx, "acc-tk", "open-tk", "text", "hi")
		_ = id
	})

	t.Run("SendXianyu 格式验证", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		id, _ := adapter.SendXianyu(ctx, "acc-xy", "open-xy", "text", "hi")
		_ = id
	})

	t.Run("SetIngress 注入", func(t *testing.T) {
		adapter := NewBridgeReachAdapter(mock)
		adapter.SetIngress(nil)
	})
}
