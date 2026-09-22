package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// officialEventID 按渠道取「官方文档明示的重复投递判定键」组装入站幂等 ID。
//
//	telegram  update_id                    （getUpdates / webhook 均以 update_id 判重）
//	qq        信封 id                       （回调重投同一条事件时 id 不变）
//	飞书       header.event_id (v2) / uuid (v1)
//	企微       MsgId
//	whatsapp  本批全部 wamid 的集合指纹
//
// 返回带「渠道+账号」前缀的键；取不到官方键时返回空串，由调用方退回整包
// body 哈希（webhook.go generateEventID）。
//
// 账号前缀不是可选的装饰：webhook_events.event_id 是**全局**唯一索引且不带
// platform/account 列，裸官方键会让两个 bot 各自的 #1 消息互相吞掉。
//
// 该函数只在 Receive 入口对明文生效：飞书/企微开启加解密后事件体在密文里，
// 官方键要到 dispatch 阶段解密后才可见，那两层各自依赖 message_hub 的
// (platform,msg_id) 唯一索引兜底（见审计文档 §5 S-04）。
func officialEventID(channel WebhookChannel, accountID string, raw []byte) string {
	// 企微的外壳可能是官方 <xml>（审计 N-08）：这里如果用 JSON-only 的解法，
	// 明文 XML 回调会在「Receive 层第一道去重」丢掉 MsgId，只剩整包哈希兜底。
	// 其余渠道维持原 JSON 口径不变。
	var doc map[string]any
	if channel == ChannelWeCom {
		doc = wecomEnvelopeMap(raw)
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	if doc == nil {
		return ""
	}
	scope := string(channel) + ":" + accountID + ":"
	switch channel {
	case ChannelTelegram:
		// update_id 官方为 JSON 数字，getString 只认字符串，故单独取数。
		if v, ok := jsonInt64(doc["update_id"]); ok {
			return scope + "upd-" + strconv.FormatInt(v, 10)
		}
	case ChannelQQ:
		if v := getString(doc, "id"); v != "" {
			return scope + "evt-" + v
		}
	case ChannelFeishu:
		if header, ok := doc["header"].(map[string]any); ok {
			if v := getString(header, "event_id"); v != "" {
				return scope + v
			}
		}
		if v := getString(doc, "uuid"); v != "" {
			return scope + v
		}
	case ChannelWeCom:
		if v := getString(doc, "MsgId", "msg_id"); v != "" {
			return scope + v
		}
	case ChannelWhatsapp:
		// 一次回调可携带多条 messages，单取首条会让「A」与「A+B」撞键；
		// 用排序后的 wamid 集合指纹，既与顺序无关又不会过度合并。
		if ids := whatsappWAMIDs(doc); len(ids) > 0 {
			sort.Strings(ids)
			sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
			return scope + "wamid-" + hex.EncodeToString(sum[:8])
		}
	}
	return ""
}

// jsonInt64 容忍 JSON 数字（float64）、json.Number 与字符串三种形态。
func jsonInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i <= 0 {
			return 0, false
		}
		return i, true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil || i <= 0 {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// whatsappWAMIDs 收集 entry[].changes[].value 下 messages/statuses 的官方消息 id (wamid)。
func whatsappWAMIDs(doc map[string]any) []string {
	var ids []string
	for _, entryAny := range jsonArray(doc["entry"]) {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			continue
		}
		for _, changeAny := range jsonArray(entry["changes"]) {
			change, ok := changeAny.(map[string]any)
			if !ok {
				continue
			}
			value, ok := change["value"].(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"messages", "statuses"} {
				for _, itemAny := range jsonArray(value[key]) {
					if item, ok := itemAny.(map[string]any); ok {
						if id := getString(item, "id"); id != "" {
							ids = append(ids, id)
						}
					}
				}
			}
		}
	}
	return ids
}

func jsonArray(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}
