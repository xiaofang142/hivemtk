package controller

// 契约锁：Host 通道的「仅限本机」这道门，和「重置 token」这句话。
//
// 两处各自独立成病：
//  1. clientIPOf 原先无条件采信 X-Real-IP / X-Forwarded-For——这两个头是调用方说什么就是什么。
//     于是 `/api/browser/host-ws`（注册在 engine 上、不过 JWT，本就只有 token 一道关）自称的
//     「双层防护」实际只剩一层：任何摸到这条路径的人加一行 `X-Real-IP: 127.0.0.1` 就过了回环门。
//     修法是用 ctx.RemoteIP()（gin 里 ClientIP 会顺着转发头改写，RemoteIP 只看连接真实对端）。
//     反方向的腿同样要有：本机 Host 经 nginx/frp 进来时，对端本来就是 127.0.0.1、转发头里带的
//     是它的公网地址——按头判就把真人拦在门外（第 3 条腿）。
//  2. ResetToken 对运维说「旧 token 24h 内仍可用」，但 ValidateHostToken 对 _prev 没有任何时限：
//     旧 token 实际活到**下一次重置**为止。撤销手段比它承诺的更弱，而没人会去读实现才发现。
//     本批不新增时限（全仓 bridge/webhook 的 _prev 都是同一形状，单给这条加就是第二个口径），
//     先把话改成与实现一致，并把「承诺=行为」钉成一条两用例：谁哪天给 prev 加了过期，
//     那条用例的红就是在提醒他同步改文案（红因不是回退）。
//
// KV 用内存假件：本批测的是「门看哪个地址」和「token 生命周期」，与 SQL 无关；
// 假件严格照抄 systemConfigKVRepo.Get 的语义（键不存在 → "", nil，而非 error），
// 抄错会让 fail-closed 那条腿假绿。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	basvc "hivemtk-user/internal/browser_automation/service"
	hrepo "hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
)

const b19fOwner = uint(781409)

type b19fKVRepo struct {
	m map[string]string
}

func (r b19fKVRepo) Available() bool { return true }

func (r b19fKVRepo) Get(_ context.Context, key string) (string, error) {
	return r.m[key], nil
}

func (r b19fKVRepo) Upsert(_ context.Context, key, value string) (string, error) {
	r.m[key] = value
	return value, nil
}

func (r b19fKVRepo) EnsureTable(context.Context) error { return nil }

func b19fKV() hrepo.SystemConfigKVRepository {
	return b19fKVRepo{m: map[string]string{}}
}

type b19fResp struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// b19fWSCall 用真实对端地址打 Host WS 入口：RemoteAddr 是「连接从哪来」的唯一可信来源，
// 表头是「调用方自称从哪来」。两者故意给不同的值，才分得清门看的是哪一个。
func b19fWSCall(t *testing.T, h *HostWSHandler, remoteAddr string, headers map[string]string, token string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/browser/host-ws", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c.Request = req
	h.Handle(c)
	return w.Code, w.Body.String()
}

func b19fReset(t *testing.T, c *HostController) (int, b19fResp) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/browser-automation/host/token/reset", nil)
	ctx.Set("user_id", b19fOwner)
	c.ResetToken(ctx)
	var b b19fResp
	if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil {
		t.Fatalf("重置响应不是合法 JSON（%v）：%s", e, w.Body.String())
	}
	return w.Code, b
}

func b19fToken(t *testing.T, b b19fResp) string {
	t.Helper()
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b.Data, &d); err != nil {
		t.Fatalf("重置响应 data 缺 token（%v）：%s", err, b.Data)
	}
	if d.Token == "" {
		t.Fatal("重置响应 data.token 为空串（读空与失败不许并成一类）")
	}
	return d.Token
}

func TestB19FSpoofedLoopbackHeaderDoesNotOpenHostChannel(t *testing.T) {
	kv := b19fKV()
	ctx := context.Background()
	// 给一个**合法** token：这样这条腿测的就不是 token 门，而是回环门本身。
	tok, err := basvc.GenerateHostToken(b19fOwner)
	if err != nil {
		t.Fatalf("生成 Host token 失败：%v", err)
	}
	if _, err := kv.Upsert(ctx, "browser_host_token", tok); err != nil {
		t.Fatalf("写入 token 失败：%v", err)
	}
	h := NewHostWSHandler(basvc.NewHostRegistry(), kv)

	// 203.0.113.0/24 是文档保留段：真实公网地址的替身，绝不与回环重合。
	code, body := b19fWSCall(t, h, "203.0.113.9:44444",
		map[string]string{"X-Real-IP": "127.0.0.1", "X-Forwarded-For": "127.0.0.1"}, tok)
	if code != http.StatusForbidden {
		t.Fatalf("远程对端伪造回环头 + 合法 token → HTTP %d（body=%q）want 403；"+
			"只要不是 403（例如 400 = 升级握手不成）就说明请求已走过回环门进了升级那一步，即门被表头骗过去了",
			code, body)
	}
	if !strings.Contains(body, "本机") {
		t.Errorf("403 文案没说是哪道门拦的：%q", body)
	}
}

