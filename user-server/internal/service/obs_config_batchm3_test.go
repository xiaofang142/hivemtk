// obs_config_batchm3_test.go 钉服务层自己的三条口：编辑口的合并语义、测试连接口、
// 建配置口的校验与兜底（批M-3，补 §23.8 第 1、2 条登记的零覆盖，外加本轮读码新照出的
// CreateConfig 零腿）。
//
// 为什么这三条非要写在 service 层（repository 那 12 条不是已经够密了吗）：
// repository 守的是**那条写语句的形状**（白名单、谓词、0 行判定），而"哪些值会进那条
// 语句"由服务层决定。合并段整块换成"整个请求覆盖"或"整个请求丢弃"，仓储白名单仍然
// 一字不差地只写那 12 列 —— 于是 §23 那 22 条腿一条都不会红。判据在两层，就必须两层各有腿。
package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// batchM3Repo 在批M 那份替身之上补一条**真库有、替身原本没有**的行为：
// `model.ObsConfig.BeforeCreate` 会在 ID 为空时生成 uuid。服务层的 CreateConfig
// 不自己赋 ID，所以少了这一条，连续建的两行会都存进 map[""] —— 第二行原地盖掉第一行，
// "只有首行自动成默认"那一档就想测什么已经测不着了。
type batchM3Repo struct {
	*batchMObsRepo
	seq int
}

func newBatchM3Repo(rows ...*model.ObsConfig) *batchM3Repo {
	return &batchM3Repo{batchMObsRepo: newBatchMObsRepo(rows...)}
}

func (r *batchM3Repo) Create(ctx context.Context, c *model.ObsConfig) error {
	if c.ID == "" {
		r.seq++
		c.ID = fmt.Sprintf("batchm3-created-%02d", r.seq)
	}
	return r.batchMObsRepo.Create(ctx, c)
}

// batchM3FullRow 造一台 12 个可编辑列**各不相同**的配置。
// 每列一个独有值，是为了让"整行替换"这种变异在断言里无所遁形：任何一列被抹成零值，
// 报错信息就直接点名是哪一列，不必再回去猜。
func batchM3FullRow(id string) *model.ObsConfig {
	return &model.ObsConfig{
		ID:         id,
		Name:       "原名",
		Provider:   model.ObsProviderLocal,
		AccessKey:  "keep-ak",
		SecretKey:  "keep-sk",
		Bucket:     "keep-bucket",
		Region:     "keep-region",
		Endpoint:   "keep-endpoint",
		Domain:     "keep-domain",
		PathPrefix: "keep-prefix",
		Config:     "keep-config",
		MaxSize:    1 << 20,
		MaxCount:   100,
		Status:     model.ObsStatusError,
		TotalSize:  8192,
		FileCount:  7,
		IsDefault:  true,
	}
}

// batchM3Kept 返回"请求里没给、必须原样留下"的那 9 列（改动列 = name/provider/max_size）。
var batchM3Kept = []struct {
	col  string
	want string
	val  func(*model.ObsConfig) string
	dto  func(*dto.ObsConfigResponse) string
}{
	{"access_key", "keep-ak", func(c *model.ObsConfig) string { return c.AccessKey }, nil},
	{"secret_key", "keep-sk", func(c *model.ObsConfig) string { return c.SecretKey }, nil},
	{"bucket", "keep-bucket", func(c *model.ObsConfig) string { return c.Bucket }, func(d *dto.ObsConfigResponse) string { return d.Bucket }},
	{"region", "keep-region", func(c *model.ObsConfig) string { return c.Region }, func(d *dto.ObsConfigResponse) string { return d.Region }},
	{"endpoint", "keep-endpoint", func(c *model.ObsConfig) string { return c.Endpoint }, func(d *dto.ObsConfigResponse) string { return d.Endpoint }},
	{"domain", "keep-domain", func(c *model.ObsConfig) string { return c.Domain }, func(d *dto.ObsConfigResponse) string { return d.Domain }},
	{"path_prefix", "keep-prefix", func(c *model.ObsConfig) string { return c.PathPrefix }, func(d *dto.ObsConfigResponse) string { return d.PathPrefix }},
	{"config", "keep-config", func(c *model.ObsConfig) string { return c.Config }, func(d *dto.ObsConfigResponse) string { return d.Config }},
	{"max_count", "100", func(c *model.ObsConfig) string { return strconv.Itoa(c.MaxCount) }, nil},
}

