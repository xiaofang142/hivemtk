package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetRootDir(t *testing.T) {
	rootDir := GetRootDir()
	if rootDir == "" {
		t.Error("Expected non-empty root directory")
	}
}

func TestGetEnvDir(t *testing.T) {
	envDir := GetEnvDir()
	if envDir == "" {
		t.Error("Expected non-empty env directory")
	}
}

func TestGetRootDir_Consistency(t *testing.T) {
	rootDir1 := GetRootDir()
	rootDir2 := GetRootDir()

	if rootDir1 != rootDir2 {
		t.Error("GetRootDir should return consistent results")
	}
}

func TestGetEnvDir_Consistency(t *testing.T) {
	envDir1 := GetEnvDir()
	envDir2 := GetEnvDir()

	if envDir1 != envDir2 {
		t.Error("GetEnvDir should return consistent results")
	}
}

func TestIsDevelopmentEnv(t *testing.T) {
	cases := []struct {
		name   string
		appEnv string
		mode   string
		gin    string
		want   bool
	}{
		{name: "APP_ENV development", appEnv: "development", want: true},
		{name: "APP_ENV dev 大小写", appEnv: "DEV", want: true},
		{name: "APP_ENV local", appEnv: " local ", want: true},
		{name: "MODE 兜底", mode: "test", want: true},
		{name: "APP_ENV 优先于 MODE", appEnv: "production", mode: "dev", want: false},
		{name: "APP_ENV production", appEnv: "production", want: false},
		{name: "未声明环境但 GIN_MODE debug", gin: "debug", want: true},
		{name: "未声明环境且 GIN_MODE release", gin: "release", want: false},
		{name: "全部未声明", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("MODE", tc.mode)
			t.Setenv("GIN_MODE", tc.gin)
			if got := IsDevelopmentEnv(); got != tc.want {
				t.Errorf("IsDevelopmentEnv()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetEnvDir_PathStructure(t *testing.T) {
	rootDir := GetRootDir()
	envDir := GetEnvDir()

	expectedEnvDir := filepath.Join(rootDir, ".env")
	if envDir != expectedEnvDir {
		t.Errorf("Expected env dir to be %s, got %s", expectedEnvDir, envDir)
	}
}

// Test that directories are created if they don't exist
func TestGetEnvDir_CreatesDirectory(t *testing.T) {
	envDir := GetEnvDir()

	_, err := os.Stat(filepath.Dir(envDir))
	if err != nil {
		t.Errorf("Expected env directory to exist, got error: %v", err)
	}
}
