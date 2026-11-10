package service

import "testing"

// 2026-11-10 审计 F1：内容窗口去重键的哈希输入必须并入媒体维度。
//
// 修复前：interceptInbound 的 Redis 键 = ContentHashWithSender(channel, sender, content)，
// 媒体消息 content 同为「[图片]」占位符 ⇒ 同一客户在同一会话 5 分钟窗口内连发的
// 两张不同图 dupKey 完全相同，第二张被判 duplicate 落库留痕但 AI 永远看不到。
// 本组用例钉住 MediaAwareDedupInput 的口径（Redis 键与 dedup_hash 列共用该函数）：
//   1. 文本/空类型/system/recall 与旧口径逐字节一致（存量行为不变）；
//   2. 同占位符、不同 media_url 的两张图输入不同（修复点）；
//   3. 同图重发输入相同（幂等仍有效）；
//   4. 类型不同（图 vs 同 URL 视频）输入不同；
//   5. 分隔符 \x1f 不出现在正文侧 ——「正文尾拼类型首」不会伪造出另一条消息的输入。
func TestMediaAwareDedupInput(t *testing.T) {
	// 1) 文本/空类型/system/recall 与旧口径逐字节一致
	if got := MediaAwareDedupInput("text", "", "在吗"); got != "在吗" {
		t.Fatalf("text 类型必须与旧口径逐字节一致: got %q", got)
	}
	if got := MediaAwareDedupInput("", "", "在吗"); got != "在吗" {
		t.Fatalf("空类型按 text 处理: got %q", got)
	}
	if got := MediaAwareDedupInput("TEXT", "", " 在吗 "); got != "在吗" {
		t.Fatalf("大小写不敏感 + trim: got %q", got)
	}
	if got := MediaAwareDedupInput("system", "", "你已添加对方为好友"); got != "你已添加对方为好友" {
		t.Fatalf("system 走旧口径: got %q", got)
	}
	if got := MediaAwareDedupInput("recall", "", "撤回了一条消息"); got != "撤回了一条消息" {
		t.Fatalf("recall 走旧口径: got %q", got)
	}

	// 2) 同占位符、不同 media_url 的两张图输入不同（修复点）
	imgA := MediaAwareDedupInput("image", "https://f/x1.png", "[图片]")
	imgB := MediaAwareDedupInput("image", "https://f/x2.png", "[图片]")
	if imgA == imgB {
		t.Fatalf("同会话两张不同图必须输入不同（修复点）: %q", imgA)
	}

	// 3) 同图重发输入相同（幂等前提）
	if got := MediaAwareDedupInput("image", "https://f/x1.png", "[图片]"); got != imgA {
		t.Fatalf("同一条图重发输入必须稳定: %q vs %q", got, imgA)
	}

	// 4) 类型参与输入
	vid := MediaAwareDedupInput("video", "https://f/x1.png", "[图片]")
	if vid == imgA {
		t.Fatal("类型必须参与输入")
	}

	// 5) 分隔符歧义防御：正文 A 尾 + 类型 B 首 ≠ 正文 AB
	withTail := MediaAwareDedupInput("image", "u2", "[图片]\x1f")
	if withTail == MediaAwareDedupInput("image\x1fimage", "u2", "[图片]") {
		t.Fatal("分隔符两侧内容组合不得互相伪造")
	}
}
