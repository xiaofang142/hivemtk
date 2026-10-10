package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/system/install"

	"github.com/gin-gonic/gin"
)

// 已初始化的实例必须拒绝匿名再建一个超管。
//
// /api/system/init-admin 挂在 public 组里（首次安装时它就得能被匿名访问），
// 门只有一道：AuthController.InitAdmin 开头的 IsSystemInitialized()。这道门一旦失效，
// 任何能摸到 8204 的人都能给自己造一个 admin——比失窃的口令更糟，因为库里的表头
// 看着完全正常。所以这里断的是"门的形状"，而不只是"这一次点了会怎样"：
//   - 403 必须**先于**参数校验出现（否则一旦有人把守卫挪到 BindJSON 之后，
//     合法载荷就会真的建号，而用例仍然绿）；
//   - 没装过 / 装了没走完的实例不能被这道门锁死（否则新装的用户进不了向导）。
//
// 三条腿都不碰数据库：install.lock 走 INSTALL_LOCK_PATH 指到临时文件，
// admin 探针显式置 nil（GetStatus 只有在 lock 说"没初始化"时才去摸库，
// 而那种情况下载荷先在 binding 上失败返回 400）。

func writeLock(t *testing.T, dir string, name string, lock *install.Lock) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var data []byte
	if lock == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("移除临时 lock 失败: %v", err)
		}
		return path
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("序列化 lock 失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写临时 lock 失败: %v", err)
	}
	return path
}

// postInitAdmin 用给定载荷打一次 init-admin，返回状态码与响应体
func postInitAdmin(t *testing.T, payload any) (int, string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	router := setupGinEngine()
	router.POST("/api/system/init-admin", NewAuthController().InitAdmin)
	req := httptest.NewRequest(http.MethodPost, "/api/system/init-admin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestInitAdmin_RejectsAnonymousReinit 已初始化：一个字段完全合法的建号请求也必须原样弹回 403。
func TestInitAdmin_RejectsAnonymousReinit(t *testing.T) {
	dir := t.TempDir()
	lock := writeLock(t, dir, "initialized.lock", &install.Lock{
		InstallID:     "ins-test",
		AdminUsername: "admin",
		Initialized:   true,
		Version:       "test",
	})
	t.Setenv("INSTALL_LOCK_PATH", lock)
	install.SetAdminProbe(nil)
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	// 载荷是"能真的建出一个超管"的形状：用户名合规、密码够长。
	// 若守卫失效，这条请求会返回 200 并留下一个账号——所以断言里连状态码一起认。
	code, body := postInitAdmin(t, gin.H{
		"username": "zz_attacker",
		"password": "Str0ng!Pass123",
	})
	if code != http.StatusForbidden {
		t.Fatalf("已初始化实例匿名建超管应被 403 拦下，实际 %d, body=%s", code, body)
	}
	if !strings.Contains(body, "禁止重复创建超管") {
		t.Fatalf("403 的文案应说明是重复初始化被拒，实际 body=%s", body)
	}
	if strings.Contains(body, `"code":0`) || strings.Contains(body, "超管创建成功") {
		t.Fatalf("响应里出现了成功语义，说明请求走到了建号分支：body=%s", body)
	}
}

func TestInitAdmin_GuardRunsBeforeBinding(t *testing.T) {
	dir := t.TempDir()
	lock := writeLock(t, dir, "initialized.lock", &install.Lock{
		InstallID:     "ins-test",
		AdminUsername: "admin",
		Initialized:   true,
	})
	t.Setenv("INSTALL_LOCK_PATH", lock)
	install.SetAdminProbe(nil)
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	// username 只有 2 位、密码 3 位：binding 的 min 规则一定拦它。
	// 拿到 403（而不是 400）才证明守卫排在 BindJSON 前面——门的位置和门的开关一样重要。
	code, body := postInitAdmin(t, gin.H{"username": "ab", "password": "123"})
	if code != http.StatusForbidden {
		t.Fatalf("坏载荷也应先撞上 403 的初始化门，实际 %d, body=%s", code, body)
	}
	if strings.Contains(body, "参数错误") {
		t.Fatalf("出现了参数校验的文案，说明守卫在 BindJSON 之后：body=%s", body)
	}
}

func TestInitAdmin_StillOpenBeforeInitialization(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		lock *install.Lock
	}{
		// 全新磁盘：连 lock 都没有
		{"missing_lock", nil},
		// 写了 lock 但没标记完成
		{"not_initialized", &install.Lock{InstallID: "ins-test", Version: "test"}},
	}
	install.SetAdminProbe(nil)
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLock(t, dir, tc.name+".lock", tc.lock)
			t.Setenv("INSTALL_LOCK_PATH", path)
			if got := install.GetStatus().Initialized; got {
				t.Fatalf("前置不成立：%s 应被判为未初始化", tc.name)
			}
			// 用坏载荷探门：400 = 放行到校验层；403 = 这道门把还没装完的实例也锁死了
			code, body := postInitAdmin(t, gin.H{"username": "ab", "password": "123"})
			if code == http.StatusForbidden {
				t.Fatalf("未初始化实例不应被初始化门锁死：%s body=%s", tc.name, body)
			}
			if code != http.StatusBadRequest {
				t.Fatalf("未初始化时坏载荷应落到参数校验 400，实际 %d, body=%s", code, body)
			}
		})
	}
}

// TestGetStatusWithoutDBProbeWhenInitialized 第二道保险：已初始化的判定不能依赖数据库。
// 若哪天 GetStatus 改成"-initialized-也要摸库"，上面那条 403 就会在库不可用时变成 500/放行。
func TestGetStatusWithoutDBProbeWhenInitialized(t *testing.T) {
	dir := t.TempDir()
	path := writeLock(t, dir, "initialized.lock", &install.Lock{
		InstallID:     "ins-test",
		AdminUsername: "admin",
		Initialized:   true,
	})
	t.Setenv("INSTALL_LOCK_PATH", path)

	// 探针一旦被调用就记下——已初始化路径不该调它，哪怕库是坏的也必须给 403 而不是别的。
	var probed int
	install.SetAdminProbe(func(context.Context) (string, error) {
		probed++
		return "admin", nil
	})
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	st := install.GetStatus()
	if !st.Initialized || st.State != "INITIALIZED" {
		t.Fatalf("lock 写了 initialized+admin，应判 INITIALIZED，实际 state=%q initialized=%v", st.State, st.Initialized)
	}
	if probed != 0 {
		t.Fatalf("已初始化判定不该回查数据库（探针被调 %d 次）", probed)
	}
}
