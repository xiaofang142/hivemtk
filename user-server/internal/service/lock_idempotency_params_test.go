package service

import (
	"strings"
	"testing"
	"time"
)

// 锁 TTL 的测试分两层：这一层是 seam 四格（值对不对），
// behavior 层在 lock_idempotency_behavior_test.go（锁真的按这个 TTL 过期）。

func TestLockIdempotencySeams(t *testing.T) {
	// 每条的「种子默认值」必须与代码兜底常量相等：参数中心默认值的语义是
	// 「没人配置时系统现在的行为」，两边不一致等于升级即改变行为。
	cases := []struct {
		name     string
		probe    func() time.Duration
		fallback time.Duration
		seed     string
		inject   time.Duration
	}{
		{"inbox_human_ttl", ProbeInboxHumanLockTTL, InboxLockTTL, "86400", 30 * time.Minute},
		{"inbox_pending_ttl", ProbeInboxPendingTTL, InboxPendingTTL, "300", 90 * time.Second},
		{"inbox_content_dedup_ttl", ProbeInboxContentDedupTTL, InboxContentDedupTTL, "300", 2 * time.Minute},
		{"ingest_lock_ttl", ProbeIngestLockTTL, IngestLockTTL, "25", 3 * time.Second},
		{"ai_processing_ttl", ProbeAIProcessingTTL, InboxAIProcessingTTL, "120", 5 * time.Minute},
		{"message_hub_idem_ttl", ProbeMessageHubIdemTTL, MessageHubDefaultIdemTTL, "86400", 45 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reset := resetLockIdempotencyProviders()
			defer reset()

			if got := c.probe(); got != c.fallback {
				t.Fatalf("未注入时应等于兜底常量 %s，实际 %s", c.fallback, got)
			}
			want, err := time.ParseDuration(c.seed + "s")
			if err != nil {
				t.Fatalf("种子默认值 %q 不是合法秒数: %v", c.seed, err)
			}
			if c.fallback != want {
				t.Errorf("种子 %s 与代码兜底 %s 不一致：升级即改变行为", want, c.fallback)
			}

			// 注入生效
			applyLockProvider(t, c.name, c.inject)
			if got := c.probe(); got != c.inject {
				t.Fatalf("注入 %s 后应读回 %s，实际 %s", c.name, c.inject, got)
			}

			// 零值必须回落兜底：duration 0 的锁语义是「永不过期」，
			// 那比任何写坏的数字都危险，所以这一格是硬要求。
			applyLockProvider(t, c.name, 0)
			if got := c.probe(); got != c.fallback {
				t.Fatalf("注入 0 应回落兜底 %s，实际 %s", c.fallback, got)
			}

			// 负值同样回落
			applyLockProvider(t, c.name, -time.Second)
			if got := c.probe(); got != c.fallback {
				t.Fatalf("注入负值应回落兜底 %s，实际 %s", c.fallback, got)
			}
		})
	}
}

// applyLockProvider 按名字注入，避免每个 case 各写一条 setter 调用。
func applyLockProvider(t *testing.T, key string, d time.Duration) {
	t.Helper()
	switch key {
	case "inbox_human_ttl":
		SetInboxHumanLockTTLProvider(func() time.Duration { return d })
	case "inbox_pending_ttl":
		SetInboxPendingTTLProvider(func() time.Duration { return d })
	case "inbox_content_dedup_ttl":
		SetInboxContentDedupTTLProvider(func() time.Duration { return d })
	case "ingest_lock_ttl":
		SetIngestLockTTLProvider(func() time.Duration { return d })
	case "ai_processing_ttl":
		SetAIProcessingTTLProvider(func() time.Duration { return d })
	case "message_hub_idem_ttl":
		SetMessageHubIdemTTLProvider(func() time.Duration { return d })
	default:
		t.Fatalf("未知 seam %q", key)
	}
}

