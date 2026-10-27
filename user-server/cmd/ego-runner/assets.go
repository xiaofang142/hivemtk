package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 资产拉取落盘（T3 用户端；F13 勘误定案：**不走 env 重定向，直接写物理目录**）。
//
// 服务端清单 → 本地 learnings/{id}/：
//   learnings/{id}/manifest.json   （id/version/domains/hivemtk.entry + 来源标记）
//   learnings/{id}/tools/{entry}   （入口脚本）
//   learnings/{id}/.installed_hash （sha256，比对跳过未变化项）
//
// 幂等纪律：全量拉清单 + 本地 hash 比对，只写变化项。清单规模是单用户流程数，
// 增量协议换不来收益且会让「装没装上」变成需要推理的事。

const (
	assetPullInterval = 10 * time.Minute
	assetPullTimeout  = 30 * time.Second
)

// installableAsset 与服务端 egolite.InstallableAsset 同形（跨端契约；
// runner 不 import server internal 包——形状漂移由字段名共同约束 + 服务端单测钉死）
type installableAsset struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Version    int      `json:"version"`
	Domains    []string `json:"domains"`
	Writes     []string `json:"writes"`
	Entry      string   `json:"entry"`
	Script     string   `json:"script"`
	ScriptHash string   `json:"script_hash"`
	Source     string   `json:"source"`
	FlowID     uint     `json:"flow_id"`
	AssetID    string   `json:"asset_id"`
	AssetVer   string   `json:"asset_version"`
}

// learningsDir 物理 learnings 根（ego lite 实际读取的目录，T2 实测可读）
func learningsDir(workspaceDir string) string {
	// workspaceDir 默认已指向 ~/.local/share/ego/ego-skills；
	// 配成旧值（~/.hivemtk/ego-workspace）时同样落到 learnings 子目录——
	// 由安装器保证 workspace 指向 ego-skills 根。这里统一取 {workspace}/learnings。
	return filepath.Join(workspaceDir, "learnings")
}

// pullAssets 拉取并安装（启动时 + 周期性）
func (r *runner) pullAssets() {
	u, err := url.Parse(r.conf.ServerURL)
	if err != nil {
		return
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	u.Scheme = scheme
	u.Path = "/api/egolite/assets/pull"
	q := u.Query()
	q.Set("token", r.conf.Token)
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: assetPullTimeout}
	resp, err := client.Get(u.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产拉取失败: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产拉取 HTTP %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Assets []installableAsset `json:"assets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产清单解析失败: %v\n", err)
		return
	}
	installed, skipped := 0, 0
	for _, a := range payload.Data.Assets {
		changed, err := r.installAsset(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ego-runner] 安装 %s 失败: %v\n", a.ID, err)
			continue
		}
		if changed {
			installed++
		} else {
			skipped++
		}
	}
	if installed > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产同步：新装 %d / 未变 %d\n", installed, skipped)
	}
}

// installAsset 单个资产落盘；返回是否实际写入（hash 未变则跳过）
func (r *runner) installAsset(a installableAsset) (bool, error) {
	if a.ID == "" || a.Entry == "" {
		return false, fmt.Errorf("清单项缺 id/entry")
	}
	// 目录穿越防护（服务端已 slug 化，runner 侧再判一次——两端都不信任）
	if strings.Contains(a.ID, "..") || strings.Contains(a.Entry, "..") ||
		filepath.IsAbs(a.ID) || filepath.IsAbs(a.Entry) {
		return false, fmt.Errorf("非法路径（id=%q entry=%q）", a.ID, a.Entry)
	}
	dir := filepath.Join(learningsDir(r.conf.WorkspaceDir), a.ID)

	// hash 比对：已装且一致 → 跳过（幂等）
	hashPath := filepath.Join(dir, ".installed_hash")
	if b, err := os.ReadFile(hashPath); err == nil && strings.TrimSpace(string(b)) == a.ScriptHash {
		return false, nil
	}

	// 本地复核 hash（防传输损坏；服务端签名链在平台包通道，用户自有流程以
	// 服务端下发值为准——这里是完整性校验不是来源认证）
	sum := sha256.Sum256([]byte(a.Script))
	if hex.EncodeToString(sum[:]) != a.ScriptHash {
		return false, fmt.Errorf("脚本 hash 不匹配（期望 %s，实得 %x）", a.ScriptHash, sum[:])
	}

	entryPath := filepath.Join(dir, filepath.FromSlash(a.Entry))
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(entryPath, []byte(a.Script), 0o600); err != nil {
		return false, err
	}

	source := a.Source
	if source == "" {
		source = "flow-publish" // 兼容旧服务端（未带 source 字段）
	}
	manifest := map[string]any{
		"id":      a.ID,
		"name":    a.Name,
		"version": fmt.Sprintf("%d.0.0", a.Version),
		"domains": a.Domains,
		"hivemtk": map[string]any{
			"entry":       a.Entry,
			"compat_min":  "0.6.0",
			"writes":      a.Writes,
			"source":      source,
			"flow_id":     a.FlowID,
			"asset_id":    a.AssetID,
			"asset_ver":   a.AssetVer,
			"synced_at":   time.Now().UTC().Format(time.RFC3339),
			"script_hash": a.ScriptHash,
		},
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(hashPath, []byte(a.ScriptHash), 0o600); err != nil {
		return false, err
	}
	return true, nil
}
