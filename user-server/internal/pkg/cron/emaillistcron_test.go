// emaillistcron_test.go 群发路径（唯一真在跑的营销外发）的三条判据：退订名单、SMTP 记录、记账口径。
package cron

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/mail"

	"github.com/google/uuid"
	"gopkg.in/gomail.v2"
)

type fakeSubscriber struct {
	unsubscribed bool
	err          error
	gotEmail     string
}

func (f *fakeSubscriber) ExistsByEmail(_ context.Context, email string) (bool, error) {
	f.gotEmail = email
	return f.unsubscribed, f.err
}

type fakeLinkSigner struct {
	link        string
	err         error
	gotEmail    string
	gotJobID    string
	calls       int
	failOpenLog string
}

func (f *fakeLinkSigner) GenerateUnsubscribeLink(_ context.Context, email, jobID string) (string, error) {
	f.calls++
	f.gotEmail = email
	f.gotJobID = jobID
	return f.link, f.err
}

type fakeSmtp struct {
	cfg   *model.EmailSmtp
	err   error
	calls int
}

func (f *fakeSmtp) GetRandEmailSmtp(context.Context) (*model.EmailSmtp, error) {
	f.calls++
	return f.cfg, f.err
}

type recordedSend struct {
	cfg     mail.Config
	to      []string
	subject string
	body    string
	headers map[string]string
}

type fakeMailer struct {
	sends []recordedSend
	err   error
}

func (f *fakeMailer) SendMail(cfg mail.Config, to []string, subject, body string, isHTML bool, opts ...mail.Option) error {
	rec := recordedSend{cfg: cfg, to: to, subject: subject, body: body}
	// Option 是 func(*gomail.Message)，把它作用到一条新消息上就是"这封信真发出去时头会长什么样"。
	m := gomail.NewMessage()
	for _, o := range opts {
		o(m)
	}
	rec.headers = map[string]string{}
	for _, field := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if got := m.GetHeader(field); len(got) > 0 {
			rec.headers[field] = got[0]
		}
	}
	f.sends = append(f.sends, rec)
	return f.err
}

type fakeLists struct {
	updates []model.EmailList
	err     error
}

func (f *fakeLists) UpdateEmailList(_ context.Context, list model.EmailList) error {
	f.updates = append(f.updates, list)
	return f.err
}

type fakeJobs struct {
	send, success, fail []uuid.UUID
	err                 error
}

func (f *fakeJobs) IncreaseSendTotal(_ context.Context, id uuid.UUID) error {
	f.send = append(f.send, id)
	return f.err
}

func (f *fakeJobs) IncreaseSuccessTotal(_ context.Context, id uuid.UUID) error {
	f.success = append(f.success, id)
	return f.err
}

func (f *fakeJobs) IncreaseFailTotal(_ context.Context, id uuid.UUID) error {
	f.fail = append(f.fail, id)
	return f.err
}

func emailRowFixture() *model.EmailList {
	return &model.EmailList{
		ID:      uuid.New(),
		To:      "Lead@Example.com",
		Subject: "本月新品",
		Content: "<p>正文</p>",
		JobsID:  uuid.New(),
	}
}

func smtpFixture() *model.EmailSmtp {
	return &model.EmailSmtp{
		Name:     "运营小号",
		Server:   "smtp.custom.example",
		Port:     2525,
		Username: "ops@example.com",
		Password: "secret-value-not-printed",
		Limit:    500,
	}
}

type rowFixture struct {
	deps  emailRowDeps
	mails *fakeMailer
	subs  *fakeSubscriber
	links *fakeLinkSigner
	lists *fakeLists
	jobs  *fakeJobs
}

func newRowFixture() rowFixture {
	m := &fakeMailer{}
	s := &fakeSubscriber{}
	l := &fakeLinkSigner{link: "https://crm.example.com/u?t=1"}
	lst := &fakeLists{}
	j := &fakeJobs{}
	sm := &fakeSmtp{cfg: smtpFixture()}
	deps := emailRowDeps{smtp: sm, subs: s, links: l, mailer: m.SendMail, lists: lst, jobs: j}
	return rowFixture{deps: deps, mails: m, subs: s, links: l, lists: lst, jobs: j}
}

