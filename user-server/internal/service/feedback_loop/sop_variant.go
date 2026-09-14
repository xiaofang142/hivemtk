package feedbackloop

import (
	"hivemtk-user/internal/model"
)

// SOPGraphMutatorForExperiment 返回实验标签对应的图变异器（纯函数，不可变入参）
func SOPGraphMutatorForExperiment(experimentTag string) func(graph model.JSONMap) model.JSONMap {
	return func(graph model.JSONMap) model.JSONMap {
		return mutateSOPGraph(graph, experimentTag)
	}
}

func mutateSOPGraph(graph model.JSONMap, experimentTag string) model.JSONMap {
	if graph == nil {
		return nil
	}
	nodesKey := ""
	for _, key := range []string{"nodes", "steps", "node_list"} {
		if _, ok := graph[key]; ok {
			nodesKey = key
			break
		}
	}
	if nodesKey == "" {
		return nil
	}
	raw, ok := graph[nodesKey].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}

	nodes := make([]any, len(raw))
	copy(nodes, raw)
	structChanged := false

	nodeMap := func(i int) map[string]any {
		if i < 0 || i >= len(nodes) {
			return nil
		}
		m, _ := nodes[i].(map[string]any)
		return m
	}
	nodeType := func(m map[string]any) string {
		s, _ := m["type"].(string)
		return s
	}
	rewireNext := func(fromID string, toIdx int) {

		if toIdx < 0 || toIdx >= len(nodes) {
			return
		}
		tm, _ := nodes[toIdx].(map[string]any)
		if tm == nil {
			return
		}
		toID, _ := tm["id"].(string)
		if toID == "" {
			return
		}
		for _, item := range nodes {
			fm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			fid, _ := fm["id"].(string)
			if fid == "" || fid != fromID {
				continue
			}
			nextIDs := make([]any, 0, 1)
			if s, ok := fm["next"].(string); ok && s != "" {
				nextIDs = append(nextIDs, s)
			} else if arr, ok := fm["next"].([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok && s != toID {
						nextIDs = append(nextIDs, s)
					}
				}
			}
			nextIDs = append(nextIDs, toID)
			fm["next"] = nextIDs
			return
		}
	}

	switch experimentTag {
	case "branch_prune":
		if len(nodes) <= 3 {
			return nil
		}
		worstIdx := -1
		for i := 1; i < len(nodes)-1; i++ {
			m := nodeMap(i)
			if m == nil {
				continue
			}
			if t := nodeType(m); t != "action" && t != "message" {
				continue
			}
			worstIdx = i
			break
		}
		if worstIdx == -1 {
			return nil
		}
		prunedID, _ := nodeMap(worstIdx)["id"].(string)
		prevID, _ := nodeMap(worstIdx - 1)["id"].(string)
		rewireNext(prevID, worstIdx+1)

		nodes = append(nodes[:worstIdx], nodes[worstIdx+1:]...)
		removeNextRef(nodes, prunedID)
		structChanged = true

	case "add_objection":
		insertAfter := -1
		for i := 0; i < len(nodes); i++ {
			m := nodeMap(i)
			if m == nil {
				continue
			}
			if nodeType(m) == "start" {
				continue
			}
			insertAfter = i
			break
		}
		if insertAfter == -1 {
			return nil
		}
		objNode := map[string]any{
			"id":     "obj_auto_1",
			"type":   "message",
			"name":   "异议处理分支(AI优化)",
			"prompt": "客户如有顾虑，先复述认同（LAER-Acknowledge），再给证据与案例，最后邀请提问。",
		}
		prevID, _ := nodeMap(insertAfter)["id"].(string)
		nextAfter, _ := nodeMap(insertAfter)["next"]
		nodes = append(nodes[:insertAfter+1], append([]any{objNode}, nodes[insertAfter+1:]...)...)
		objNode["next"] = nextAfter
		rewireNext(prevID, insertAfter+1)
		structChanged = true

	case "timing_adjust":
		found := false
		for i := 0; i < len(nodes) && !found; i++ {
			m := nodeMap(i)
			if m == nil || nodeType(m) != "wait" {
				continue
			}
			cfg, ok := m["config"].(map[string]any)
			if !ok {
				cfg = map[string]any{}
			}
			sec, _ := cfg["wait_seconds"].(float64)
			if sec <= 0 {
				sec = 3600
			}
			half := sec / 2
			if half < 60 {
				half = 60
			}
			cfg["wait_seconds"] = half
			m["config"] = cfg
			found = true
		}
		if !found {
			return nil
		}

	case "add_empathy":
		found := false
		for i := 0; i < len(nodes) && !found; i++ {
			m := nodeMap(i)
			if m == nil {
				continue
			}
			if t := nodeType(m); t != "llm" && t != "message" {
				continue
			}
			prompt, _ := m["prompt"].(string)
			m["prompt"] = prompt + "\n[共情补充] 回复开头先一句话复述客户的感受，表达理解，再进入正题。"
			found = true
		}
		if !found {
			return nil
		}

	case "node_merge":
		merged := false
		for i := 0; i < len(nodes)-1; i++ {
			if nodeType(nodeMap(i)) != "message" || nodeType(nodeMap(i+1)) != "message" {
				continue
			}
			mergedID, _ := nodeMap(i + 1)["id"].(string)

			nextOfSecond := nodeMap(i + 1)["next"]
			nodeMap(i)["next"] = nextOfSecond
			nodes = append(nodes[:i+1], nodes[i+2:]...)
			removeNextRef(nodes, mergedID)
			merged = true
			structChanged = true
			break
		}
		if !merged {
			return nil
		}

	default:
		return nil
	}

	if !structChanged && experimentTag != "timing_adjust" && experimentTag != "add_empathy" {
		return nil
	}

	out := make(model.JSONMap, len(graph))
	for k, v := range graph {
		out[k] = v
	}
	out[nodesKey] = nodes
	variables := map[string]any{"variant_tag": experimentTag}
	if rawVars, ok := out["variables"].(map[string]any); ok {
		for k, v := range rawVars {
			variables[k] = v
		}
	}
	out["variables"] = variables
	return out
}

