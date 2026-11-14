package service

// 自回显字符多重集判据（2026-11-14 生产实证 4836 回归钉）。
//
// 事故：抖音 DOM 把自家出站气泡抓成**交错拼接**（入站 204 字 ≈ 出站 107 字的字符
// 交错双写），与等值/包含/尾部三类既有判据全部错位（等值不等、包含不连、剥后非空），
// 假入站落库。本判据与顺序无关：同频必拦、纯双倍频必拦。纯函数测试（无 DB）。

import "testing"

func TestSortedRunes(t *testing.T) {
	if got := sortedRunes("cba"); got != "abc" {
		t.Errorf("sortedRunes(cba) = %q, want abc", got)
	}
	if got := sortedRunes(normalizeEchoText(" 你 好 ")); got != "你好" {
		t.Errorf("空白归一后排序 = %q, want 你好", got)
	}
}

func TestDoubleSorted(t *testing.T) {
	if got := doubleSorted("ab"); got != "aabb" {
		t.Errorf("doubleSorted(ab) = %q, want aabb", got)
	}
}

func TestEchoMultiset_CatchesInterleavedDoubleWrite(t *testing.T) {
	// 生产实证形态：出站 T=107 字，入站=两份 T 交错拼接（含连接空白已归一）
	const T = "在的～这是HiveMTK一套AI私域营销中台抖音小红书微博等十三渠道的私信统一进一个收件箱"
	interleaved := ""
	for i := 0; i < len([]rune(T)); i++ {
		interleaved += string([]rune(T)[i])
	}
	// 构造交错双写：字符序列 T 的每个字符后紧跟另一份的同位字符
	dbl := ""
	rs := []rune(T)
	for i := range rs {
		dbl += string(rs[i]) + string(rs[i])
	}
	_ = interleaved
	inbound := normalizeEchoText(dbl)
	outbound := normalizeEchoText(T)
	if inbound == outbound {
		t.Fatal("测试构造失效：交错双写不应等于原串")
	}
	// 判据断言
	inS, outS := sortedRunes(inbound), sortedRunes(outbound)
	if len([]rune(outbound)) < 16 {
		t.Fatalf("出站构造过短，护栏应跳过——测试构造错误")
	}
	if inS != outS && inS != doubleSorted(outS) {
		t.Errorf("交错双写应被多重集判据命中：in=%d out=%d", len(inS), len(outS))
	}
}

func TestEchoMultiset_DoesNotSwallowRealCustomerMessage(t *testing.T) {
	// 真实客户消息（与出站不同内容）不得被误吞
	outbound := normalizeEchoText("在的～这是HiveMTK一套AI私域营销中台抖音小红书微博等十三渠道的私信统一进一个收件箱")
	inbound := normalizeEchoText("我想知道你们的价格和部署方式，另外售后怎么算")
	inS, outS := sortedRunes(inbound), sortedRunes(outbound)
	if inS == outS || inS == doubleSorted(outS) {
		t.Error("不同内容被误判为回声——判据过宽")
	}
	// 客户引用我们整句话再追问：inbound = T + "那价格呢" —— 非同频非双倍，放行 ✓
	quoted := normalizeEchoText("在的～这是HiveMTK一套AI私域营销中台抖音小红书微博等十三渠道的私信统一进一个收件箱那价格呢")
	qS := sortedRunes(quoted)
	if qS == outS || qS == doubleSorted(outS) {
		t.Error("引用+追问场景被误吞——16 字护栏/频率判据过宽")
	}
}