// TestDeliverEmailListRowSkipsUnsubscribed 退订名单必须拦得住群发。
//
// 单封投递与排水路径早就在查 email_unsubscribes，唯独这条真正做营销群发的路径不查 ——
// 于是"退订了却每波都被发到"，而这恰恰是退订义务唯一会被检验的场景（一天一波，每次万人）。
// 记账口径：合规跳过既不进 send_total 也不进 fail_total。send_total 的语义是"拨过 SMTP 的信"，
// 把退订跳过计成失败，运营会去查一条根本没坏过的 SMTP 链路。
func TestDeliverEmailListRowSkipsUnsubscribed(t *testing.T) {
	f := newRowFixture()
	f.subs.unsubscribed = true
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 0 {
		t.Fatalf("已退订地址仍被投递 %d 次", len(f.mails.sends))
	}
	if f.subs.gotEmail != "lead@example.com" {
		t.Errorf("退订检查用了未归一化的地址 %q（名单里存的是小写）", f.subs.gotEmail)
	}
	if len(f.lists.updates) != 1 || f.lists.updates[0].IsSend != 1 {
		t.Errorf("跳过行未被收口：%+v", f.lists.updates)
	}
	// 收口≠记账：没拨过 SMTP 的行不该写 from/send_time，否则 GetTodayCountByFrom 会把它
	// 数进"今日实发"，退订得越多反而越早撞满 Limit、把整波正常投递堵住。
	if got := f.lists.updates[0]; !got.SendTime.IsZero() || got.From != "" {
		t.Errorf("合规跳过被记成了实发：from=%q send_time=%v", got.From, got.SendTime)
	}
	if len(f.jobs.send)+len(f.jobs.success)+len(f.jobs.fail) != 0 {
		t.Errorf("合规跳过被计进了外发指标：%+v", f.jobs)
	}
}

// TestDeliverEmailListRowFailsClosedWhenListUnreadable 退订名单读不出来时不发。
//
// 与排水路径的反向选择（读失败按未退订处理）在这里不成立：那条路径每轮上限 100 封且行会留下重试，
// 这一条一分钟一波、每波最多 10 行且标完 IsSend=1 就永远不再重投 —— 读失败照发等于
// "数据库抖一下，退订名单作废一波"。所以这里判 fail-closed：不发、也不给行打完结标记，下一轮再看。
func TestDeliverEmailListRowFailsClosedWhenListUnreadable(t *testing.T) {
	f := newRowFixture()
	f.subs.err = errors.New("db down")
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 0 {
		t.Fatalf("退订名单读失败仍发信 %d 次", len(f.mails.sends))
	}
	if len(f.lists.updates) != 0 {
		t.Errorf("读失败却把行标成已完结（下一轮再也捞不回来）：%+v", f.lists.updates)
	}
}

