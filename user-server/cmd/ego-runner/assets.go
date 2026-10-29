package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
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
	t0 := time.Now()
	u, err := url.Parse(r.conf.ServerURL)
	if err != nil {
		recordTelemetry("sync", time.Since(t0), false, "", "network")
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
	q.Set("format", "zip")
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: assetPullTimeout}
	resp, err := client.Get(u.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产拉取失败: %v\n", err)
		recordTelemetry("sync", time.Since(t0), false, "", "network")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产拉取 HTTP %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		recordTelemetry("sync", time.Since(t0), false, "", "network")
		return
	}
	// 包通道（S-7）：Content-Type 是 zip 就走整包安装，否则回落 JSON 清单（兼容旧服务端）。
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "zip") {
		installed, skipped, ierr := r.installFromZip(resp.Body)
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "[ego-runner] 资产包安装失败: %v\n", ierr)
			recordTelemetry("sync", time.Since(t0), false, "", "hash")
			return
		}
		if installed > 0 || skipped > 0 {
			fmt.Fprintf(os.Stderr, "[ego-runner] 资产包同步：新装 %d / 未变 %d\n", installed, skipped)
		}
		recordTelemetry("sync", time.Since(t0), true, "", "")
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
		recordTelemetry("sync", time.Since(t0), false, "", "hash")
		return
	}
	installed, skipped := 0, 0
	for _, a := range payload.Data.Assets {
		changed, err := r.installAsset(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ego-runner] 安装 %s 失败: %v\n", a.ID, err)
			// S-9 遥测 install：按资产记失败样本（错误码分类，不收原文）
			errClass := "io"
			if strings.Contains(err.Error(), "hash") {
				errClass = "hash"
			}
			recordTelemetry("install", 0, false, a.ID, errClass)
			continue
		}
		if changed {
			installed++
			recordTelemetry("install", 0, true, a.ID, "")
		} else {
			skipped++
		}
	}
	if installed > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "[ego-runner] 资产同步：新装 %d / 未变 %d\n", installed, skipped)
	}
	recordTelemetry("sync", time.Since(t0), true, "", "")
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

// ---------- S-7 包通道：zip 原子解包 ----------

const (
	assetZipMaxEntries    = 512
	assetZipMaxEntryBytes = 4 << 20
	assetZipMaxTotalBytes = 32 << 20
)

// safeZipEntry 校验包内路径（两端互不信任：服务端已判一次，这里再判）。
func safeZipEntry(name string) bool {
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "\\") {
		return false
	}
	if strings.HasPrefix(name, "/") || path.IsAbs(name) {
		return false
	}
	return path.Clean(name) == name
}

