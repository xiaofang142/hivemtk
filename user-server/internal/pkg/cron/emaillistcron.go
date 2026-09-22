package cron

import (
	"context"
	"fmt"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/mail"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/storage"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// smtpSource / unsubscribeReader / emailListStore / jobTotaller 是行处理的四个外部面。
// 各自只声明本路径真正要用的那一两个方法：既让 fake 能只实现一条方法，
// 也让"这行代码到底依赖了什么"在一屏内可核。
type smtpSource interface {
	GetRandEmailSmtp(ctx context.Context) (*model.EmailSmtp, error)
}

type unsubscribeReader interface {
	ExistsByEmail(ctx context.Context, email string) (bool, error)
}

type sendFunc func(cfg mail.Config, to []string, subject, body string, isHTML bool, opts ...mail.Option) error

type emailListStore interface {
	UpdateEmailList(ctx context.Context, list model.EmailList) error
}

type jobTotaller interface {
	IncreaseSendTotal(ctx context.Context, jobsID uuid.UUID) error
	IncreaseSuccessTotal(ctx context.Context, jobsID uuid.UUID) error
	IncreaseFailTotal(ctx context.Context, jobsID uuid.UUID) error
}

// emailRowDeps 一行群发用到的全部外部依赖。
//
// 抽成组合字面量的理由与排水 worker 同源：合规判据（退订名单）、出口（退订头/页脚）与记账
// 都要能在测试里跑，而测试不该连 SMTP。真实装配见 EmailListCron。
type emailRowDeps struct {
	smtp   smtpSource
	subs   unsubscribeReader
	links  email.UnsubscribeLinker
	pixel  email.OpenPixelLinker
	mailer sendFunc
	lists  emailListStore
	jobs   jobTotaller
	// attachments 附件列 → 磁盘路径的解析器。群发行与单封共用 mail 包里那一份判据，
	// 两边各写一套就会有一边悄悄挂不上附件。
	attachments mail.AttachmentResolver
}

// bulkAttachWarnOnce 附件一项都没挂上这件事只在进程内出声一次：一波最多 10 行，
// 逐行报只会把别的日志埋掉，而原因（录入值不是本站形状）不会自己变。
var bulkAttachWarnOnce sync.Once

// bulkPixelWarnOnce 像素签不出来同样只出声一次（一波最多 10 行，逐行报等于没报）。
var bulkPixelWarnOnce sync.Once

// bulkLinkWarnOnce 缺密钥这条只在进程内出一次声（配置缺陷不会自己变，也不该刷满日志）。
var bulkLinkWarnOnce sync.Once

// EmailListCron 群发排水：一分钟一波、每波最多 10 行。
func EmailListCron() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[email_list_cron] panic recovered: %v", r)
		}
	}()
	emailListService := email.NewEmailListService()
	emailListList, err := emailListService.GetUnsentEmailList(context.Background(), 10)
	if err != nil {
		logger.Info(fmt.Sprintf("获取未发送的email列表失败 %s", err.Error()))
		return
	}

	deps := emailRowDeps{
		smtp:        email.NewEmailSmtpService(),
		subs:        repository.NewEmailUnsubscribeRepository(nil),
		links:       service.NewEmailUnsubscribeService(nil),
		pixel:       service.NewEmailOpenTrackerService(nil, nil),
		mailer:      mail.SendMail,
		lists:       emailListService,
		jobs:        email.NewEmailJobsService(),
		attachments: mail.LocalAttachments(storage.LocalAttachmentSource()),
	}

	for _, emailList := range emailListList {
		deliverEmailListRow(context.Background(), emailList, deps)
	}
}