// TestDeliverEmailListRowCarriesUnsubscribeAndRealSmtp 正常一行的两个形状：
// SMTP 用记录里配的那台（不是按域名猜的），退订出口带 job 归属。
func TestDeliverEmailListRowCarriesUnsubscribeAndRealSmtp(t *testing.T) {
	f := newRowFixture()
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("投递 %d 次，期望 1 次", len(f.mails.sends))
	}
	sent := f.mails.sends[0]
	if sent.cfg.Host != "smtp.custom.example" || sent.cfg.Port != 2525 {
		t.Errorf("用了 %s:%d，期望 SMTP 记录里的 smtp.custom.example:2525（按域名猜等于无视运营配的那台）",
			sent.cfg.Host, sent.cfg.Port)
	}
	// 2525 是 STARTTLS 端口。SSL 一律为真的话，gomail 会在明文端口上立刻发起 TLS 握手 ——
	// 那是"当场拨不通"，比"发到错的地方"更早暴露，但同样不该由这条路径制造。
	if sent.cfg.SSL {
		t.Error("非 465 端口被当成隐式 TLS（SSL=true）")
	}
	if sent.cfg.From != "ops@example.com" {
		t.Errorf("发信账号 = %q，期望 SMTP 记录里的登录名", sent.cfg.From)
	}
	if len(sent.to) != 1 || sent.to[0] != "Lead@Example.com" {
		t.Errorf("投递地址被归一化改写成了 %v（归一化只用于合规查询与签发）", sent.to)
	}
	if got := sent.headers["List-Unsubscribe"]; got != "<"+f.links.link+">" {
		t.Errorf("群发信缺退订头：%v", sent.headers)
	}
	if got := sent.headers["List-Unsubscribe-Post"]; got != "List-Unsubscribe=One-Click" {
		t.Errorf("群发信缺一键退订声明：%v", sent.headers)
	}
	if !strings.Contains(sent.body, "退订") {
		t.Errorf("正文没有退订页脚：%q", sent.body)
	}
	if !strings.HasPrefix(sent.body, "<p>正文</p>") {
		t.Errorf("页脚把原正文覆盖了：%q", sent.body)
	}
	if f.links.gotEmail != "lead@example.com" || f.links.gotJobID != row.JobsID.String() {
		t.Errorf("退订链接归属错位：email=%q job=%q（期望 job=%q）",
			f.links.gotEmail, f.links.gotJobID, row.JobsID.String())
	}
	if len(f.jobs.send) != 1 || len(f.jobs.success) != 1 {
		t.Fatalf("记账不完整：send=%v success=%v", f.jobs.send, f.jobs.success)
	}
	// 计数打在哪个 job 上也要判：只数次数的话，把每一封都记到同一个（甚至 uuid.Nil）job 上看不出来。
	if f.jobs.send[0] != row.JobsID || f.jobs.success[0] != row.JobsID {
		t.Errorf("外发指标记到了别的 job 上：send=%v success=%v，期望 %v",
			f.jobs.send[0], f.jobs.success[0], row.JobsID)
	}
	upd := f.lists.updates[0]
	if upd.IsSuccess != 1 || upd.IsSend != 1 {
		t.Errorf("行状态未收口：%+v", f.lists.updates)
	}
	// 记账列写的必须是登录账号：额度统计（GetTodayCountByFrom）按这一列数今日实发，
	// 写成展示名就等于两边各数各的 —— 这条断言与限流那一半是同一个键的上下两道锁。
	if upd.From != "ops@example.com" || upd.SendTime.IsZero() {
		t.Errorf("实发留痕错位：from=%q send_time_zero=%v", upd.From, upd.SendTime.IsZero())
	}
}

// TestDeliverEmailListRowStillSendsWithoutLink 缺密钥（签发失败）不阻断群发，只是没有退订头。
//
// 与单封路径同一条取舍：配置缺陷不该放大成整波停摆。
func TestDeliverEmailListRowStillSendsWithoutLink(t *testing.T) {
	f := newRowFixture()
	f.links.err = errors.New("EMAIL_UNSUBSCRIBE_SECRET 未配置")
	f.links.link = ""

	deliverEmailListRow(context.Background(), emailRowFixture(), f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("签发失败把群发也阻断了（投递 %d 次）", len(f.mails.sends))
	}
	sent := f.mails.sends[0]
	if _, ok := sent.headers["List-Unsubscribe"]; ok {
		t.Error("没有可用链接却写了退订头")
	}
	if strings.Contains(sent.body, "退订") {
		t.Errorf("没有可用链接仍在正文塞了指向不明的退订文字：%q", sent.body)
	}
}

// TestDeliverEmailListRowMarksFailure 投递失败：行标已发未成功、只进 fail_total。
func TestDeliverEmailListRowMarksFailure(t *testing.T) {
	f := newRowFixture()
	f.mails.err = errors.New("smtp dial refused")
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.jobs.send) != 1 || len(f.jobs.fail) != 1 || len(f.jobs.success) != 0 {
		t.Fatalf("记账错位：send=%d fail=%d success=%d", len(f.jobs.send), len(f.jobs.fail), len(f.jobs.success))
	}
	if f.jobs.send[0] != row.JobsID || f.jobs.fail[0] != row.JobsID {
		t.Errorf("失败计数记到了别的 job 上：send=%v fail=%v，期望 %v", f.jobs.send[0], f.jobs.fail[0], row.JobsID)
	}
	if len(f.lists.updates) != 1 || f.lists.updates[0].IsSuccess != 0 || f.lists.updates[0].IsSend != 1 {
		t.Errorf("失败行状态错位：%+v", f.lists.updates)
	}
}