// installFromZip 把整包解到临时目录、逐资产原子换入；返回 (新装, 未变, error)。
//
// 原子性来自"先写 .tmp-<id> 再 rename"：中途失败时旧目录仍然完整，
// 不会出现"entry 已换、manifest 还是旧版"的半装状态。
func (r *runner) installFromZip(body io.Reader) (int, int, error) {
	data, err := io.ReadAll(io.LimitReader(body, assetZipMaxTotalBytes+1))
	if err != nil {
		return 0, 0, err
	}
	if len(data) > assetZipMaxTotalBytes {
		return 0, 0, fmt.Errorf("包体积超过上限 %d", assetZipMaxTotalBytes)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, fmt.Errorf("读包失败: %w", err)
	}
	if len(zr.File) > assetZipMaxEntries {
		return 0, 0, fmt.Errorf("条目数 %d 超过上限 %d", len(zr.File), assetZipMaxEntries)
	}

	type pkgAsset struct {
		files map[string][]byte
		hash  string
	}
	pkgs := map[string]*pkgAsset{}
	total := 0
	for _, f := range zr.File {
		if !safeZipEntry(f.Name) {
			return 0, 0, fmt.Errorf("非法条目路径: %q", f.Name)
		}
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("条目 %q 不在资产目录下", f.Name)
		}
		id, rel := parts[0], parts[1]
		if f.UncompressedSize64 > assetZipMaxEntryBytes {
			return 0, 0, fmt.Errorf("条目 %s 超过单条上限", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return 0, 0, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, assetZipMaxEntryBytes+1))
		rc.Close()
		if err != nil {
			return 0, 0, err
		}
		if len(content) > assetZipMaxEntryBytes {
			return 0, 0, fmt.Errorf("条目 %s 超过单条上限", f.Name)
		}
		total += len(content)
		if total > assetZipMaxTotalBytes {
			return 0, 0, fmt.Errorf("解压体积超过上限 %d", assetZipMaxTotalBytes)
		}
		p := pkgs[id]
		if p == nil {
			p = &pkgAsset{files: map[string][]byte{}}
			pkgs[id] = p
		}
		p.files[rel] = content
	}

	installed, skipped := 0, 0
	for id, p := range pkgs {
		mb, ok := p.files["manifest.json"]
		if !ok {
			return installed, skipped, fmt.Errorf("资产 %s 缺 manifest.json", id)
		}
		var m struct {
			Hivemtk struct {
				ScriptHash string `json:"script_hash"`
				Entry      string `json:"entry"`
			} `json:"hivemtk"`
		}
		if err := json.Unmarshal(mb, &m); err != nil {
			return installed, skipped, fmt.Errorf("资产 %s manifest 解析失败: %w", id, err)
		}
		// 内容完整性：入口脚本的 hash 必须与 manifest 一致（签名只覆盖 manifest）。
		if m.Hivemtk.ScriptHash != "" && m.Hivemtk.Entry != "" {
			entryBody, ok := p.files[m.Hivemtk.Entry]
			if !ok {
				return installed, skipped, fmt.Errorf("资产 %s 缺入口 %s", id, m.Hivemtk.Entry)
			}
			sum := sha256.Sum256(entryBody)
			if hex.EncodeToString(sum[:]) != m.Hivemtk.ScriptHash {
				return installed, skipped, fmt.Errorf("资产 %s 入口 hash 与 manifest 不符", id)
			}
		}
		dir := filepath.Join(learningsDir(r.conf.WorkspaceDir), id)
		// S-6：带签名的 manifest 必须有可验的信任公钥，验不过即拒（fail-close）。
		if sig := manifestSignature(mb); sig != "" {
			if verr := verifyAssetManifestAt(dir, mb); verr != nil {
				return installed, skipped, fmt.Errorf("资产 %s 签名验证失败: %w", id, verr)
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, ".installed_hash")); err == nil &&
			m.Hivemtk.ScriptHash != "" && strings.TrimSpace(string(b)) == m.Hivemtk.ScriptHash {
			skipped++
			continue
		}
		tmp := dir + ".tmp"
		if err := os.RemoveAll(tmp); err != nil {
			return installed, skipped, err
		}
		for rel, content := range p.files {
			full := filepath.Join(tmp, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				return installed, skipped, err
			}
			if err := os.WriteFile(full, content, 0o600); err != nil {
				return installed, skipped, err
			}
		}
		if m.Hivemtk.ScriptHash != "" {
			if err := os.WriteFile(filepath.Join(tmp, ".installed_hash"), []byte(m.Hivemtk.ScriptHash), 0o600); err != nil {
				return installed, skipped, err
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			return installed, skipped, err
		}
		if err := os.Rename(tmp, dir); err != nil {
			return installed, skipped, err
		}
		installed++
	}
	return installed, skipped, nil
}

// manifestSignature 读 manifest.hivemtk.signature（空串 = 未签名）。
func manifestSignature(mb []byte) string {
	var m struct {
		Hivemtk struct {
			Signature string `json:"signature"`
		} `json:"hivemtk"`
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		return ""
	}
	return m.Hivemtk.Signature
}