// SOPGraphMutatorForNodePrompt 返回将指定节点 prompt 替换为 newPrompt 的图变异器
//
// 用于 prompt_rewrite 建议：优先精确匹配 nodeID；找不到时回退首个 llm/message 节点。
// 两者都不可得时返回 nil（降级原样克隆，由 gate 验证门兜底）。
func SOPGraphMutatorForNodePrompt(nodeID, newPrompt string) func(graph model.JSONMap) model.JSONMap {
	return func(graph model.JSONMap) model.JSONMap {
		if graph == nil || newPrompt == "" {
			return nil
		}
		nodesKey := ""
		for _, key := range []string{"nodes", "steps", "node_list"} {
			if _, ok := graph[key]; ok {
				nodesKey = key
				break
			}
		}
		if nodesKey == "" {
			return nil
		}
		raw, ok := graph[nodesKey].([]any)
		if !ok || len(raw) == 0 {
			return nil
		}
		nodes := make([]any, len(raw))
		copy(nodes, raw)

		targetIdx := -1
		fallbackIdx := -1
		for i, item := range nodes {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if (t == "llm" || t == "message") && fallbackIdx == -1 {
				fallbackIdx = i
			}
			id, _ := m["id"].(string)
			if id != "" && id == nodeID {
				targetIdx = i
				break
			}
		}
		if targetIdx == -1 {
			targetIdx = fallbackIdx
		}
		if targetIdx == -1 {
			return nil
		}
		m, ok := nodes[targetIdx].(map[string]any)
		if !ok {
			return nil
		}
		m["prompt"] = newPrompt

		out := make(model.JSONMap, len(graph))
		for k, v := range graph {
			out[k] = v
		}
		out[nodesKey] = nodes
		variables := map[string]any{"variant_tag": "prompt_rewrite", "prompt_rewritten_node": nodeID}
		if rawVars, ok := out["variables"].(map[string]any); ok {
			for k, v := range rawVars {
				variables[k] = v
			}
		}
		out["variables"] = variables
		return out
	}
}

// ExtractNodePrompt 从 SOPGraph 中提取指定节点的当前 prompt
//
// 优先精确匹配 nodeID；找不到时回退首个 llm/message 节点。
// 返回 ok=false 表示图中无可用 prompt（调用方应 fail-closed，不得用建议文本顶替）。
func ExtractNodePrompt(graph model.JSONMap, nodeID string) (string, bool) {
	if graph == nil {
		return "", false
	}
	nodesKey := ""
	for _, key := range []string{"nodes", "steps", "node_list"} {
		if _, ok := graph[key]; ok {
			nodesKey = key
			break
		}
	}
	if nodesKey == "" {
		return "", false
	}
	raw, ok := graph[nodesKey].([]any)
	if !ok || len(raw) == 0 {
		return "", false
	}
	fallback := ""
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if t != "llm" && t != "message" {
			continue
		}
		prompt, _ := m["prompt"].(string)
		if fallback == "" {
			fallback = prompt
		}
		id, _ := m["id"].(string)
		if nodeID != "" && id == nodeID {
			return prompt, true
		}
	}
	if fallback != "" {
		return fallback, true
	}
	return "", false
}

func removeNextRef(nodes []any, targetID string) {
	if targetID == "" {
		return
	}
	for _, item := range nodes {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arr, ok := m["next"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok && s == targetID {
				continue
			}
			kept = append(kept, v)
		}
		if len(kept) == 0 {
			delete(m, "next")
		} else {
			m["next"] = kept
		}
	}
}
