// obs_config_batchm_test.go 钉服务层对"默认存储"的两条判断（批M / N-26）。
//
// 为什么在 service 层用替身、而不是全部推给 repository 那一份真库用例：
// 本批在服务层新加的判断只有一处（不许把停用行设成默认），它属于"选取判据"
// 与"写回形状"的接缝 —— repository 用例看不到它，controller 用例又会把
// service 换掉。UploadFile 那条同理：要断的是"服务层调的是哪一个写回入口"。
package service

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// batchMObsRepo 是 ObsConfigRepository 的记账替身：每个方法记下自己被怎么调用，
// 好让"写回用的是哪条入口"这种问题变成可断言的事实。
type batchMObsRepo struct {
	rows         map[string]*model.ObsConfig
	order        []string
	updated      []string // 走整行 Save 的 id
	increments   []batchMInc
	setDefaults  []string
	deleted      []string // 走删除口的 id
	beforeDelete func(string)
	clearCalls   int
}

type batchMInc struct {
	id   string
	size int64
}

func newBatchMObsRepo(rows ...*model.ObsConfig) *batchMObsRepo {
	r := &batchMObsRepo{rows: map[string]*model.ObsConfig{}}
	for _, row := range rows {
		r.rows[row.ID] = row
		r.order = append(r.order, row.ID)
	}
	return r
}

func (r *batchMObsRepo) Create(ctx context.Context, c *model.ObsConfig) error {
	r.rows[c.ID] = c
	r.order = append(r.order, c.ID)
	return nil
}

func (r *batchMObsRepo) GetByID(ctx context.Context, id string) (*model.ObsConfig, error) {
	c, ok := r.rows[id]
	if !ok {
		return &model.ObsConfig{}, gorm.ErrRecordNotFound
	}
	return c, nil
}

func (r *batchMObsRepo) GetList(ctx context.Context, page, limit int, provider, status string) ([]*model.ObsConfig, int64, error) {
	return nil, 0, nil
}

func (r *batchMObsRepo) Update(ctx context.Context, c *model.ObsConfig) error {
	r.updated = append(r.updated, c.ID)
	r.rows[c.ID] = c
	return nil
}

// DeleteNonDefault 复刻真库那条语句的判据（WHERE id = ? AND is_default = false）：
// 默认行删不动、不存在的 id 也报 ErrRecordNotFound。beforeDelete 用来模拟
// "服务层预读之后、这条 DELETE 之前"另一个请求把这一行抬成了默认（S5）。
func (r *batchMObsRepo) DeleteNonDefault(ctx context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	if r.beforeDelete != nil {
		r.beforeDelete(id)
	}
	c := r.rows[id]
	if c == nil || c.IsDefault {
		return gorm.ErrRecordNotFound
	}
	delete(r.rows, id)
	return nil
}

// GetDefault 复刻真库判据：is_default AND status = active，并且按 created_at 升序，
// 这样替身不会替实现"放行"一个只在自己这儿成立的读法。
func (r *batchMObsRepo) GetDefault(ctx context.Context) (*model.ObsConfig, error) {
	var best *model.ObsConfig
	for _, id := range r.order {
		c := r.rows[id]
		if c == nil || !c.IsDefault || c.Status != model.ObsStatusActive {
			continue
		}
		if best == nil || c.CreatedAt.Before(best.CreatedAt) {
			best = c
		}
	}
	if best == nil {
		return &model.ObsConfig{}, gorm.ErrRecordNotFound
	}
	return best, nil
}

func (r *batchMObsRepo) SetDefault(ctx context.Context, id string) error {
	if _, ok := r.rows[id]; !ok {
		return gorm.ErrRecordNotFound
	}
	r.setDefaults = append(r.setDefaults, id)
	for _, c := range r.rows {
		c.IsDefault = c.ID == id
	}
	return nil
}