// S6: UpdateConfig 是 PATCH —— 给了的才改，没给的必须留旧值。
//
// 这条腿钉的是"合并"这个动作本身，两个方向各有一格变异盯着：
// 整段条件判空改成恒真 = 整个请求覆盖（未提交的列被抹成零值），
// 改成恒假 = 整个请求丢弃（管理员点了保存、什么都没变）。
//
// ⚠ 本腿**不**断"能不能把一列清回空值"。那是 §5 N-31① 记着的缺陷（判空即"未提供"
// ⇒ 12 个可编辑列全都清不空，"去掉自定义域名"这类运维动作做不到）。它的修法要改
// `UpdateObsConfigRequest` 的字段类型（指针或显式 null）并同步前端，属对外契约变更。
// 谁改那件事，必须连带把本腿的期望改成"空串 = 清列"，别把"清不空"当成被测试保护下来的性质。
func TestBatchM_UpdateConfigIsPatchNotReplace(t *testing.T) {
	ctx := context.Background()
	row := batchM3FullRow("batchm3-s6")
	repo := newBatchMObsRepo(row)
	svc := &obsConfigService{repo: repo}

	resp, err := svc.UpdateConfig(ctx, row.ID, &dto.UpdateObsConfigRequest{
		Name:     "新名字",
		Provider: string(model.ObsProviderQiniu),
		MaxSize:  4096,
	})
	if err != nil {
		t.Fatalf("UpdateConfig 失败: %v", err)
	}
	if len(repo.updated) != 1 || repo.updated[0] != row.ID {
		t.Fatalf("编辑口调用 = %v，期望恰好一次且是这一行", repo.updated)
	}

	saved := repo.rows[row.ID]
	if saved.Name != "新名字" {
		t.Errorf("给了的列没生效：name = %q，期望 新名字（合并段被丢弃？）", saved.Name)
	}
	if saved.Provider != model.ObsProviderQiniu {
		t.Errorf("给了的列没生效：provider = %q，期望 qiniu", saved.Provider)
	}
	if saved.MaxSize != 4096 {
		t.Errorf("给了的列没生效：max_size = %d，期望 4096", saved.MaxSize)
	}
	for _, k := range batchM3Kept {
		if got := k.val(saved); got != k.want {
			t.Errorf("请求里没给的列被改写了：%s = %q，期望仍是 %q（合并段被换成整行替换？）", k.col, got, k.want)
		}
	}

	// 属主不在编辑口的四列：服务层不许动，仓储白名单是第二道（R6 钉的就是那一道）。
	if saved.Status != model.ObsStatusError {
		t.Errorf("status = %q，期望 error（编辑口不许把停用/报错的行复活）", saved.Status)
	}
	if !saved.IsDefault {
		t.Error("is_default 被编辑口摘掉了")
	}
	if saved.FileCount != 7 || saved.TotalSize != 8192 {
		t.Errorf("用量被编辑口写回：%+v，期望 7 / 8192", saved)
	}

	// 出口形状：改后的值要看得见，密钥不许原样吐回去。
	if resp.Name != "新名字" || resp.Provider != string(model.ObsProviderQiniu) || resp.ProviderName != "七牛云存储" {
		t.Errorf("DTO 未反映改动：%+v", resp)
	}
	if resp.MaxSize != 4096 {
		t.Errorf("DTO max_size = %d，期望 4096", resp.MaxSize)
	}
	for _, k := range batchM3Kept {
		if k.dto != nil && k.dto(resp) != k.want {
			t.Errorf("DTO 里 %s = %q，期望仍是 %q", k.col, k.dto(resp), k.want)
		}
	}
	if resp.SecretKey != "***" {
		t.Errorf("DTO 把 SecretKey 明文回显了：%q，期望 ***", resp.SecretKey)
	}
}