// TestDeliverEmailListRowWithoutSmtpQuota 一台可用 SMTP 都没有时不发、不记账、不标行。
func TestDeliverEmailListRowWithoutSmtpQuota(t *testing.T) {
	f := newRowFixture()
	f.deps.smtp = &fakeSmtp{err: errors.New("没有找到可用的smtp")}

	deliverEmailListRow(context.Background(), emailRowFixture(), f.deps)

	if len(f.mails.sends) != 0 || len(f.jobs.send) != 0 || len(f.lists.updates) != 0 {
		t.Errorf("无可用 SMTP 时仍有副作用：sends=%d send_total=%d updates=%d",
			len(f.mails.sends), len(f.jobs.send), len(f.lists.updates))
	}
}

// readNonCommentLines 读一个源码文件的非注释行。
//
// 剔注释不是洁癖：静态锁判的是"接了几次"，而这套判据的动机、字段名乃至反例都会写进注释里。
// 数全文的话，一句「不要重复接 subs:」就把计数抬到 2，锁会因为写注释而红 —— 那种门没人会留着。
func readNonCommentLines(t *testing.T, name string) []string {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func countLinesContaining(lines []string, substr string) int {
	hits := 0
	for _, line := range lines {
		if strings.Contains(line, substr) {
			hits++
		}
	}
	return hits
}

// compactLines 摘掉每行里的空白字符，免得判据绑在 gofmt 的字段对齐上 —— 对齐宽度会随同结构体里
// 最长的字段名变化，绑上去的门会因为一次无关改名而红。
func compactLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.Join(strings.Fields(line), "")
	}
	return out
}

// TestDeliverEmailListRowWithoutJob 无 job 归属的行照发，但不碰指标表、也不按全零 job 签发。
//
// 计数器无处可加（加到 uuid.Nil 上等于给一个不存在的 job 记功），但发信本身不该被这件事阻断 ——
// 地址是运营自己导进来的，指标只是记账。签发同理：job 字段留空，退订落库时不该记一个全零归属。
func TestDeliverEmailListRowWithoutJob(t *testing.T) {
	f := newRowFixture()
	row := emailRowFixture()
	row.JobsID = uuid.Nil

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("没有 job 归属就不发信（投递 %d 次）", len(f.mails.sends))
	}
	if len(f.lists.updates) != 1 || f.lists.updates[0].IsSuccess != 1 {
		t.Errorf("行未收口：%+v", f.lists.updates)
	}
	if len(f.jobs.send)+len(f.jobs.success)+len(f.jobs.fail) != 0 {
		t.Errorf("指标被加到了 uuid.Nil 这个不存在的 job 上：send=%v success=%v fail=%v",
			f.jobs.send, f.jobs.success, f.jobs.fail)
	}
	if f.links.gotJobID != "" {
		t.Errorf("退订链接带上了全零 job 归属 %q", f.links.gotJobID)
	}
}

// TestEmailListCronWiresComplianceRead 静态锁：真实装配必须把退订读取与签发接到行处理上。
//
// 判据本身有上面的行为用例，但"cron 里那台 deps 到底接没接退订名单"只能在装配点看到 ——
// 这一格和 R21 是同一个病灶的形状（方法在、没人接）。
func TestEmailListCronWiresComplianceRead(t *testing.T) {
	src := compactLines(readNonCommentLines(t, "emaillistcron.go"))
	// 判到"接的是哪一个构造函数"为止，不止判键名：把 subs 换成 nil、或把 links 换成一个
	// 不签发的实现，键名都还在、行为却退回"照发给已退订地址"。
	// 上界同样要判：接两次意味着有一份是死的。
	for _, wiring := range []string{
		"smtp:email.NewEmailSmtpService(),",
		"subs:repository.NewEmailUnsubscribeRepository(nil),",
		"links:service.NewEmailUnsubscribeService(nil),",
		"mailer:mail.SendMail,",
		"lists:emailListService,",
		"jobs:email.NewEmailJobsService(),",
	} {
		if got := countLinesContaining(src, wiring); got != 1 {
			t.Errorf("deps 装配里 %s 命中 %d 次，期望恰好 1 次", wiring, got)
		}
	}
	// 入口本身也得还挂着：R21 的形状就是"判据在、非测试调用点为 0"。
	if got := countLinesContaining(compactLines(readNonCommentLines(t, "cron.go")), "EmailListCron("); got != 1 {
		t.Errorf("cron.go 里 EmailListCron 的注册命中 %d 次，期望 1 次 ⇒ 群发排水没人触发", got)
	}
}
