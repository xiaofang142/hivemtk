package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"gorm.io/datatypes"
	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"

	"hivemtk-user/internal/browser_automation/repository"
)

// readSrc 读取本目录/相对路径源码（静态契约断言用，风格对齐 service_test.go 的 os.ReadFile）
func readSrc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Skipf("源码 %s 不可读（目录结构变化？）: %v", rel, err)
	}
	return string(b)
}

// fakeSessionRepo BrowserSessionRepository 最小假实现：只认 UpdateExtractedData（mergeExtract 测试面）
type fakeSessionRepo struct {
	repository.BrowserSessionRepository // 嵌入接口获得全部方法签名（未覆盖调用=nil panic，测试不触达）
	extracted                           map[uint][]byte
}

func (f *fakeSessionRepo) UpdateExtractedData(_ context.Context, id uint, data repository.ExtractedJSON) error {
	if f.extracted == nil {
		f.extracted = map[uint][]byte{}
	}
	f.extracted[id] = append([]byte(nil), data...)
	return nil
}

// F2②（G11 正确版）回归：post_comment 三段式 prep→send→verify(finalize) 的契约锁。
// 手法沿用本包既有测试风格：纯函数 + 源码静态约束（finalize 走真机需要 Host 在线，
// HostRegistry.Request 依赖真实 websocket 连接，此处不做网络桩）。

// 1) 编排顺序契约：dispatchStep 的 post_comment 分支必须 prep→send→finalize，
// 且全文件不得再有一站式 postComment 调用（双路径分叉=红线）。
func TestPostCommentThreePhaseOrder(t *testing.T) {
	src := readSrc(t, "executor.go")
	iPrep := strings.Index(src, "e.hand.commentPrep(")
	iSend := strings.Index(src, "e.hand.commentSend(")
	iFin := strings.Index(src, "e.finalizeComment(")
	if iPrep < 0 || iSend < 0 || iFin < 0 {
		t.Fatalf("三段式子调用缺失: prep=%d send=%d finalize=%d", iPrep, iSend, iFin)
	}
	if !(iPrep < iSend && iSend < iFin) {
		t.Errorf("编排顺序必须 prep→send→finalize，got prep=%d send=%d finalize=%d", iPrep, iSend, iFin)
	}
	// 一站式 postComment 调用必须从 executor.go 消失（Hand 方法即使保留也不许被执行路径引用）
	if strings.Contains(src, ".postComment(") {
		t.Error("executor.go 仍引用一站式 postComment（双路径分叉，违反 F2② 单一路径原则）")
	}
}

// 2) finalize 只读轮询契约：finalizeComment 循环体内只允许 commentVerify（只读），
// 绝不允许出现 commentSend/commentPrep（验证态任何重提交=双发红线 G11）。
func TestFinalizeNeverResubmits(t *testing.T) {
	src := readSrc(t, "executor.go")
	start := strings.Index(src, "func (e *Executor) finalizeComment")
	if start < 0 {
		t.Fatal("finalizeComment 不存在")
	}
	end := strings.Index(src[start:], "\nfunc ")
	body := src[start:]
	if end > 0 {
		body = src[start : start+end]
	}
	if strings.Contains(body, "commentSend(") || strings.Contains(body, "commentPrep(") {
		t.Error("finalize 轮询体内出现提交类调用（双发风险）")
	}
	if !strings.Contains(body, "commentVerify(") {
		t.Error("finalize 必须以 commentVerify 为轮询单元")
	}
	// 可中断：轮询必须检查 ctx 与 stopCh
	if !strings.Contains(body, "ctx.Err()") || !strings.Contains(body, "stopFired") {
		t.Error("finalize 轮询必须 ctx/stopCh 双可中断")
	}
}

// 3) stopChFor：注册后可取回同一通道；注销后返回 nil（nil channel 接收永久阻塞，调用方已判空）
func TestStopChForLifecycle(t *testing.T) {
	e := &Executor{stopRegistry: make(map[uint]chan struct{})}
	if e.stopChFor(9) != nil {
		t.Error("未注册 session 应返回 nil")
	}
	ch := e.registerStop(9)
	got := e.stopChFor(9)
	if got == nil {
		t.Fatal("注册后应可取回通道")
	}
	if got != ch {
		t.Error("取回的必须与注册的同一通道")
	}
	e.unregisterStop(9)
	if e.stopChFor(9) != nil {
		t.Error("注销后应返回 nil")
	}
}

