// Package secrets 的 fileloader 提供 Vault/KMS 形态的密钥注入通道（OPT-SEC-08）：
//
// 容器编排（K8s Secret 挂载 / Docker Swarm secrets / Vault Agent templating）
// 普遍以“文件”而非“环境变量”分发密钥。LoadFiles 在 InitFromEnv 之前把
// /run/secrets 下的密钥文件预加载为环境变量，使 MASTER_KEY 等既有 env 密钥
// 无需修改配置来源即可从挂载卷读取。
//
// 文件约定（两种风格）：
//   - *.txt（单值风格）：文件名去扩展名即变量名，文件首行内容即变量值，
//     例 /run/secrets/MASTER_KEY.txt 内容为 32+ 字节密钥；
//   - *.env（env 风格）：标准 KEY=VALUE 逐行解析，# 开头为注释，值去首尾空白。
//
// 优先级：进程中已显式设置的 env 优先，本加载器**不覆盖**既有变量
// （允许 `docker run -e MASTER_KEY=...` 覆盖挂载文件，也便于本地调试）。
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadFiles 从 dir 读取 *.txt / *.env 密钥文件并注入环境变量。
//
// 返回成功注入（实际 Setenv）的变量数量。行为：
//   - dir 不存在：返回 (0, nil)，视为“未启用文件注入”，不报错；
//   - 已存在的同名 env：跳过不覆盖（显式 env 优先级更高）；
//   - 单个文件解析失败：跳过该文件继续处理其余文件，并在返回值 err 中
//     聚合报告（errors.Join），调用方可只记 WARN 不中断启动。
func LoadFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("secrets: read secrets dir %s: %w", dir, err)
	}

	injected := 0
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".txt" && ext != ".env" {
			continue
		}
		path := filepath.Join(dir, name)
		vars, ferr := parseSecretFile(path, name, ext)
		if ferr != nil {
			errs = append(errs, ferr)
			continue
		}
		for k, v := range vars {
			if _, exists := os.LookupEnv(k); exists {
				continue
			}
			if v == "" {
				continue
			}
			if serr := os.Setenv(k, v); serr != nil {
				errs = append(errs, fmt.Errorf("secrets: setenv %s: %w", k, serr))
				continue
			}
			injected++
		}
	}
	return injected, errors.Join(errs...)
}

// parseSecretFile 按扩展名分派解析，返回 变量名->值 集合。
func parseSecretFile(path, name, ext string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: open %s: %w", path, err)
	}
	defer f.Close()

	vars := make(map[string]string)
	if ext == ".txt" {
		// 单值风格：MASTER_KEY.txt -> MASTER_KEY，取首行非空内容。
		key := strings.TrimSuffix(name, filepath.Ext(name))
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			vars[key] = line
			break
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("secrets: scan %s: %w", path, err)
		}
		return vars, nil
	}

	// env 风格：逐行 KEY=VALUE，剥离可选的 export 前缀与包裹引号。
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		vars[k] = trimQuotes(strings.TrimSpace(v))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("secrets: scan %s: %w", path, err)
	}
	return vars, nil
}

// trimQuotes 去掉值两侧成对的单/双引号。
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