// batchM3CloudProviders 四家云厂商与各自的"配置不完整"文案 —— 文案各家不同，
// 所以要逐家钉：合并成一句通用文案会把"哪台没配好"这个唯一的可操作信息丢掉。
var batchM3CloudProviders = []struct {
	provider, label, wantErr string
}{
	{string(model.ObsProviderAliyun), "阿里云", "阿里云OSS配置不完整"},
	{string(model.ObsProviderQiniu), "七牛", "七牛云存储配置不完整"},
	{string(model.ObsProviderTencent), "腾讯云", "腾讯云COS配置不完整"},
	{string(model.ObsProviderAWS), "AWS", "AWS S3配置不完整"},
}

// S7: 四家云厂商的"测试连接"只认三个字段非空，且必须逐家报自己那一句。
//
// 这条腿同时是一份**含金量声明**，不只是绿灯：`storage.Factory` 对这四家返回的是
// `cloudStub`（`internal/storage/factory.go:50`，SDK 未接线），构造永不失败 ⇒
// 这一句 nil 的真实含义是"三个字段都不为空"，不是"连上了"。管理页据此回
// "连接测试成功" ⇒ §5 N-34 记着这一条（本批只登记不修）。
// 同理，四支里的 `驱动构造失败` 分支在 provider 已由外层 switch 收窄之后**不可达**，
// 本腿不为不可达的分支编造断言。
func TestBatchM_TestConnectionCloudArmsRequireThreeFields(t *testing.T) {
	ctx := context.Background()
	svc := &obsConfigService{repo: newBatchMObsRepo()}

	full := func(provider string) *dto.ObsConfigResponse {
		return &dto.ObsConfigResponse{
			ID: "batchm3-s7", Provider: provider,
			AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		}
	}

	for _, p := range batchM3CloudProviders {
		t.Run(p.label+"三字段齐即通过", func(t *testing.T) {
			if err := svc.TestConnection(ctx, full(p.provider)); err != nil {
				t.Errorf("TestConnection = %v，期望 nil（这一句只证明格式 OK，见本腿注释）", err)
			}
		})
		for _, col := range []struct{ name, value string }{{"access_key", "AccessKey"}, {"secret_key", "SecretKey"}, {"bucket", "Bucket"}} {
			t.Run(p.label+"缺"+col.name, func(t *testing.T) {
				cfg := full(p.provider)
				switch col.value {
				case "AccessKey":
					cfg.AccessKey = ""
				case "SecretKey":
					cfg.SecretKey = ""
				case "Bucket":
					cfg.Bucket = ""
				}
				err := svc.TestConnection(ctx, cfg)
				if err == nil {
					t.Fatalf("缺 %s 仍然报通过（管理页会显示连接成功）", col.name)
				}
				if err.Error() != p.wantErr {
					t.Errorf("错误 = %q，期望点名厂商的原话 %q", err.Error(), p.wantErr)
				}
			})
		}
	}

	// Region 在四家云厂商的 TestConnection 里**不**要求：DTO 注释（dto/obs_config.go:6-8）
	// 写的是"必须填 AccessKey / SecretKey / Bucket / Region"，实现只认三个。
	// 这一格断的是今天的真值，注释与实现的分歧登记在 §23.10，不改行为。
	t.Run("缺 region 不拦（与 DTO 注释分歧，见腿注）", func(t *testing.T) {
		cfg := full(string(model.ObsProviderAliyun))
		cfg.Region = ""
		if err := svc.TestConnection(ctx, cfg); err != nil {
			t.Errorf("TestConnection = %v，期望 nil（判据不含 region）", err)
		}
	})

	t.Run("未知_provider_必须报不支持", func(t *testing.T) {
		cfg := full("swift")
		err := svc.TestConnection(ctx, cfg)
		if err == nil || err.Error() != "不支持的存储提供商" {
			t.Errorf("未知提供商 = %v，期望「不支持的存储提供商」", err)
		}
	})
}

// batchM3LocalConfig 指向一个具体目录，避免测试落进 `./uploads` 兜底（那条支路会把
// 临时文件写进源码树，见 S8 最后一子档为什么显式设 env）。
func batchM3LocalConfig(baseDir string) *dto.ObsConfigResponse {
	return &dto.ObsConfigResponse{ID: "batchm3-s8", Provider: string(model.ObsProviderLocal), Endpoint: baseDir}
}

