package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
)

// faWecomCSServer 按请求游标返回两页合成消息：
//   - cursor "" → 页1: m1,m2, next=c1, has_more=1
//   - cursor "c1" → 页2: m3,m4, next=c2, has_more=0
//   - 其它 → 空页
type faWecomCSServer struct {
	tokenCalls int
	pageCalls  map[string]int
}

func newFAWecomCSServer() (*httptest.Server, *faWecomCSServer) {
	state := &faWecomCSServer{pageCalls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			state.tokenCalls++
			fmt.Fprint(w, `{"errcode":0,"access_token":"tok","expires_in":7200}`)
		case "/cgi-bin/kf/sync_msg":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			var req struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(body, &req)
			state.pageCalls[req.Cursor]++
			var out string
			switch req.Cursor {
			case "":
				out = `{"errcode":0,"next_cursor":"c1","has_more":1,"msg_list":[` +
					`{"msgid":"m1","origin":3,"msgtype":"text","text":{"content":"a"},"external_userid":"u1"},` +
					`{"msgid":"m2","origin":3,"msgtype":"text","text":{"content":"b"},"external_userid":"u1"}` +
					`]}`
			case "c1":
				out = `{"errcode":0,"next_cursor":"c2","has_more":0,"msg_list":[` +
					`{"msgid":"m3","origin":3,"msgtype":"text","text":{"content":"c"},"external_userid":"u1"},` +
					`{"msgid":"m4","origin":3,"msgtype":"text","text":{"content":"d"},"external_userid":"u1"}` +
					`]}`
			default:
				out = `{"errcode":0,"next_cursor":"","has_more":0,"msg_list":[]}`
			}
			fmt.Fprint(w, out)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, state
}

func faWecomCSSvc(srvURL string, ingress func(context.Context, *core.InboundMessage) (*model.MessageHub, error), save func(context.Context, uint, string) error) (*wecomCSService, *wecomcs.Client) {
	w := &wecomCSService{
		clients:      map[uint]*wecomcs.Client{},
		pulling:      map[uint]struct{}{},
		ingressFn:    ingress,
		saveCursorFn: save,
	}
	client := wecomcs.NewClient(core.WithBaseURL(srvURL))
	return w, client
}

func faWecomCSAcc(cursor string) *model.WeComCSAccount {
	return &model.WeComCSAccount{ID: 7, CorpID: "corp", Secret: "sec", OpenKfID: "kf1", SyncCursor: cursor}
}

// TestFAWecomCS_CursorOnlyPastDurablePages：两页全部入站成功 → 游标推进到最后一页，
// 每条消息恰好入链一次。
func TestFAWecomCS_CursorOnlyPastDurablePages(t *testing.T) {
	srv, state := newFAWecomCSServer()
	defer srv.Close()
	var ingested []string
	w, client := faWecomCSSvc(srv.URL,
		func(_ context.Context, in *core.InboundMessage) (*model.MessageHub, error) {
			ingested = append(ingested, in.MessageID)
			return &model.MessageHub{}, nil
		},
		func(_ context.Context, _ uint, _ string) error { return nil },
	)
	acc := faWecomCSAcc("")
	if err := w.pullAccount(context.Background(), acc, client); err != nil {
		t.Fatalf("pullAccount: %v", err)
	}
	if strings.Join(ingested, ",") != "m1,m2,m3,m4" {
		t.Fatalf("入站消息 = %v, want m1,m2,m3,m4", ingested)
	}
	if acc.SyncCursor != "c2" {
		t.Fatalf("最终游标 = %q, want c2", acc.SyncCursor)
	}
	if state.pageCalls[""] != 1 || state.pageCalls["c1"] != 1 {
		t.Fatalf("分页调用 = %+v", state.pageCalls)
	}
}

// TestFAWecomCS_MiddleIngressFailureKeepsCursor：第二页 m3 入站失败 →
// 游标停在 c1（页1已耐久），不越过失败消息；下一轮仍从 c1 重拉。
func TestFAWecomCS_MiddleIngressFailureKeepsCursor(t *testing.T) {
	srv, _ := newFAWecomCSServer()
	defer srv.Close()
	var saved []string
	w, client := faWecomCSSvc(srv.URL,
		func(_ context.Context, in *core.InboundMessage) (*model.MessageHub, error) {
			if in.MessageID == "m3" {
				return nil, fmt.Errorf("ingest failed")
			}
			return &model.MessageHub{}, nil
		},
		func(_ context.Context, _ uint, cursor string) error {
			saved = append(saved, cursor)
			return nil
		},
	)
	acc := faWecomCSAcc("")
	err := w.pullAccount(context.Background(), acc, client)
	if err == nil {
		t.Fatal("第二页入站失败应返回错误")
	}
	if acc.SyncCursor != "c1" {
		t.Fatalf("失败后内存游标 = %q, want c1（不得越过失败页）", acc.SyncCursor)
	}
	if strings.Join(saved, ",") != "c1" {
		t.Fatalf("已持久化游标 = %v, want [c1]（不得写 c2）", saved)
	}
	// 重启/下一轮：从 c1 重拉，m3 恢复成功后应能推进到 c2。
	saved = nil
	w2, client2 := faWecomCSSvc(srv.URL,
		func(_ context.Context, in *core.InboundMessage) (*model.MessageHub, error) {
			return &model.MessageHub{}, nil
		},
		func(_ context.Context, _ uint, cursor string) error {
			saved = append(saved, cursor)
			return nil
		},
	)
	acc2 := faWecomCSAcc("c1")
	if err := w2.pullAccount(context.Background(), acc2, client2); err != nil {
		t.Fatalf("重拉: %v", err)
	}
	if acc2.SyncCursor != "c2" || strings.Join(saved, ",") != "c2" {
		t.Fatalf("恢复后游标 = %q saved=%v, want c2", acc2.SyncCursor, saved)
	}
}

// TestFAWecomCS_CursorWriteFailureKeepsOldCursor：游标持久化失败 →
// 返回错误且内存游标保持旧值，下一轮仍从旧游标重拉。
func TestFAWecomCS_CursorWriteFailureKeepsOldCursor(t *testing.T) {
	srv, _ := newFAWecomCSServer()
	defer srv.Close()
	w, client := faWecomCSSvc(srv.URL,
		func(_ context.Context, in *core.InboundMessage) (*model.MessageHub, error) {
			return &model.MessageHub{}, nil
		},
		func(_ context.Context, _ uint, cursor string) error {
			if cursor == "c1" {
				return fmt.Errorf("db write failed")
			}
			return nil
		},
	)
	acc := faWecomCSAcc("")
	if err := w.pullAccount(context.Background(), acc, client); err == nil {
		t.Fatal("游标写失败应返回错误")
	}
	if acc.SyncCursor != "" {
		t.Fatalf("游标写失败后内存游标 = %q, want 空（保持旧值）", acc.SyncCursor)
	}
}
