package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/utils"
)

type marketingFlowGraph struct {
	levels [][]model.FlowNode
}

// 先完整校验图，历史非法定义也不能在触发时执行到一半才失败。
func parseMarketingFlowGraph(raw string) (*marketingFlowGraph, error) {
	invalid := func(message string) (*marketingFlowGraph, error) {
		return nil, fmt.Errorf("%w: %s", utils.ErrInvalidInput, message)
	}
	if raw == "" {
		return invalid("流程定义为空")
	}
	var def model.FlowDefinition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return invalid("流程定义 JSON 解析失败")
	}
	if len(def.Nodes) == 0 {
		return invalid("流程至少需要一个节点")
	}
	nodes := make(map[string]model.FlowNode, len(def.Nodes))
	indegree := make(map[string]int, len(def.Nodes))
	hasTrigger := false
	for _, node := range def.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return invalid("节点 ID 不能为空")
		}
		if _, exists := nodes[node.ID]; exists {
			return invalid("节点 ID 重复：" + node.ID)
		}
		nodes[node.ID] = node
		indegree[node.ID] = 0
		hasTrigger = hasTrigger || node.Type == "trigger"
	}
	if !hasTrigger {
		return invalid("流程必须有触发器节点")
	}
	for _, node := range def.Nodes {
		seen := make(map[string]bool, len(node.NextNodes))
		for _, next := range node.NextNodes {
			if _, exists := nodes[next]; !exists {
				return invalid("节点引用不存在：" + next)
			}
			if seen[next] {
				return invalid("节点重复引用：" + next)
			}
			seen[next] = true
			indegree[next]++
		}
	}
	graph := &marketingFlowGraph{}
	visited := make(map[string]bool, len(nodes))
	for len(visited) < len(nodes) {
		var level []model.FlowNode
		for _, node := range def.Nodes {
			if !visited[node.ID] && indegree[node.ID] == 0 {
				level = append(level, node)
			}
		}
		if len(level) == 0 {
			return invalid("流程不能包含环")
		}
		sort.Slice(level, func(i, j int) bool { return level[i].ID < level[j].ID })
		graph.levels = append(graph.levels, level)
		for _, node := range level {
			visited[node.ID] = true
			for _, next := range node.NextNodes {
				indegree[next]--
			}
		}
	}
	return graph, nil
}

type marketingFlowNodeResult struct {
	node   model.FlowNode
	output map[string]any
	err    error
}

// 同层输入独立，结果在整层结束后由调度器按节点 ID 合并；不同值的同键写入明确失败。
func (g *marketingFlowGraph) run(ctx context.Context, input map[string]any, execute func(context.Context, model.FlowNode, map[string]any) (map[string]any, error), current func(string)) (map[string]any, error) {
	data, err := cloneMarketingFlowData(input)
	if err != nil {
		return nil, err
	}
	delete(data, "_next_node")
	delete(data, "_condition_matched")
	reachable := make(map[string]bool)
	for _, level := range g.levels {
		for _, node := range level {
			if node.Type == "trigger" {
				reachable[node.ID] = true
			}
		}
	}
	for _, level := range g.levels {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var active []model.FlowNode
		for _, node := range level {
			if reachable[node.ID] {
				active = append(active, node)
			}
		}
		inputs := make([]map[string]any, len(active))
		for i := range active {
			inputs[i], err = cloneMarketingFlowData(data)
			if err != nil {
				return nil, err
			}
		}
		done := make(chan marketingFlowNodeResult, len(active))
		for i, node := range active {
			if current != nil {
				current(node.ID)
			}
			snapshot := inputs[i]
			utils.SafeGo(ctx, "marketing-flow-node", func(ctx context.Context) {
				result := marketingFlowNodeResult{node: node}
				defer func() {
					if recovered := recover(); recovered != nil {
						result.err = fmt.Errorf("节点 %s panic: %v", node.ID, recovered)
					}
					done <- result
				}()
				result.output, result.err = execute(ctx, node, snapshot)
			})
		}
		results := make(map[string]marketingFlowNodeResult, len(active))
		for range active {
			result := <-done
			results[result.node.ID] = result
		}
		writes := make(map[string]any)
		for _, node := range active {
			result := results[node.ID]
			if result.err != nil {
				return nil, fmt.Errorf("节点 %s 执行失败：%w", node.ID, result.err)
			}
			if node.Type == "condition" {
				next, _ := result.output["_next_node"].(string)
				if next != "" {
					declared := false
					for _, candidate := range node.NextNodes {
						declared = declared || candidate == next
					}
					if !declared {
						return nil, fmt.Errorf("条件节点 %s 选择了未声明的后继", node.ID)
					}
					reachable[next] = true
				}
			} else {
				for _, next := range node.NextNodes {
					reachable[next] = true
				}
			}
			keys := make([]string, 0, len(result.output))
			for key := range result.output {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value := result.output[key]
				if key == "_next_node" || key == "_condition_matched" {
					continue
				}
				if before, exists := data[key]; exists && reflect.DeepEqual(before, value) {
					continue
				}
				if previous, exists := writes[key]; exists && !reflect.DeepEqual(previous, value) {
					return nil, fmt.Errorf("同层节点结果冲突：%s", key)
				}
				writes[key] = value
			}
		}
		for key, value := range writes {
			data[key] = value
		}
	}
	return data, nil
}

// 递归复制 JSON 可表示的 map/slice/array，保留整数类型，避免 JSON 往返损失大整数精度。
func cloneMarketingFlowData(input map[string]any) (map[string]any, error) {
	if input == nil {
		return make(map[string]any), nil
	}
	value, err := cloneMarketingFlowValue(reflect.ValueOf(input), 0)
	if err != nil {
		return nil, fmt.Errorf("%w: 流程数据无法复制：%v", utils.ErrInvalidInput, err)
	}
	return value.Interface().(map[string]any), nil
}

func cloneMarketingFlowValue(value reflect.Value, depth int) (reflect.Value, error) {
	if depth > 64 {
		return reflect.Value{}, fmt.Errorf("数据嵌套过深或包含循环引用")
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		child, err := cloneMarketingFlowValue(value.Elem(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		copy := reflect.New(value.Type()).Elem()
		copy.Set(child)
		return copy, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("对象键必须为字符串")
		}
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		copy := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			child, err := cloneMarketingFlowValue(iter.Value(), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			copy.SetMapIndex(iter.Key(), child)
		}
		return copy, nil
	case reflect.Slice, reflect.Array:
		var copy reflect.Value
		if value.Kind() == reflect.Slice {
			if value.IsNil() {
				return reflect.Zero(value.Type()), nil
			}
			copy = reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		} else {
			copy = reflect.New(value.Type()).Elem()
		}
		for i := 0; i < value.Len(); i++ {
			child, err := cloneMarketingFlowValue(value.Index(i), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			copy.Index(i).Set(child)
		}
		return copy, nil
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return value, nil
	default:
		return reflect.Value{}, fmt.Errorf("不支持的数据类型：%s", value.Kind())
	}
}
