package install

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// 本包的真相源是一个磁盘文件，且 InitGuard 每个请求都要读它一次，
// 所以用例全部走真文件（t.TempDir），不 mock IO。

// resetMemo 清掉 2 秒内存缓存：不清的话，上一个用例写的 lock 会被下一个用例读成自己的。
func resetMemo() {
	mu.Lock()
	memoLR, memoPath, memoExp = nil, "", time.Time{}
	mu.Unlock()
}

// useLockFile 把 install.lock 指到临时目录，并成对还原缓存与探针。
func useLockFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install.lock")
	t.Setenv("INSTALL_LOCK_PATH", path)
	resetMemo()
	SetAdminProbe(nil)
	t.Cleanup(func() {
		SetAdminProbe(nil)
		resetMemo()
	})
	return path
}

func writeRaw(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("铺夹具失败：%v", err)
	}
	resetMemo()
}

func readBackFromDisk(t *testing.T, path string) *Lock {
	t.Helper()
	resetMemo()
	lr, err := Load()
	if err != nil {
		t.Fatalf("自愈后 install.lock 必须能正常解析，实得 err=%v", err)
	}
	if lr == nil {
		t.Fatal("自愈后 install.lock 不该读成「不存在」")
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("回读文件失败：%v", rerr)
	}
	var probe map[string]any
	if jerr := json.Unmarshal(raw, &probe); jerr != nil {
		t.Fatalf("磁盘上的文件仍是坏 JSON：%v", jerr)
	}
	return lr
}

// realLockBody 用和生产同款的方式（MarshalIndent + 结构体字段序）出正文，
// 免得夹具的键序和真实文件对不上，测出来的是自己手写的形状。
func realLockBody(t *testing.T, lr *Lock) string {
	t.Helper()
	data, err := json.MarshalIndent(lr, "", "  ")
	if err != nil {
		t.Fatalf("铺夹具失败：%v", err)
	}
	return string(data)
}

// cutAfter 把正文掐到 marker 末尾为止，模拟「写到一半被杀」留下的半截文件。
func cutAfter(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("夹具正文里找不到截断锚点 %q，实得正文：%s", marker, body)
	}
	return body[:i+len(marker)]
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	path := useLockFile(t)
	lr, err := Load()
	if err != nil || lr != nil {
		t.Fatalf("文件不存在必须是 (nil, nil)，实得 (%v, %v)", lr, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("夹具不该自己造出 install.lock：%v", statErr)
	}
}

