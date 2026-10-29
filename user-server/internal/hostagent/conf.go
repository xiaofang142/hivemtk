// Package hostagent 收口 nm-host 与 ego-runner 两个宿主进程共享的配置解析。
//
// 为什么单独成包：两个 cmd 各自内置了一份 key=value 解析（一份用 strings.Cut、
// 一份用 bufio.Scanner），语义一致但字节实现不同，改一处漏一处会让同一份
// nm_host.conf 在两个进程里读出不同结果。runner 不 import 服务端 internal 包
// 的纪律只约束"服务端业务实现"，纯文本解析属于两侧共用的基础设施，放这里两边都能用。
package hostagent

import (
	"os"
	"path/filepath"
	"strings"
)

// ParseConf 解析 key=value 文本：跳过空行与 # 注释，键值各去一次首尾空白。
// 不校验取值——缺哪一项由调用方决定退回环境变量还是默认值。
func ParseConf(data []byte) map[string]string {
	conf := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		conf[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return conf
}

// ReadConfFile 读并解析配置文件。文件不存在时返回空 map（不视为错误）。
func ReadConfFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return ParseConf(data), nil
}

// DefaultConfPath 返回 ~/.hivemtk/<name> 的绝对路径（拿不到 home 时返回空串）。
func DefaultConfPath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hivemtk", name)
}
