// 批9a 假绿收口（服务端侧）：open_tab/markdown 的回包事实必须原样落进步骤结果。
// 立项依据是真机 session=432：扩展 open_tab 未等加载就读到空 DOM，回包只有 chrome_tab_id，
// 服务端据此判这一步成功——「读到了什么」这件事在审计面上完全不存在。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

func b9aStepResult(t *testing.T, rows []*model.BrowserStep, idx int) map[string]any {
	t.Helper()
	for _, r := range rows {
		if r.StepIndex != idx {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(r.Result, &m); err != nil {
			t.Fatalf("步 %d result 解析失败: %v（raw=%s）", idx, err, string(r.Result))
		}
		return m
	}
	t.Fatalf("找不到第 %d 步的结果行", idx)
	return nil
}

func TestB9A_OpenTabRecordsLoadTruthfulness(t *testing.T) {
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/"},{"action":"markdown"}]`
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		switch action {
		case "open_tab":
			return map[string]any{"chrome_tab_id": 888, "loaded": true, "load_wait_ms": 42, "title": "大页夹具"}, ""
		case "markdown":
			return map[string]any{"markdown": "# 标题\n\n正文", "markdown_chars": 9, "full_chars": 70000,
				"truncated": true, "content_empty": false}, ""
		}
		return map[string]any{}, ""
	})
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	open := b9aStepResult(t, bundle.steps(t, session.ID), 0)
	if open["page_loaded"] != true {
		t.Errorf("open_tab 步未记下 page_loaded=%v（没加载完这一步必须能被审出来）", open["page_loaded"])
	}
	if open["chrome_tab_id"] != float64(888) {
		t.Errorf("chrome_tab_id=%v want 888", open["chrome_tab_id"])
	}

	md := b9aStepResult(t, bundle.steps(t, session.ID), 1)
	if md["truncated"] != true {
		t.Errorf("markdown 步未如实记 truncated=%v", md["truncated"])
	}
	// 字符数口径必须来自扩展：Go 的 len() 是字节数，中文差 3 倍
	if md["markdown_chars"] != float64(9) || md["full_chars"] != float64(70000) {
		t.Errorf("markdown 字符口径被改写: chars=%v full=%v want 9/70000", md["markdown_chars"], md["full_chars"])
	}
	// content_empty 同理：这一步到底读没读到东西，审计面上必须与扩展侧一致
	if md["content_empty"] != false {
		t.Errorf("content_empty=%v，扩展侧报 false 时必须原样落库", md["content_empty"])
	}
}

// 老 Host 不回 loaded 时只能记 null：把「不知道」写成 false 会造出一批假红任务，
// 写成 true 则回到本批要消灭的假绿。
func TestB9A_UnknownLoadedStaysNull(t *testing.T) {
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/"}]`
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		return map[string]any{"chrome_tab_id": 991}, ""
	})
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	rows := bundle.steps(t, session.ID)
	open := b9aStepResult(t, rows, 0)
	v, ok := open["page_loaded"]
	if !ok {
		t.Fatal("page_loaded 键缺失：老 Host 也要留下这一问，不能整字段消失")
	}
	if v != nil {
		t.Errorf("page_loaded=%v，未知必须是 null（既非 true 也非 false）", v)
	}
}