// 2 秒缓存是按"当前生效路径"读文件的，那它就必须按同一条路径记账：
// 路径换了还把上一份 lock 端过来，等于把 A 机的安装身份安到 B 机头上
// （跨包用例、装配期改 INSTALL_LOCK_PATH、换卷都会踩到）。
func TestLoadCacheIsScopedToItsPath(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.lock")
	pathB := filepath.Join(dir, "b.lock")
	t.Cleanup(func() { SetAdminProbe(nil); resetMemo() })

	t.Setenv("INSTALL_LOCK_PATH", pathA)
	resetMemo()
	if err := Save(&Lock{InstallID: "ins-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AdminUsername: "a", Initialized: true}); err != nil {
		t.Fatalf("写 A 失败：%v", err)
	}
	t.Setenv("INSTALL_LOCK_PATH", pathB)
	resetMemo()
	if err := Save(&Lock{InstallID: "ins-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AdminUsername: "b", Initialized: true}); err != nil {
		t.Fatalf("写 B 失败：%v", err)
	}

	// 缓存现在记的是 B；把生效路径切回 A，读回来的必须还是 A 那份。
	t.Setenv("INSTALL_LOCK_PATH", pathA)
	lr, err := Load()
	if err != nil || lr == nil {
		t.Fatalf("读 A 失败：%v %v", lr, err)
	}
	if lr.InstallID != "ins-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("缓存串了路径，A 读到了 %q", lr.InstallID)
	}
	if got := GetAdminUsername(); got != "a" {
		t.Errorf("超管名跟着串了，实得 %q", got)
	}
}

// 损坏的 lock 过去会把解析错误一路抛回 InitGuard：每个请求读→失败→查库→写不进去，
// 于是 install.lock 永久坏着、心跳静默停发。这里钉"能被修好"。
func TestMarkAdminInitializedHealsTruncatedLock(t *testing.T) {
	path := useLockFile(t)
	writeRaw(t, path, `{"install_id":"ins-0123456789abcdef0123456789abcdef","install_time":"2026-09-04T05:03:30Z","version":"1.2.3","admin_username":"admin","init`)
	if err := MarkAdminInitialized("admin"); err != nil {
		t.Fatalf("写到一半被截断的 lock 应当自愈，实得 err=%v", err)
	}
	lr := readBackFromDisk(t, path)
	if !lr.Initialized || lr.AdminUsername != "admin" {
		t.Errorf("自愈后状态不对：initialized=%v admin=%q", lr.Initialized, lr.AdminUsername)
	}
	if lr.InstallID != "ins-0123456789abcdef0123456789abcdef" {
		t.Errorf("install_id 是安装身份，必须从截断的原文里保下来，实得 %q", lr.InstallID)
	}
	if lr.InstallTime != "2026-09-04T05:03:30Z" {
		t.Errorf("install_time 重打成「今天」＝谎报安装日期，实得 %q", lr.InstallTime)
	}
	if lr.Version != "1.2.3" {
		t.Errorf("还能读出来的键不许丢，version 实得 %q", lr.Version)
	}
}

// 整份文件都不是 JSON 时没有可保的键，但必须重建出一份能用的 lock。
func TestMarkAdminInitializedHealsUnreadableLock(t *testing.T) {
	path := useLockFile(t)
	writeRaw(t, path, "\x00\x01not json at all")
	if err := MarkAdminInitialized("root"); err != nil {
		t.Fatalf("彻底读不懂的 lock 也应当重建，实得 err=%v", err)
	}
	lr := readBackFromDisk(t, path)
	if !strings.HasPrefix(lr.InstallID, "ins-") || len(lr.InstallID) != 36 {
		t.Errorf("无可保 install_id 时应新铸一枚 ins-+32hex，实得 %q", lr.InstallID)
	}
	if !lr.Initialized || lr.AdminUsername != "root" {
		t.Errorf("重建后的状态不对：initialized=%v admin=%q", lr.Initialized, lr.AdminUsername)
	}
}

// 自愈之后不许每请求再查一次库：这条是 InitGuard 热路径上的真实代价。
func TestGetStatusHealsOnceAndStopsProbingDB(t *testing.T) {
	path := useLockFile(t)
	probeCalls := 0
	SetAdminProbe(func(context.Context) (string, error) {
		probeCalls++
		return "admin", nil
	})
	writeRaw(t, path, `{"install_id":"ins-ffffffffffffffffffffffffffffffff","install_time":"2026-01-02T03:04:05Z","admin_username":"ad`)

	st := GetStatus()
	if st.State != "INITIALIZED" || !st.Initialized || !st.HasAdmin {
		t.Fatalf("库里有超管时损坏的 lock 应判 INITIALIZED，实得 %+v", st)
	}
	if st.InstallID != "ins-ffffffffffffffffffffffffffffffff" {
		t.Errorf("自愈后返回的 install_id 不能是空的（前端与心跳都读它），实得 %q", st.InstallID)
	}
	if probeCalls != 1 {
		t.Errorf("自愈那一次查库即可，实得 %d 次", probeCalls)
	}

	resetMemo()
	if st2 := GetStatus(); st2.State != "INITIALIZED" || st2.InstallID == "" {
		t.Errorf("缓存过期后的下一次请求仍应 INITIALIZED 且带 install_id，实得 %+v", st2)
	}
	if probeCalls != 1 {
		t.Errorf("文件已修好后仍每请求查库＝每个 API 请求多一次 DB 往返，累计 %d 次", probeCalls)
	}
}

// 全新安装（无文件、库里没超管）不许留下半份文件，也不许把探针错误当成功。
func TestGetStatusFreshInstallWritesNothing(t *testing.T) {
	path := useLockFile(t)
	SetAdminProbe(func(context.Context) (string, error) { return "", nil })
	if st := GetStatus(); st.State != "NOT_INSTALLED" || st.Initialized {
		t.Fatalf("空库空文件必须 NOT_INSTALLED，实得 %+v", st)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("NOT_INSTALLED 这条路上不该有 install.lock：%v", err)
	}
	resetMemo()
	if st := GetStatus(); st.Reminted {
		t.Error("从来没装过 ≠ 身份丢了：首装判读不许报重铸")
	}
}

// install.lock 整个没了而库里明明有超管 ⇒ 回填必然铸一枚全新的 install_id，
// 这台实例在平台侧就变成了一个新装商户（商户行按 install_id 建，历史就此断开）。
// 最常见成因是换目录启动：默认路径 ./install.lock 是进程 CWD 相对的，
// 本机就实测同时存在过三份 install_id 各不相同的锁文件。
// 这件事过去完全无声，所以状态里要带着"这次身份是重铸的"出门，让告警腿有得说。
func TestGetStatusFlagsRemintWhenLockIsGone(t *testing.T) {
	useLockFile(t)
	SetAdminProbe(func(context.Context) (string, error) { return "admin", nil })

	st := GetStatus()
	if st.State != "INITIALIZED" {
		t.Fatalf("库里有超管时缺失的 lock 要能回填成 INITIALIZED，实得 %+v", st)
	}
	if !st.Reminted {
		t.Error("磁盘上没有身份而库里已安装＝身份丢了，必须标出来给告警腿用")
	}

	// 铸完就有文件了：下一次读不该再报重铸，否则心跳每 3 分钟刷一条同义告警。
	resetMemo()
	if st2 := GetStatus(); st2.Reminted {
		t.Errorf("新身份已经落盘，下一次不该再判重铸： %+v", st2)
	}
}

// 文件坏着但 install_id 还捞得出来：身份没丢，不许报重铸。
func TestGetStatusKeepsSalvagedIdentityUnflagged(t *testing.T) {
	path := useLockFile(t)
	SetAdminProbe(func(context.Context) (string, error) { return "admin", nil })
	writeRaw(t, path, `{"install_id":"ins-33333333333333333333333333333333","install_time":"2026-01-01T00:00:00Z","admin_username":"ad`)

	st := GetStatus()
	if st.InstallID != "ins-33333333333333333333333333333333" {
		t.Fatalf("还能捞出来的身份必须保着，实得 %q", st.InstallID)
	}
	if st.Reminted {
		t.Errorf("原身份被保下来时不该报重铸： %+v", st)
	}
}

// 坏到连 install_id 都捞不出来，和文件整个没了是同一件事：必须报重铸。
func TestGetStatusFlagsRemintWhenIdentityUnreadable(t *testing.T) {
	path := useLockFile(t)
	SetAdminProbe(func(context.Context) (string, error) { return "ops", nil })
	writeRaw(t, path, "\x00\x01not json at all")

	if st := GetStatus(); !st.Reminted {
		t.Errorf("旧身份救不回来就是换了身份，不许不报： %+v", st)
	}
}

// Reminted 只给进程内的告警腿看，不许混进任何对外响应：
// 前端与平台侧读到的字段集必须保持原样（这是一份公开可读的接口）。
func TestStatusRemintedNeverSerialized(t *testing.T) {
	data, err := json.Marshal(&Status{State: "INITIALIZED", InstallID: "ins-x", Reminted: true})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	body := string(data)
	if strings.Contains(body, "eminted") {
		t.Errorf("重铸标记漏进了对外响应：%s", body)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("响应体解不开：%v", err)
	}
	keys := make([]string, 0, len(back))
	for k := range back {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range []string{"state", "initialized", "has_admin", "install_id"} {
		if _, ok := back[k]; !ok {
			t.Errorf("对外字段 %q 不该消失，实得 keys=%v", k, keys)
		}
	}
}

// 半截写入是损坏的主要来源，所以落盘必须走"临时文件 + rename"。
func TestSaveLeavesNoTempFileAndKeepsMode(t *testing.T) {
	path := useLockFile(t)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatalf("铺陈旧临时文件失败：%v", err)
	}
	if err := Save(&Lock{AdminUsername: "admin", Initialized: true}); err != nil {
		t.Fatalf("Save 失败：%v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("rename 之后不许留临时文件（留了＝下次可能读到半截）：%v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Save 后读不到文件：%v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("install.lock 权限应为 0644，实得 %v", info.Mode().Perm())
	}
	readBackFromDisk(t, path)
}

// Save 缓存的是调用方那份指针时，调用方之后改一笔就与磁盘上的文件悄悄分叉。
func TestSaveDoesNotAliasCallerLock(t *testing.T) {
	useLockFile(t)
	lr := &Lock{AdminUsername: "admin", Initialized: true}
	if err := Save(lr); err != nil {
		t.Fatalf("Save 失败：%v", err)
	}
	lr.AdminUsername = "tampered"
	if got := GetAdminUsername(); got != "admin" {
		t.Errorf("改调用方手里的副本不该影响已落盘的读值，实得 %q", got)
	}
}

// 兜住 GetStatus 的其余分支：文件在、没标 initialized、库里没超管 ⇒ HAS_ADMIN，且不改写文件。
func TestGetStatusHasAdminWithoutDBAdmin(t *testing.T) {
	path := useLockFile(t)
	SetAdminProbe(func(context.Context) (string, error) { return "", nil })
	body := `{"install_id":"ins-11111111111111111111111111111111","install_time":"2026-01-01T00:00:00Z","admin_username":"admin","initialized":false,"version":""}`
	writeRaw(t, path, body)
	if st := GetStatus(); st.State != "HAS_ADMIN" || st.Initialized || st.InstallID == "" {
		t.Fatalf("有超管名但库中无超管应判 HAS_ADMIN，实得 %+v", st)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if string(got) != body {
		t.Errorf("HAS_ADMIN 这条判读路径不许改写文件：\nwant %s\ngot  %s", body, got)
	}
}

// init-complete 走的是 Standalone 这条腿：文件坏着时它也必须能落盘，
// 且要把坏文件里还看得见的 admin_username 保住（否则标记完初始化反而把超管名洗掉）。
func TestMarkAdminInitializedStandaloneHealsAndKeepsAdmin(t *testing.T) {
	path := useLockFile(t)
	lr := &Lock{
		InstallID:     "ins-abcdefabcdefabcdefabcdefabcdefab",
		InstallTime:   "2026-03-04T05:06:07Z",
		AdminUsername: "ops",
		Version:       "1.4.2",
	}
	// 真实损坏形状是「写到一半被杀」：后面的键丢了，前面的键是完整的一行。
	body := cutAfter(t, realLockBody(t, lr), `"admin_username": "ops",`)
	writeRaw(t, path, body)
	if err := MarkAdminInitializedStandalone(); err != nil {
		t.Fatalf("Standalone 这条腿遇损坏也必须自愈，实得 err=%v", err)
	}
	got := readBackFromDisk(t, path)
	if got.AdminUsername != "ops" {
		t.Errorf("自愈不该把已记录的超管名洗成空，实得 %q", got.AdminUsername)
	}
	if got.InstallID != lr.InstallID {
		t.Errorf("install_id 应保下来，实得 %q", got.InstallID)
	}
	if !got.Initialized {
		t.Error("Standalone 的语义就是把 initialized 置真")
	}
}

// 尾部多出一截垃圾（盘上残留／非 JSON 尾巴）时，前面的键明明全在，
// 一个都不该丢——这条兜住四条捞取正则各自真的接上了键名。
func TestMarkAdminInitializedKeepsAllKeysWhenTailGarbage(t *testing.T) {
	path := useLockFile(t)
	lr := &Lock{
		InstallID:     "ins-abcdefabcdefabcdefabcdefabcdefab",
		InstallTime:   "2026-03-04T05:06:07Z",
		AdminUsername: "ops",
		Version:       "1.4.2",
	}
	writeRaw(t, path, realLockBody(t, lr)+"\x00\x00stale-tail")
	if err := MarkAdminInitialized("ops"); err != nil {
		t.Fatalf("坏尾巴不许挡住落盘，实得 err=%v", err)
	}
	got := readBackFromDisk(t, path)
	if got.InstallID != lr.InstallID || got.Version != lr.Version || got.AdminUsername != "ops" {
		t.Errorf("全在的键被捞丢了：install_id=%q version=%q admin=%q", got.InstallID, got.Version, got.AdminUsername)
	}
	if !got.Initialized {
		t.Error("initialized 应当被置真")
	}
}

// 损坏到值本身没写完时，救不出名字是能力边界、不是缺陷；
// 但落盘不能失败、已写全的 install_id 不能跟着一起丢。
func TestMarkAdminInitializedHealsMidValueCut(t *testing.T) {
	path := useLockFile(t)
	lr := &Lock{
		InstallID:     "ins-abcdefabcdefabcdefabcdefabcdefab",
		InstallTime:   "2026-03-04T05:06:07Z",
		AdminUsername: "ops",
	}
	body := cutAfter(t, realLockBody(t, lr), `"admin_username": "o`)
	writeRaw(t, path, body)
	if err := MarkAdminInitializedStandalone(); err != nil {
		t.Fatalf("半截值也必须能落盘自愈，实得 err=%v", err)
	}
	got := readBackFromDisk(t, path)
	if got.InstallID != lr.InstallID {
		t.Errorf("install_id 那一行是完整的，不该跟着丢，实得 %q", got.InstallID)
	}
	if got.AdminUsername != "" {
		t.Errorf("没写完的值不许捞成半个词，实得 %q", got.AdminUsername)
	}
}

// 文件在、超管名在，却没标 initialized（init-admin 落了一半就断电）：
// 库里有超管就该判 INITIALIZED 并把文件回填，否则每个请求都要再查一次库。
func TestGetStatusBackfillsWhenLockNotMarked(t *testing.T) {
	path := useLockFile(t)
	calls := 0
	SetAdminProbe(func(context.Context) (string, error) {
		calls++
		return "admin", nil
	})
	writeRaw(t, path, realLockBody(t, &Lock{
		InstallID:     "ins-11111111111111111111111111111111",
		InstallTime:   "2026-01-01T00:00:00Z",
		AdminUsername: "admin",
	}))

	st := GetStatus()
	if st.State != "INITIALIZED" || !st.Initialized {
		t.Fatalf("库里有超管时未标记的文件应就地判 INITIALIZED，实得 %+v", st)
	}
	if calls != 1 {
		t.Errorf("回填那一次查库即可，实得 %d 次", calls)
	}
	resetMemo()
	if st2 := GetStatus(); st2.State != "INITIALIZED" {
		t.Errorf("回填后的下一次请求仍应 INITIALIZED，实得 %+v", st2)
	}
	if calls != 1 {
		t.Errorf("文件已回填还每请求查库＝每个 API 请求多一次 DB 往返，累计 %d 次", calls)
	}
}

// 文件里连超管名都没写上的那一档（只有 install_id）：库里有超管时同样要回填出完整 lock。
func TestGetStatusBackfillsWhenAdminNameMissing(t *testing.T) {
	path := useLockFile(t)
	calls := 0
	SetAdminProbe(func(context.Context) (string, error) {
		calls++
		return "ops", nil
	})
	writeRaw(t, path, realLockBody(t, &Lock{
		InstallID:   "ins-22222222222222222222222222222222",
		Version:     "1.4.2",
		InstallTime: "2026-01-01T00:00:00Z",
	}))

	if st := GetStatus(); st.State != "INITIALIZED" || !st.Initialized || !st.HasAdmin {
		t.Fatalf("无超管名但库里有超管应判 INITIALIZED，实得 %+v", st)
	}
	got := readBackFromDisk(t, path)
	if got.AdminUsername != "ops" || !got.Initialized {
		t.Errorf("回填没落到盘上：admin=%q initialized=%v", got.AdminUsername, got.Initialized)
	}
	if got.Version != "1.4.2" {
		t.Errorf("回填不该把已有的 version 洗掉，实得 %q", got.Version)
	}
	if got.InstallID != "ins-22222222222222222222222222222222" {
		t.Errorf("回填不该换一枚 install_id，实得 %q", got.InstallID)
	}
	if calls != 1 {
		t.Errorf("只许查库一次，实得 %d 次", calls)
	}
}
