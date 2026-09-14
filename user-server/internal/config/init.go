package config

import (
	"os"
	"path/filepath"
	"strings"
)

func GetRootDir() string {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	dir := filepath.Dir(executable)
	if _, err := os.Stat(filepath.Dir(dir)); os.IsNotExist(err) {
		err = os.MkdirAll(filepath.Dir(dir), 0755)
		if err != nil {
			panic(err)
		}
	}
	return dir
}

func GetEnvDir() string {
	envDir := filepath.Join(GetRootDir(), ".env")
	parent := filepath.Dir(envDir)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		if err := os.MkdirAll(parent, 0755); err != nil {
			panic(err)
		}
	}
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		if err := os.MkdirAll(envDir, 0755); err != nil {
			panic(err)
		}
	}
	return envDir
}

// IsDevelopmentEnv 判定当前进程是否运行在非生产环境。
//
// 取值优先级：APP_ENV > MODE > GIN_MODE=debug。
// 供 secrets（MASTER_KEY fail-fast）等安全开关复用，避免各处重复实现判定逻辑。
func IsDevelopmentEnv() bool {
	env := ""
	for _, key := range []string{"APP_ENV", "MODE"} {
		if v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v != "" {
			env = v
			break
		}
	}
	switch env {
	case "dev", "development", "debug", "test", "testing", "local":
		return true
	case "":
		return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "debug")
	default:
		return false
	}
}