// 4) mergeExtract：key 非空嵌套追加数组（多次评论各留证据）；key 空顶层合并（extract 原语义）
func TestMergeExtractSemantics(t *testing.T) {
	repo := &fakeSessionRepo{}
	e := &Executor{sessionRepo: repo}
	s := &model.BrowserSession{ID: 1}

	e.mergeExtract(context.Background(), s, "post_comment", map[string]any{"text": "a"})
	e.mergeExtract(context.Background(), s, "post_comment", map[string]any{"text": "b"})
	var m map[string]any
	if err := json.Unmarshal(s.ExtractedData, &m); err != nil {
		t.Fatal(err)
	}
	arr, ok := m["post_comment"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("post_comment 证据应累加为 2 条数组: %s", s.ExtractedData)
	}

	// 顶层合并语义（extract 路径）：key 覆盖，其它键保留
	e.mergeExtract(context.Background(), s, "", map[string]any{"k": "v"})
	if err := json.Unmarshal(s.ExtractedData, &m); err != nil {
		t.Fatal(err)
	}
	if m["k"] != "v" || m["post_comment"] == nil {
		t.Errorf("顶层合并丢失既有键: %s", s.ExtractedData)
	}

	// 非法既有 JSON（存量脏数据）不 panic、可恢复为干净 map
	s.ExtractedData = datatypes.JSON(`{broken`)
	e.mergeExtract(context.Background(), s, "", map[string]any{"x": 1})
	if err := json.Unmarshal(s.ExtractedData, &m); err != nil || m["x"] == nil {
		t.Errorf("脏数据恢复失败: %s err=%v", s.ExtractedData, err)
	}
}

// 5) R26-2 归因三分：注入竞速超时=点击未发生（早返不进 finalize）；
// WS 超时=结果未知（进 finalize 回查）；普通错误同理。静态契约+纯函数双锁。
func TestInjectTimeoutAttribution(t *testing.T) {
	if !isInjectTimeout(errors.New("comment_send_inject_timeout_15000ms")) {
		t.Error("扩展竞速错误必须识别")
	}
	if isInjectTimeout(errors.New("Host 命令超时（45s，action=comment_send）")) {
		t.Error("WS 超时=结果未知，不得误判为未发生")
	}
	if isInjectTimeout(nil) || isInjectTimeout(errors.New("send_button_not_found")) {
		t.Error("nil/业务错误不得识别为注入超时")
	}
	// dispatchStep 里 send 路径：注入超时早返，其余进 finalize
	src := readSrc(t, "executor.go")
	iSend := strings.Index(src, "e.hand.commentSend(")
	iFin := strings.Index(src, "e.finalizeComment(")
	iGuard := strings.Index(src, "isInjectTimeout(sendErr)")
	if !(iSend < iGuard && iGuard < iFin) {
		t.Errorf("归因顺序必须 send→超时判定→finalize，got send=%d guard=%d finalize=%d", iSend, iGuard, iFin)
	}
}

// 6) dto/提示词口径：post_comment 仍是对外动作名（三段式是内部实现，不扩 dto 枚举），
// 且 Brain 提示词描述与三段式语义一致（防文档性谎言）。
func TestPostCommentPublicContractUnchanged(t *testing.T) {
	src := readSrc(t, "../dto/task.go")
	if !strings.Contains(src, "post_comment") {
		t.Error("dto StepItem 枚举必须仍含 post_comment")
	}
	for _, sub := range []string{"comment_prep", "comment_send", "comment_verify"} {
		if strings.Contains(src, sub) {
			t.Errorf("dto 不应暴露内部子命令 %s（三段式是扩展协议，不是编排动作）", sub)
		}
	}
	p := readSrc(t, "brain_prompts.go")
	if !strings.Contains(p, "提交与验证分离") && !strings.Contains(p, "finalize") {
		t.Error("brain_prompts 对 post_comment 的描述应体现三段式/finalize 语义")
	}
	// 能力矩阵诚实性：CommentLocatorsFor 未声明平台仍 fails-loudly
	if _, err := platform.CommentLocatorsFor(context.Background(), "nonexistent"); err == nil {
		t.Error("未注册平台必须报错")
	}
	_ = dto.StepItem{}
}