// S8: local 那一支是真的在探文件系统（stat + 写一个文件再删掉），五档各有独立判据。
func TestBatchM_TestConnectionLocalProbesDirectory(t *testing.T) {
	ctx := context.Background()
	svc := &obsConfigService{repo: newBatchMObsRepo()}
	base := t.TempDir()

	t.Run("目录不存在要报不存在", func(t *testing.T) {
		err := svc.TestConnection(ctx, batchM3LocalConfig(filepath.Join(base, "nope")))
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("不存在")) {
			t.Errorf("错误 = %v，期望点名「目录不存在」并带上路径", err)
		}
	})

	t.Run("路径是文件要报不是目录", func(t *testing.T) {
		file := filepath.Join(base, "a-file")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("铺夹具失败: %v", err)
		}
		err := svc.TestConnection(ctx, batchM3LocalConfig(file))
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("不是目录")) {
			t.Errorf("错误 = %v，期望「不是目录」——与「不可写」是两回事，混了就查不出配错路径", err)
		}
	})

	t.Run("可写目录通过且不留测试文件", func(t *testing.T) {
		writable := filepath.Join(base, "writable")
		if err := os.Mkdir(writable, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := svc.TestConnection(ctx, batchM3LocalConfig(writable)); err != nil {
			t.Fatalf("可写目录报失败: %v", err)
		}
		entries, err := os.ReadDir(writable)
		if err != nil {
			t.Fatalf("读目录失败: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("探测之后目录里留下 %d 个文件（`.obs_test_write` 没删干净）：%v", len(entries), namesOf(entries))
		}
	})

	// 只读目录这一档要求进程不是 root（root 无视 0500），CI 上常以 root 跑 ⇒ 显式跳过，
	// 而不是让这一档在别人的环境里以错误的理由红。
	t.Run("不可写目录要报不可写", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("以 root 运行，0500 目录照样可写，这一档无判据")
		}
		ro := filepath.Join(base, "readonly")
		if err := os.Mkdir(ro, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatalf("改权限失败: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		err := svc.TestConnection(ctx, batchM3LocalConfig(ro))
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("不可写")) {
			t.Errorf("错误 = %v，期望「不可写」", err)
		}
	})

	// Endpoint 留空时的取值链是 endpoint → STORAGE_LOCAL_BASE_DIR → ./uploads。
	// 两档各断一件事：
	// ① env 里那个目录**真的被取用了** —— 判法是指向一个不存在的目录，要求错误里点名它。
	//    如果只断"传了 env 就成功"，一旦源码树里恰好有 ./uploads，摘掉 env 回退那一格也不会红，
	//    变异格就成了碰运气（本机就不巧：internal/service 下没有 ./uploads，别处可能有）。
	// ② 真目录走通且不留探测文件。
	t.Run("endpoint_空时取环境变量", func(t *testing.T) {
		missing := filepath.Join(base, "from-env-missing")
		t.Setenv("STORAGE_LOCAL_BASE_DIR", missing)
		err := svc.TestConnection(ctx, batchM3LocalConfig(""))
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte(missing)) {
			t.Errorf("错误 = %v，期望点名 env 里那个目录 %s —— baseDir 没有取 env 的值", err, missing)
		}

		dir := filepath.Join(base, "from-env")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		t.Setenv("STORAGE_LOCAL_BASE_DIR", dir)
		if err := svc.TestConnection(ctx, batchM3LocalConfig("")); err != nil {
			t.Fatalf("env 兜底没走通: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读目录失败: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("env 兜底这一档留下 %v，期望探测文件已清理", namesOf(entries))
		}
	})
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// S9: 测试连接不许写库，也不许改写进来的那份配置。
//
// 管理页的"测试"按钮是纯读操作；一旦它开始写行，就会与并发的编辑口互相覆盖
// （R6 那一族：陈旧快照整行写回复活 is_default）。
//
// ⚠ 本腿刻意**不**断"测试失败该不该把原因写进 last_error/last_test_at"。那两列今天
// 零生产写口（§5 N-31②，管理页那两栏恒空），补写回是**修法**而不是缺陷。将来有人实现
// 它，必须新开一个只写那两列的入口（像 `UpdateStatus` 那样），届时把本腿的
// `len(repo.updated) != 0` 改成"只许走那个新入口" —— 别顺手放宽成"用 Update 写也行"。
func TestBatchM_TestConnectionDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cloud := batchM3FullRow("batchm3-s9-cloud")
	cloud.Provider = model.ObsProviderAliyun
	local := batchM3FullRow("batchm3-s9-local")
	local.Endpoint = dir
	repo := newBatchMObsRepo(cloud, local)
	svc := &obsConfigService{repo: repo}

	beforeCloud := *cloud
	beforeLocal := *local

	for _, cfg := range []*dto.ObsConfigResponse{
		{ID: cloud.ID, Provider: string(model.ObsProviderAliyun), AccessKey: "ak", SecretKey: "sk", Bucket: "b"},
		{ID: local.ID, Provider: string(model.ObsProviderLocal), Endpoint: dir},
		{ID: "batchm3-s9-bad", Provider: string(model.ObsProviderQiniu)},
		{ID: "batchm3-s9-unknown", Provider: "swift"},
	} {
		_ = svc.TestConnection(ctx, cfg)
	}

	if got := len(repo.updated) + len(repo.increments) + len(repo.setDefaults) + len(repo.deleted); got != 0 {
		t.Errorf("测试连接期间发生了写库调用（共 %d 次：update=%v increment=%v setDefault=%v delete=%v）",
			got, repo.updated, repo.increments, repo.setDefaults, repo.deleted)
	}
	if *cloud != beforeCloud || *local != beforeLocal {
		t.Errorf("测试连接改写了库里的行：cloud=%+v local=%+v", *cloud, *local)
	}
}