// resetLockIdempotencyProviders 把六个 provider 摘回「未注入」状态并返回复位函数。
// setter 的 nil 语义是不注入而不是复位（见 config_param_seam 里那条通用约定），
// 所以复位只能同包直接写 provider 变量。
func resetLockIdempotencyProviders() func() {
	saved := [](func() time.Duration){
		inboxHumanLockTTLProvider, inboxPendingTTLProvider, inboxContentDedupTTLProvider,
		ingestLockTTLProvider, aiProcessingTTLProvider, messageHubIdemTTLProvider,
	}
	inboxHumanLockTTLProvider = func() time.Duration { return InboxLockTTL }
	inboxPendingTTLProvider = func() time.Duration { return InboxPendingTTL }
	inboxContentDedupTTLProvider = func() time.Duration { return InboxContentDedupTTL }
	ingestLockTTLProvider = func() time.Duration { return IngestLockTTL }
	aiProcessingTTLProvider = func() time.Duration { return InboxAIProcessingTTL }
	messageHubIdemTTLProvider = func() time.Duration { return MessageHubDefaultIdemTTL }
	return func() {
		inboxHumanLockTTLProvider, inboxPendingTTLProvider, inboxContentDedupTTLProvider,
			ingestLockTTLProvider, aiProcessingTTLProvider, messageHubIdemTTLProvider =
			saved[0], saved[1], saved[2], saved[3], saved[4], saved[5]
	}
}

func TestLockIdempotencySetterNilIsNoop(t *testing.T) {
	reset := resetLockIdempotencyProviders()
	defer reset()

	// 先注入一个非兜底值，再传 nil：若 nil 被当成「复位」这里会变回 24h。
	SetInboxHumanLockTTLProvider(func() time.Duration { return 42 * time.Minute })
	SetInboxHumanLockTTLProvider(nil)
	if got := ProbeInboxHumanLockTTL(); got != 42*time.Minute {
		t.Fatalf("setter 传 nil 应为空操作，保持 42m，实际 %s", got)
	}
}

// TestLockGroupSeedDefaultsPassValidation 每条种子的默认值必须落在自己的
// min/max 区间内。ResetToDefault / BulkResetGroup 会把 DefaultValue 写回参数
// 行，越界种子等于埋雷；validateValue 那个包级函数就是两端共用的唯一口径
// （0.2 起读写两侧都走它），这里直接调它而不绕 service。
func TestLockGroupSeedDefaultsPassValidation(t *testing.T) {
	for _, def := range DefaultParamDefs() {
		if def.Group != "lock" {
			continue
		}
		if err := validateValue(def.ValueType, def.DefaultValue, def.Min, def.Max); err != nil {
			t.Errorf("lock.%s 默认值 %q 过不了自身校验: %v", def.Key, def.DefaultValue, err)
		}
		// 默认值还必须等于代码兜底常量：参数中心默认值的语义是「没人配置时
		// 系统现在的行为」，两边不一致等于升级即改变行为。
		// ai_lock_ttl 是未接线项，没有代码常量可比，只要求它仍是未接线标注。
		var got time.Duration
		switch def.Key {
		case "inbox_human_ttl":
			got = InboxLockTTL
		case "inbox_pending_ttl":
			got = InboxPendingTTL
		case "inbox_content_dedup_ttl":
			got = InboxContentDedupTTL
		case "ingest_lock_ttl":
			got = IngestLockTTL
		case "ai_processing_ttl":
			got = InboxAIProcessingTTL
		case "message_hub_idem_ttl":
			got = MessageHubDefaultIdemTTL
		case "ai_lock_ttl":
			if !strings.Contains(def.Name, "未接线") {
				t.Errorf("lock.ai_lock_ttl 的实现仍是死代码，Name 应保留未接线标注，实际 %q", def.Name)
			}
			continue
		}
		if want := seedDurationToConst(def.DefaultValue); got != want {
			t.Errorf("lock.%s 种子默认 %s ≠ 代码兜底 %s", def.Key, want, got)
		}
	}
}

func seedDurationToConst(raw string) time.Duration {
	secs, err := parseDurationSeconds(raw)
	if err != nil {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// TestLockGroupKeysAreRegisteredAsExpected 反向钉住这一组登记的键集合：
// 新增键要在这里显式现身，防止「加了种子但忘了接线」静悄悄发生。
func TestLockGroupKeysAreRegisteredAsExpected(t *testing.T) {
	want := map[string]bool{
		"inbox_human_ttl":         true,
		"inbox_pending_ttl":       true,
		"inbox_content_dedup_ttl": true,
		"ingest_lock_ttl":         true,
		"ai_processing_ttl":       true,
		"message_hub_idem_ttl":    true,
		"ai_lock_ttl":             true, // 未接线，如实保留标注
	}
	var got []string
	for _, def := range DefaultParamDefs() {
		if def.Group == "lock" {
			got = append(got, def.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("lock 组应有 %d 条，实际 %d 条: %v", len(want), len(got), got)
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("意外的 lock 组键 %q（若是新增，请同步接线与用例）", k)
		}
	}
}
