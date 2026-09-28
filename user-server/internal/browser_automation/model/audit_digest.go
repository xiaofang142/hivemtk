package model

import "time"

// BrowserAuditDigest 裁剪前的**逐批内容摘要**（批22 / §8.3-6 A6）。
//
// 立项依据：`PruneBefore` 是整行删除，90 天一过，`browser_command_log` 里那段历史就一个字
// 都不剩。这不是「保留期到了」而是「证据消失」——审计面上「那三天没有命令」与「那三天的
// 命令被人在保留期外偷偷删了/删错了」完全同形，而前者是唯一可接受的解释、没人能证明它是。
// AWS CloudTrail 的处理是给每个交付窗口留一条 digest 文件（`logFiles[].hashValue` +
// `previousDigestHashValue`），删掉日志本身不影响「那段确实发生过什么」的可证性；
// RFC 6962 的 inclusion proof、pg_partman 的 `retention_keep_table=true` 同一条路。
//
// 于是口径定为：**裁剪不等于证据消失**。删之前把这一批的
// (session, 行数, seq 区间, 逐行内容哈希折成的批摘要, 接上一条的链哈希) 写进本表，
// 本表永不裁剪（一行约 100 字节，一天几十条，十年不心疼）。
//
// 两列值得单说：
//   - `batch_digest` 由**库内** sha256 逐行算出再按 id 序拼接后二次 sha256，
//     payload 全文从不过网络（一批 5000 行 × 可到 64 KiB，拉到 Go 里哈希就是把
//     治理任务变成内存炸弹）。它把「删掉的是哪些内容」钉成一个指纹：日后若有人拿出
//     那批行的副本，能验证它与摘要同源；拿不出副本的人则无法伪造一段「自洽的历史」。
//   - `prev_chain_hash` 让本表自己也是条链：只删中间某一批 digest 会当场断链，
//     否则「删日志」可以升级成「连摘要一起删、再补一段假的」。
//
// 刻意**不带** gorm.DeletedAt：软删的摘要行等于给「抹掉证据」留了一条正规通道。
// 同理本表不参与任何裁剪——它是这张表存在的全部理由的反面（被裁的那张才需要自证）。
type BrowserAuditDigest struct {
	ID        uint `gorm:"primaryKey" json:"id"`
	SessionID uint `gorm:"column:session_id;not null;uniqueIndex:uk_browser_audit_digests_session_ordinal,priority:1" json:"session_id"`

	// 一次裁剪批里属于该 session 的那一段。RowCount 与 [FirstSeq,LastSeq] 合起来
	// 给出「有没有断号」的判据：seq 是 session 内单调递增的，区间长度对不上行数就是缺行
	// ——缺的那一行是在**裁剪之前**就没了（被人删了/写丢了），摘要把它如实记下来，
	// 而不是等删完之后再无从分辨。
	RowCount int64 `gorm:"column:row_count;not null" json:"row_count"`
	FirstSeq int   `gorm:"column:first_seq;not null" json:"first_seq"`
	LastSeq  int   `gorm:"column:last_seq;not null" json:"last_seq"`
	PrevSeq  int   `gorm:"column:prev_seq;not null" json:"prev_seq"` // 同 session 上一条摘要的 LastSeq，首条 0
	// Ordinal 是本表自己的序号（不是命令行号）：同一 session 内从 1 起单调递增，
	// 与 session_id 一起唯一。唯一约束在这里的用处和批20f 同一条：重放同一次裁剪会撞约束，
	// 而不是悄悄多出一条并行的历史（本表自己若可重复，它就失去了作为凭据的资格）。
	Ordinal int `gorm:"column:ordinal;not null;uniqueIndex:uk_browser_audit_digests_session_ordinal,priority:2" json:"ordinal"`

	BatchDigest   string `gorm:"column:batch_digest;size:64;not null" json:"batch_digest"`
	PrevChainHash string `gorm:"column:prev_chain_hash;size:64;not null" json:"prev_chain_hash"`
	ChainHash     string `gorm:"column:chain_hash;size:64;not null" json:"chain_hash"`

	// Cutoff 是这次裁剪的时间界（哪些行该走），CreatedAt 是写下这一行的时刻。
	// 两者分开：运维查的是「这个界放行的行去哪了」，取证查的是「这条摘要什么时候产生的」。
	Cutoff    time.Time `gorm:"column:cutoff;not null" json:"cutoff"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (BrowserAuditDigest) TableName() string { return "browser_audit_digests" }

// BrowserAuditPruneRun 每次裁剪扫描的一行留痕（批22 / A6 的「零事件也要能自证」那一半）。
//
// 只记「删了多少」不够：删了 0 行的那次扫描同样是一个事实——它证明**在那个时间点、按那个界，
// 表里没有该走的行**。CloudTrail 的 `logFiles: []` 就是这个用法：空摘要不是「没跑」，
// 而是一句可断言的「该时段无事件」。少了这一行，「那段本来就没有日志」和
// 「扫描器那天根本没跑/跑挂了」在库里完全同形，而这两种解释的处置动作相反。
//
// 三条恒等式是本行的全部用处，读者拿它们做闭合算术（对不上就是「有行既没被裁、
// 也没留摘要地消失了」，那正是本批要让它无处遁形的那种事）：
//
//	RowsPruned == RowsBefore          // 扫描跑到「没有该走的行」才返回，中途死掉则整行不存在
//	RowsPruned == Σ 本次摘要.row_count // 每一行被删前都落进且只落进一条摘要
//	同界（Cutoff）内 Batches ≥ Σ 摘要按 5000 行的下取整 // 分批承诺没被悄悄收回
//
// 本行与摘要、删除同属一次扫描，但**不**同事务：分批的整个意义就是把锁窗口切成一批，
// 于是「扫描中断」表现为——已提交的那几批各自带着自己的摘要（自洽），只是没有这条汇总行。
// 汇总行缺席因此是可查的（末条摘要的 created_at 落在两个界之间却无 run 行），不是静默的。
type BrowserAuditPruneRun struct {
	ID     uint      `gorm:"primaryKey" json:"id"`
	Cutoff time.Time `gorm:"column:cutoff;not null;index" json:"cutoff"`

	// CutoffSource 写明**这个界是从哪来的**（批23 / §7.28 八-3）。只有 `cutoff` 一列时，
	// 库里能证明「删掉的那些行存在过、内容是什么」，却证不了「按什么界放的行」：
	// 运维把 `BROWSER_AUDIT_RETENTION_DAYS` 从 90 改成 7 再等一天，读数与一次正常裁剪
	// 逐字节同形。摘要链防的是「抹掉证据」，这一列防的是「改界这件事本身不留痕」。
	// 由调用方必填（空串时 PruneBefore 直接拒绝，一行都不删）：可选字段等于没有字段。
	CutoffSource string `gorm:"column:cutoff_source;size:64;not null" json:"cutoff_source"`

	Batches    int   `gorm:"column:batches;not null" json:"batches"`         // 本次扫描发出几条批量 DELETE
	Digests    int   `gorm:"column:digests;not null" json:"digests"`         // 写下几条摘要行（0 且 RowsPruned=0 = 空窗口自证）
	RowsPruned int64 `gorm:"column:rows_pruned;not null" json:"rows_pruned"` // 实删行数

	// RowsBefore 是入口处「按本界该走」的行数（`created_at < cutoff` 且 id ≤ 本次高水位）。
	// 不是全表行数：全表里绝大多数行还没到期，把它们算进来上面第一条恒等式就不成立。
	// 它必然被数出来（数不出来事务就回滚、这行也就不存在），所以是 int64 不是指针。
	RowsBefore int64 `gorm:"column:rows_before;not null" json:"rows_before"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (BrowserAuditPruneRun) TableName() string { return "browser_audit_prune_runs" }