// S10: 建配置口的校验与兜底（本轮读 §23.8 时新照出的第三处零覆盖面）。
//
// 三条各自有牙的判据：① 云厂商三字段必填（与 TestConnection 同一把尺子，两处的
// 不一致本身就是要钉的东西）；② local 的 Endpoint/Domain 取值链，兜底值是运维
// 真的会撞上的那两个（`./uploads`、`/files`）；③ **只有表里第一行**自动成默认，
// 且新建行必须显式 active —— 这两条决定了"装完系统有没有一个能用的默认存储"，
// 而 §5 N-26 一整批都在讲默认位与 status 承重。
func TestBatchM_CreateConfigValidatesAndSeedsDefault(t *testing.T) {
	ctx := context.Background()

	t.Run("名称必填", func(t *testing.T) {
		repo := newBatchM3Repo()
		svc := &obsConfigService{repo: repo}
		_, err := svc.CreateConfig(ctx, &dto.CreateObsConfigRequest{Provider: string(model.ObsProviderLocal)})
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("名称")) {
			t.Errorf("错误 = %v，期望点名「配置名称不能为空」", err)
		}
		if len(repo.rows) != 0 {
			t.Errorf("校验失败却已经建了 %d 行", len(repo.rows))
		}
	})

	for _, col := range []struct{ label, field, want string }{
		{"AccessKey", "AccessKey", "云存储 AccessKey 不能为空"},
		{"SecretKey", "SecretKey", "云存储 SecretKey 不能为空"},
		{"Bucket", "Bucket", "云存储 Bucket 不能为空"},
	} {
		t.Run("云厂商缺"+col.label, func(t *testing.T) {
			repo := newBatchM3Repo()
			svc := &obsConfigService{repo: repo}
			req := &dto.CreateObsConfigRequest{
				Name: "c", Provider: string(model.ObsProviderTencent),
				AccessKey: "ak", SecretKey: "sk", Bucket: "b",
			}
			switch col.field {
			case "AccessKey":
				req.AccessKey = ""
			case "SecretKey":
				req.SecretKey = ""
			case "Bucket":
				req.Bucket = ""
			}
			_, err := svc.CreateConfig(ctx, req)
			if err == nil || err.Error() != col.want {
				t.Errorf("错误 = %v，期望 %q", err, col.want)
			}
			if len(repo.rows) != 0 {
				t.Error("校验失败却已经把行写进库了")
			}
		})
	}

	t.Run("未知 provider 必须报错", func(t *testing.T) {
		repo := newBatchM3Repo()
		svc := &obsConfigService{repo: repo}
		_, err := svc.CreateConfig(ctx, &dto.CreateObsConfigRequest{Name: "c", Provider: "swift"})
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("不支持的存储提供商")) {
			t.Errorf("错误 = %v，期望点名不支持的提供商", err)
		}
		if len(repo.rows) != 0 {
			t.Error("provider 都不认识，却已经把行建出来了")
		}
	})

	t.Run("本地配置取环境变量", func(t *testing.T) {
		dir, pub := t.TempDir(), "/cdn"
		t.Setenv("STORAGE_LOCAL_BASE_DIR", dir)
		t.Setenv("STORAGE_LOCAL_PUBLIC_URL", pub)
		repo := newBatchM3Repo()
		svc := &obsConfigService{repo: repo}
		if _, err := svc.CreateConfig(ctx, &dto.CreateObsConfigRequest{Name: "本地", Provider: string(model.ObsProviderLocal)}); err != nil {
			t.Fatalf("建 local 配置失败: %v", err)
		}
		saved := onlyRow3(t, repo)
		if saved.Endpoint != dir {
			t.Errorf("endpoint = %q，期望取 env 的 %q", saved.Endpoint, dir)
		}
		if saved.Domain != pub {
			t.Errorf("domain = %q，期望取 env 的 %q", saved.Domain, pub)
		}
	})

	t.Run("无_env_时用内置兜底", func(t *testing.T) {
		t.Setenv("STORAGE_LOCAL_BASE_DIR", "")
		t.Setenv("STORAGE_LOCAL_PUBLIC_URL", "")
		repo := newBatchM3Repo()
		svc := &obsConfigService{repo: repo}
		if _, err := svc.CreateConfig(ctx, &dto.CreateObsConfigRequest{Name: "本地", Provider: string(model.ObsProviderLocal)}); err != nil {
			t.Fatalf("建 local 配置失败: %v", err)
		}
		saved := onlyRow3(t, repo)
		if saved.Endpoint != "./uploads" || saved.Domain != "/files" {
			t.Errorf("兜底值 = %q / %q，期望 ./uploads 与 /files（与 init_storage.go 的 seed 同口径）", saved.Endpoint, saved.Domain)
		}
	})

	t.Run("只有首行自动成默认", func(t *testing.T) {
		t.Setenv("STORAGE_LOCAL_BASE_DIR", t.TempDir())
		repo := newBatchM3Repo()
		svc := &obsConfigService{repo: repo}
		for _, name := range []string{"第一台", "第二台"} {
			if _, err := svc.CreateConfig(ctx, &dto.CreateObsConfigRequest{Name: name, Provider: string(model.ObsProviderLocal)}); err != nil {
				t.Fatalf("建 %s 失败: %v", name, err)
			}
		}
		var defaults []string
		for _, id := range repo.order {
			row := repo.rows[id]
			if row.Status != model.ObsStatusActive {
				t.Errorf("%s 的 status = %q，期望新建行就是 active（否则它永远不会被 GetDefault 选中）", id, row.Status)
			}
			if row.IsDefault {
				defaults = append(defaults, id)
			}
		}
		if len(defaults) != 1 || defaults[0] != repo.order[0] {
			t.Errorf("自动默认落点 = %v，期望恰好首行 %v", defaults, repo.order[:1])
		}
		if got, err := svc.GetDefaultConfig(ctx); err != nil || got.Name != "第一台" {
			t.Errorf("默认配置 = %+v err=%v，期望第一台", got, err)
		}
	})
}

func onlyRow3(t *testing.T, repo *batchM3Repo) *model.ObsConfig {
	t.Helper()
	if len(repo.rows) != 1 {
		t.Fatalf("库里期望恰好 1 行，实际 %d 行", len(repo.rows))
	}
	for _, c := range repo.rows {
		return c
	}
	t.Fatal("取不到那一行（不应到达）")
	return nil
}