func (r *batchMObsRepo) IncrementUsage(ctx context.Context, id string, size int64) error {
	c, ok := r.rows[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	r.increments = append(r.increments, batchMInc{id: id, size: size})
	c.FileCount++
	c.TotalSize += size
	return nil
}

func (r *batchMObsRepo) UpdateStatus(ctx context.Context, id string, status model.ObsStatus) error {
	c, ok := r.rows[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	c.Status = status
	return nil
}

func (r *batchMObsRepo) CountByStatus(ctx context.Context, status model.ObsStatus) (int64, error) {
	return 0, nil
}

func (r *batchMObsRepo) Count(ctx context.Context) (int64, error) { return int64(len(r.rows)), nil }

func batchMObsRow(id, name string, provider model.ObsProvider, status model.ObsStatus, isDefault bool) *model.ObsConfig {
	return &model.ObsConfig{
		ID: id, Name: name, Provider: provider,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: status, MaxSize: 1 << 20, MaxCount: 100, IsDefault: isDefault,
	}
}

// S1: 停用中的配置不能被设成默认。
//
// 这条是本批自己**造出来**的风险：GetDefault 加上 status 判据之后，
// "默认行是非 active"第一次变成一个可以被人一键做出的状态 —— 界面回"成功"，
// 而全站上传与媒体转存当场断炊。所以设默认必须与选取用同一把尺子。
func TestBatchM_SetDefaultConfigRejectsInactive(t *testing.T) {
	ctx := context.Background()
	disabled := batchMObsRow("batchm-svc-disabled", "停用那台", model.ObsProviderLocal, model.ObsStatusInactive, false)
	repo := newBatchMObsRepo(batchMObsRow("batchm-svc-active", "在用那台", model.ObsProviderLocal, model.ObsStatusActive, true), disabled)
	svc := &obsConfigService{repo: repo}

	err := svc.SetDefaultConfig(ctx, disabled.ID)
	if err == nil {
		t.Fatal("把停用中的配置设成默认返回了 nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("inactive")) {
		t.Errorf("错误里没有交代清楚状态： %v", err)
	}
	if len(repo.setDefaults) != 0 {
		t.Errorf("校验没挡住写：SetDefault 被调用了 %v", repo.setDefaults)
	}

	// 对照腿：同一台恢复 active 之后，必须能设，且只调一次 repo.SetDefault。
	disabled.Status = model.ObsStatusActive
	if err := svc.SetDefaultConfig(ctx, disabled.ID); err != nil {
		t.Fatalf("active 配置设默认失败: %v", err)
	}
	if len(repo.setDefaults) != 1 || repo.setDefaults[0] != disabled.ID {
		t.Errorf("SetDefault 调用 = %v，期望 [%s]", repo.setDefaults, disabled.ID)
	}
}

// S1b: 服务层不再自己"先清后设"。
//
// 旧实现是 repo.ClearDefault + repo.SetDefault 两句：第一句一提交，库里就进入
// "零条默认"状态，第二句失败（行被并发删掉）时这个状态就永久留下。
// 现在切换的原子性归 repo.SetDefault 那一个事务，服务层只许调它一次。
func TestBatchM_SetDefaultConfigDoesNotPreClear(t *testing.T) {
	ctx := context.Background()
	target := batchMObsRow("batchm-svc-b", "B", model.ObsProviderQiniu, model.ObsStatusActive, false)
	repo := newBatchMObsRepo(batchMObsRow("batchm-svc-a", "A", model.ObsProviderLocal, model.ObsStatusActive, true), target)
	svc := &obsConfigService{repo: repo}

	if err := svc.SetDefaultConfig(ctx, target.ID); err != nil {
		t.Fatalf("SetDefaultConfig 失败: %v", err)
	}
	if repo.clearCalls != 0 {
		t.Errorf("服务层仍自己清了默认（%d 次），零默认窗口又回来了", repo.clearCalls)
	}
	if got, _ := repo.GetDefault(ctx); got == nil || got.ID != target.ID {
		t.Errorf("切换后默认行不是目标行：%+v", got)
	}
}

// S2: 上传成功后的写回必须走列级自增，不许整行 Save。
//
// UploadFile 手上的 config 是上传开始时的快照；整行写回会连 is_default 一起落库，
// 期间管理员切换默认 ⇒ 陈旧的那台被复活成第二条默认行（详见 repository 那份 R3）。
// 这里断"调用形状"，repository 那份断"SQL 形状"，两层各管一段。
func TestBatchM_UploadFileWritesUsageViaIncrementOnly(t *testing.T) {
	baseDir := t.TempDir()
	cfg := batchMObsRow("batchm-svc-up", "本地默认", model.ObsProviderLocal, model.ObsStatusActive, true)
	cfg.Endpoint = baseDir
	cfg.Domain = "/files"
	repo := newBatchMObsRepo(cfg)
	svc := &obsConfigService{repo: repo}

	url := batchMUpload(t, svc, "note.txt", []byte("hello batchm"))
	if url == "" {
		t.Fatal("上传返回空 URL")
	}
	if len(repo.increments) != 1 || repo.increments[0].id != cfg.ID || repo.increments[0].size != int64(len("hello batchm")) {
		t.Errorf("IncrementUsage 调用 = %+v，期望 1 次 {batchm-svc-up, 12}", repo.increments)
	}
	if len(repo.updated) != 0 {
		t.Errorf("上传路径仍在整行 Save（%v），陈旧快照会复活 is_default", repo.updated)
	}
	if got := repo.rows[cfg.ID]; got.FileCount != 1 || got.TotalSize != 12 {
		t.Errorf("用量计数 = %+v，期望 FileCount 1 / TotalSize 12", got)
	}
	// 文件真的落到了这台存储的目录里（防"驱动换了别的路径还报成功"）。
	found := false
	_ = filepath.Walk(baseDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("上传报成功，但 baseDir 下没有任何文件")
	}
}

// batchMUpload 造一份真实 multipart 上传（不碰 http，直接喂 UploadFile 的入参形状）。
func batchMUpload(t *testing.T, svc ObsConfigService, filename string, payload []byte) string {
	t.Helper()
	body := &bytes.Buffer{}
	mimeHeader := textproto.MIMEHeader{}
	mimeHeader.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	mimeHeader.Set("Content-Type", "text/plain")
	w := multipart.NewWriter(body)
	part, err := w.CreatePart(mimeHeader)
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("写入 multipart 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}
	reader := multipart.NewReader(body, w.Boundary())
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm 失败: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	fhs := form.File["file"]
	if len(fhs) != 1 {
		t.Fatalf("夹具里应有 1 个文件，实际 %d", len(fhs))
	}
	f, err := fhs[0].Open()
	if err != nil {
		t.Fatalf("打开 multipart.File 失败: %v", err)
	}
	defer f.Close()

	url, err := svc.UploadFile(context.Background(), f, fhs[0], "batchm")
	if err != nil {
		t.Fatalf("UploadFile 失败: %v", err)
	}
	return url
}

// S3: 没有可用默认时必须把错误传出去，而不是拿一个 nil 配置继续往下走。
//
// 为什么单独一条：UploadFile 的兜底分支以前会被调用方误读成"存储驱动坏了"，
// 因为它把 record not found 混进了 "构造存储驱动失败"。这里只判第一句。
func TestBatchM_UploadFileWithoutDefaultIsExplicit(t *testing.T) {
	ctx := context.Background()
	repo := newBatchMObsRepo(batchMObsRow("batchm-svc-none", "停用", model.ObsProviderLocal, model.ObsStatusInactive, true))
	svc := &obsConfigService{repo: repo}

	if _, err := svc.GetDefaultConfig(ctx); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("GetDefaultConfig 错误 = %v，期望 gorm.ErrRecordNotFound", err)
	}

	_, err := svc.UploadFile(ctx, nil, nil, "batchm")
	if err == nil {
		t.Fatal("无可用默认时 UploadFile 竟然成功")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("未找到默认存储配置")) {
		t.Errorf("错误文案不含选取失败归因：%v", err)
	}
	if len(repo.increments) != 0 || len(repo.updated) != 0 {
		t.Error("选取失败之后仍然写了用量")
	}
}

// S5: 删除口只许走带守卫的那一个入口。
//
// 服务层那句 `if config.IsDefault` 只负责把错误话说清楚（"不能删除默认配置"），
// 它**不是**安全判据：预读与落库之间另一次"设为默认"提交，先读后删就会删掉全站
// 唯一默认。真判据在 repo 那条 DELETE 的 WHERE 里（见 repository 那份 R7）。
// 这条腿用 beforeDelete 把那个交错铺出来，钉两件事：
// ① 服务层确实调的是带守卫的删除口；② 越过预检的并发写回不会留下零默认。
func TestBatchM_DeleteConfigCannotRemoveDefault(t *testing.T) {
	ctx := context.Background()
	a := batchMObsRow("batchm-svc-del-a", "A(默认)", model.ObsProviderLocal, model.ObsStatusActive, true)
	b := batchMObsRow("batchm-svc-del-b", "B", model.ObsProviderQiniu, model.ObsStatusActive, false)
	c := batchMObsRow("batchm-svc-del-c", "C", model.ObsProviderQiniu, model.ObsStatusActive, false)
	repo := newBatchMObsRepo(a, b, c)
	svc := &obsConfigService{repo: repo}

	// 正常路径：非默认行删得掉，且走的就是那一个入口。
	if err := svc.DeleteConfig(ctx, b.ID); err != nil {
		t.Fatalf("删除非默认配置失败: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != b.ID {
		t.Errorf("删除口调用 = %v，期望 [%s]", repo.deleted, b.ID)
	}
	if _, ok := repo.rows[b.ID]; ok {
		t.Error("删除回成功后替身里仍留着这一行")
	}

	// 预读就能看到的默认行：错误里要点名"默认"，且不必再走删除口。
	err := svc.DeleteConfig(ctx, a.ID)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("默认")) {
		t.Errorf("删除默认配置的返回 = %v，期望错误里点名默认", err)
	}
	if len(repo.deleted) != 1 {
		t.Errorf("预检就该挡下，不该还调删除口（deleted = %v）", repo.deleted)
	}

	// 竞态路径：预读时 C 还不是默认，落到删除口之前被并发抬成默认 ⇒ 删除必须失败、行必须在。
	repo.beforeDelete = func(id string) {
		for _, row := range repo.rows {
			row.IsDefault = row.ID == id
		}
	}
	if err := svc.DeleteConfig(ctx, c.ID); err == nil {
		t.Error("并发把目标抬成默认之后删除仍返回 nil（唯一默认行会被删走）")
	}
	if _, ok := repo.rows[c.ID]; !ok {
		t.Error("被守卫挡下的删除把这一行删走了")
	}
	if got, _ := repo.GetDefault(ctx); got == nil || got.ID != c.ID {
		t.Errorf("并发切换之后默认行 = %+v，期望仍是 %s（零默认状态不许出现）", got, c.ID)
	}
}
