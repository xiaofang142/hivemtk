// attachment.go 附件出口：把邮件记录里的附件列换成"真能挂上的文件"，两条外发路径共用一份判定。
//
// 为什么放在 mail 包（与 unsubscribe.go 同一个理由）：单封投递走
// internal/email/service.sendActualEmail，营销群发走 internal/pkg/cron.EmailListCron。
// "粘贴进来的那串值算不算一个本站附件"写在任何一条路径的包里，另一条都会各写一份，
// 然后各自漂移。而两份历史实现（各写死一个扁平根 uploads/attachments + 抹掉全部斜杠再取
// Base）实测永远 Stat 不到文件：上传侧落盘是 {baseDir}/attachments/{yyyy}/{mm}/{uuid}.{ext}，
// 于是附件被静默丢弃，邮件照样标成已发送 —— 没人知道用户的附件从来没寄出去过。
package mail

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/gomail.v2"
)

// AttachmentResolver 把附件列里的一条录入值映射成"真能挂上的磁盘路径"。
// ok=false 表示这一条不是本站可挂的文件。
//
// 做成注入的接缝而不是包内直读配置：测试要能把附件根指到临时目录，
// 而真实根来自部署侧那组环境变量（见 storage.LocalAttachmentSource）。
type AttachmentResolver func(value string) (path string, ok bool)

// AttachmentPaths 拆逗号分隔的附件列并逐条解析，只保留解析成功的路径。
//
// 单独导出而不只藏在 Option 里：调用方要能区分"用户压根没填附件"和"填了但一项都没挂上"，
// 后者是需要出声的静默失败 —— 消息选项本身带不出这个信息。
func AttachmentPaths(csv string, resolve AttachmentResolver) []string {
	var paths []string
	for _, item := range attachmentEntries(csv) {
		if path, ok := resolve(item); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

// attachmentEntries 拆出附件列里的非空条目（列里存的是 "a.pdf,b.pdf" 这种形状）。
func attachmentEntries(csv string) []string {
	var items []string
	for _, item := range strings.Split(csv, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// AttachmentsDropped 判"附件列填了值却一项都没解析出来"。
//
// 单独成函数是为了让这个判定本身可断：两条外发路径都靠它决定要不要出声，
// 而"填了附件、邮件已发送、附件从来没过"正是这一格安静了很久的原因。
func AttachmentsDropped(csv string, paths []string) bool {
	return len(attachmentEntries(csv)) > 0 && len(paths) == 0
}

// AttachmentsFromPaths 把已解析出的路径作为附件挂上消息。
//
// 接收路径而不是原始 CSV：解析留在外面，发信侧才能先看条数再决定要不要出声；
// 空列表什么都不做 —— 附件挂不上不该让整封信发不出去。
func AttachmentsFromPaths(paths []string) Option {
	return func(m *gomail.Message) {
		for _, path := range paths {
			m.Attach(path)
		}
	}
}

// LocalAttachments 构造一个只认本站上传落盘形状的解析器。
//
// dir 是附件根（{baseDir}/{folder}），urlPrefix 是它对外的 URL 前缀（两者都来自上传侧
// 同一组环境变量）。只接 {prefix}/{folder}/{yyyy}/{mm}/{name} 这一种形状，理由有两层：
//   - 这是 LocalDriver 真正写出来的唯一形状，别的值挂不上就是挂不上；
//   - 段数与数字段一钉死，最后一段就不可能带目录分隔符，附件根天然出不去 ——
//     不需要再补一道越界检查，也不需要靠"抹掉所有斜杠"那种把合法值一起废掉的做法防越界。
//
// host 不参与比对：值可能来自配了绝对公开地址的部署，也可能是粘贴时带上了同样的路径
// 而域名不同。越界风险由"必须落在附件根的既定形状里"兜住，而不是由认得自家域名兜住 ——
// 后者会让没配公开域名的部署重新回到静默不附。
func LocalAttachments(dir, urlPrefix string) AttachmentResolver {
	// 前缀自己也只取路径段：公开地址配成裸源站（https://host，不带路径）时，
	// 上传接口回的是 https://host/attachments/{yyyy}/{mm}/…，认原样比对就永远切不出来。
	root, _ := publicPath(urlPrefix)
	prefix := strings.TrimRight(root, "/") + "/" + filepath.Base(filepath.Clean(dir)) + "/"
	return func(value string) (string, bool) {
		path, ok := publicPath(value)
		if !ok {
			return "", false
		}
		rel, found := strings.CutPrefix(path, prefix)
		if !found {
			return "", false
		}
		if !isStorageLayout(rel) {
			return "", false
		}
		local := filepath.Join(dir, filepath.FromSlash(rel))
		// Lstat 而不是 Stat：形状对了也不允许最后一段是软链接 —— 跟过去之后读的是链接
		// 指向的文件，那正是"必须落在附件根里"这条界要挡的东西。
		info, err := os.Lstat(local)
		if err != nil || !info.Mode().IsRegular() {
			return "", false
		}
		return local, true
	}
}

// publicPath 取一个值的路径段：先丢掉 scheme 与 host，再丢掉 query 与 fragment。
//
// 不解码：%2f 之类的编码斜杠在这里就只是普通字符，不会变成路径分隔符。
func publicPath(value string) (string, bool) {
	if i := strings.Index(value, "://"); i >= 0 {
		rest := value[i+3:]
		j := strings.Index(rest, "/")
		if j < 0 {
			return "", false
		}
		value = rest[j:]
	}
	if j := strings.IndexAny(value, "?#"); j >= 0 {
		value = value[:j]
	}
	return value, value != ""
}

// isStorageLayout 判"相对附件根的这一串是不是 {yyyy}/{mm}/{name} 三段"。
//
// 只判形状，不判名字本身：名字那一段既然不含分隔符（段数已钉死），就越不出根；
// 空名、带 NUL 的名字、指向目录的软链接，也都由后面的"必须是个常规文件"那一条挡掉，
// 在这里再写一遍只会多出一条永远走不到、也永远杀不死的腿。
func isStorageLayout(rel string) bool {
	segments := strings.Split(rel, "/")
	return len(segments) == 3 && allDigits(segments[0], 4) && allDigits(segments[1], 2)
}

func allDigits(s string, want int) bool {
	if len(s) != want {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