func TestB19FLoopbackPeerStillReachesTokenGate(t *testing.T) {
	h := NewHostWSHandler(basvc.NewHostRegistry(), b19fKV())
	// 本机对端 + 不带 token：必须是「token 无效」401，不能被回环门一起挡成 403——
	// 否则「修门」的正确做法就退化成了锁死所有人（真机腿第 1 条即挂）。
	code, body := b19fWSCall(t, h, "127.0.0.1:5555", nil, "")
	if code == http.StatusForbidden {
		t.Fatalf("本机对端被回环门挡了：%q", body)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("本机对端缺 token → HTTP %d（body=%q）want 401", code, body)
	}
}

func TestB19FLocalHostBehindForwarderIsNotLockedOut(t *testing.T) {
	h := NewHostWSHandler(basvc.NewHostRegistry(), b19fKV())
	// 本机 Host 走 nginx/frp：对端是 127.0.0.1，转发头里是它的公网地址。
	// 按头判就会把真人拦在门外（这是第 1 条腿的反向，两半合起来才叫「门看连接、不看头」）。
	code, body := b19fWSCall(t, h, "127.0.0.1:5556",
		map[string]string{"X-Forwarded-For": "198.51.100.7, 127.0.0.1"}, "")
	if code == http.StatusForbidden {
		t.Fatalf("本机对端因转发头里的公网地址被判远程 → %q", body)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("本机对端（带转发头、缺 token）→ HTTP %d want 401（body=%q）", code, body)
	}
}

func TestB19FUnparseablePeerAddressIsRefused(t *testing.T) {
	kv := b19fKV()
	tok, err := basvc.GenerateHostToken(b19fOwner)
	if err != nil {
		t.Fatalf("生成 Host token 失败：%v", err)
	}
	if _, err := kv.Upsert(context.Background(), "browser_host_token", tok); err != nil {
		t.Fatalf("写入 token 失败：%v", err)
	}
	h := NewHostWSHandler(basvc.NewHostRegistry(), kv)
	// RemoteAddr 缺端口时 gin 的 RemoteIP() 返回空串。空串不是「未知=放行」，
	// 它是「判不出这是本机」——判不出就必须挡（否则伪造一个畸形地址就又绕过去了）。
	code, body := b19fWSCall(t, h, "203.0.113.9", map[string]string{"X-Real-IP": "127.0.0.1"}, tok)
	if code != http.StatusForbidden {
		t.Fatalf("对端地址畸形 + 合法 token → HTTP %d（body=%q）want 403（fail-closed）", code, body)
	}
}

func TestB19FResetTokenMessageMatchesPrevTokenLifetime(t *testing.T) {
	kv := b19fKV()
	ctx := context.Background()
	c := NewHostController(basvc.NewHostRegistry(), kv)

	code, b1 := b19fReset(t, c)
	if code != http.StatusOK {
		t.Fatalf("无存量 token 时首次重置 → HTTP %d want 200（msg=%q）", code, b1.Message)
	}
	first := b19fToken(t, b1)
	// KV 里没有任何归属线索时，重置落到默认 admin(1)——本用例后面两次 Validate 的归属都以此为前提。
	if !strings.HasPrefix(first, "bh_1_") {
		t.Fatalf("无存量 token 时首次重置应生成默认归属的 token，实际 %q", first)
	}
	if strings.Contains(b1.Message, "24h") || strings.Contains(b1.Message, "24 小时") {
		t.Errorf("重置文案向运维承诺了实现里不存在的过期时间：%q", b1.Message)
	}
	if !strings.Contains(b1.Message, "下一次重置") {
		t.Errorf("重置文案没说明旧 token 真正的失效条件：%q", b1.Message)
	}

	_, b2 := b19fReset(t, c)
	second := b19fToken(t, b2)
	if second == first {
		t.Fatal("两次重置给出同一个 token：轮换没生效")
	}

	// 行为侧：_prev 无时限——第二次重置后旧 token 仍然可用。这条与上面的文案是一条锁的两半，
	// 哪天给 prev 加了过期，这里必须红，逼着同批改文案。
	if _, ok := basvc.ValidateHostToken(ctx, kv, first); !ok {
		t.Fatal("旧 token 已判失效：文案与实现的锁该同步更新（见 controller/host.go ResetToken）")
	}
	// 轮换不得偷偷改归属：token 里的 userID 是「命令只投归属 Host」那条多租户边界。
	if id, ok := basvc.ValidateHostToken(ctx, kv, second); !ok || id != 1 {
		t.Errorf("第二次重置的新 token 归属 %d ok=%v，want 1/true（EnsureHostToken 默认 admin=1）", id, ok)
	}
}