// deliverEmailListRow 把一行待群发的地址投出去，并把结果写回行与 job 计数。
//
// 顺序即判据顺序：先要有一台可用 SMTP（没有就是空转，不该产生任何副作用），再问收件人
// 有没有退订，最后才拨 SMTP。退订检查排在签发之前 —— 已退订的地址连链接都不该签。
func deliverEmailListRow(ctx context.Context, row *model.EmailList, deps emailRowDeps) {
	emailSmtp, err := deps.smtp.GetRandEmailSmtp(ctx)
	if err != nil {
		logger.Info(fmt.Sprintf("获取随机smtp失败 %s", err.Error()))
		return
	}

	// 名单里存的是归一化后的地址（小写去空白），检查与签发都按同一形态读，
	// 否则"运营在表里手填了大写"就等于绕过退订名单。
	to := strings.ToLower(strings.TrimSpace(row.To))

	unsubscribed, err := deps.subs.ExistsByEmail(ctx, to)
	if err != nil {
		// fail-closed。排水路径对读失败是 fail-open（按未退订照发），这里反过来选：
		// 那一边每轮上限 100 封且行会留下重试，这一边一分钟一波、打完 IsSend=1 就永远
		// 不再重投 —— 照发等于"数据库抖一下，退订名单作废一波"，而且无法补救。
		// 所以不发、也不给行打完结标记，下一轮再判一次。
		logger.Warnf("[email_list_cron] 退订名单读取失败，本行不发送且留待下一轮 id=%s: %v", row.ID, err)
		return
	}
	if unsubscribed {
		// 合规跳过：只把行收口，不写 From/SendTime。
		// 不写完结时间是为了不占额度（GetTodayCountByFrom 按 from + send_time 数今日实发，
		// 一封没拨过 SMTP 的信不该吃掉日限的一个名额）；不打 job 计数同理 ——
		// send_total 的语义是"拨过 SMTP 的信"，把退订跳过计成失败只会让运营去查一条
		// 根本没坏过的链路。行必须收口：否则这一行每分钟被重捞一次，永远堵在队首。
		row.IsSend = 1
		row.IsSuccess = 0
		if e := deps.lists.UpdateEmailList(ctx, *row); e != nil {
			logger.Warnf("[email_list_cron] 退订跳过落库失败 id=%s: %v", row.ID, e)
		}
		logger.Infof("[email_list_cron] 收件人 %s 已在退订名单，跳过 id=%s", to, row.ID)
		return
	}

	unsub := ""
	jobsID := ""
	if row.JobsID != uuid.Nil {
		jobsID = row.JobsID.String()
	}
	if link, linkErr := deps.links.GenerateUnsubscribeLink(ctx, to, jobsID); linkErr != nil {
		bulkLinkWarnOnce.Do(func() {
			logger.Warnf("[email_list_cron] 退订链接签发失败（本进程只报这一次），后续群发将不带 List-Unsubscribe 头发出: %v", linkErr)
		})
	} else {
		unsub = link
	}

	// 用记录里配的那台服务器：按发信域名猜 host 等于无视运营配的自建 SMTP，
	// 而猜不出来时 autoConfig 会把 host 落成空串、端口 587 —— 那是"发不出去"，
	// 不是"发到错的地方"，所以 SendMail 里的兜底只在记录本身不全时才该生效。
	cfg := mail.Config{
		Host:     emailSmtp.Server,
		Port:     emailSmtp.Port,
		From:     emailSmtp.Username,
		Password: emailSmtp.Password,
		SSL:      emailSmtp.Port == mail.ImplicitTLSPort,
	}

	var opts []mail.Option
	if unsub != "" {
		opts = append(opts, mail.Unsubscribe(unsub))
	}
	// 附件挂不上不阻断投递（与单封侧同一口径），但"填了却一项都没挂上"必须出声一次：
	// 历史上这里静默丢附件丢了很久，因为邮件状态只记发送结果、不记部件。
	paths := mail.AttachmentPaths(row.Attachments, deps.attachments)
	if mail.AttachmentsDropped(row.Attachments, paths) {
		bulkAttachWarnOnce.Do(func() {
			logger.Warnf("[email_list_cron] 附件列非空但没有一项能挂上（只认本站上传落盘的 {yyyy}/{mm}/{文件名}），后续群发将不带附件发出")
		})
	}
	opts = append(opts, mail.AttachmentsFromPaths(paths))
	// 像素与退订链接同一档取舍：签不出来（多半是 EMAIL_TRACKING_SECRET 没配）就少一枚 img，
	// 不拦投递。但它必须出声一次 —— 否则"打开数恒为 0"会被读成"没人打开"，
	// 而真实原因是这批信根本没带过像素。
	pixel := ""
	if deps.pixel != nil {
		if url, pixelErr := deps.pixel.GenerateOpenPixelURL(ctx, to, jobsID); pixelErr != nil {
			bulkPixelWarnOnce.Do(func() {
				logger.Warnf("[email_list_cron] 打开追踪像素签发失败（本进程只报这一次），后续群发将不带像素发出: %v", pixelErr)
			})
		} else {
			pixel = url
		}
	}
	body := mail.AppendOpenPixel(mail.AppendUnsubscribeFooter(row.Content, unsub), pixel)

	// RCPT TO 用表里的原值：归一化只服务于合规查询与签发，不该悄悄改写投递地址。
	if err := deps.mailer(cfg, []string{row.To}, row.Subject, body, true, opts...); err != nil {
		logger.Info(fmt.Sprintf("发送失败:%s", err.Error()))
		finishEmailListRow(ctx, row, emailSmtp, deps, false)
		return
	}
	finishEmailListRow(ctx, row, emailSmtp, deps, true)
}

// finishEmailListRow 收口一行：打了 SMTP 就要留痕（成功与失败都算实发），并按结果累加 job 计数。
func finishEmailListRow(ctx context.Context, row *model.EmailList, emailSmtp *model.EmailSmtp, deps emailRowDeps, ok bool) {
	isSuccess := 0
	if ok {
		isSuccess = 1
	}

	row.IsSend = 1
	row.SendTime = time.Now()
	row.From = emailSmtp.Username
	row.IsSuccess = isSuccess
	if e := deps.lists.UpdateEmailList(ctx, *row); e != nil {
		logger.Warnf("[email_list_cron] 更新邮件列表状态失败 id=%s: %v", row.ID, e)
	}

	if row.JobsID == uuid.Nil {
		logger.Errorf("[email_list_cron] 行 %s 没有 job 归属，外发指标无处累加", row.ID)
		return
	}
	if e := deps.jobs.IncreaseSendTotal(ctx, row.JobsID); e != nil {
		logger.Warnf("[email_list_cron] 累加发送总数失败 jobsID=%s: %v", row.JobsID, e)
	}
	totalFn := deps.jobs.IncreaseSuccessTotal
	if !ok {
		totalFn = deps.jobs.IncreaseFailTotal
	}
	if e := totalFn(ctx, row.JobsID); e != nil {
		logger.Warnf("[email_list_cron] 累加外发结果总数失败 jobsID=%s: %v", row.JobsID, e)
	}
}
