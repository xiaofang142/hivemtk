// kbrelease.go 知识库"发布制"的读闸门与待发布版本分配（新规划 T-P9-02 / G-5）。
//
// 一句话口径：一段**全仓唯一**的 SQL 片段决定一条 chunk 今天能不能被检索召回，
// 加上一个把新写入路由进"待发布桶"的分配函数；除此以外本包不放任何业务。
//
// 为什么闸门要放叶子包而不是 internal/service：
//   - 召回查询全在 aiagent 侧（rag/retrieval、knowledge/service），而 aiagent 不得
//     import internal/repository / internal/service（check-architecture.sh 的既有口径）；
//   - 反过来把闸门搬到 service 侧，等于让读路径跨层，比重复一份片段更贵。
//     所以本包按**表**劈：kb_releases 上"每个写侧都要用"的那一次点查 + 分配小写入
//     在这里，kb_change_requests / kb_change_audit_logs 的读写全在 internal/repository。
//     两侧文件头互指，别只改一边。
//
// 三态旗子 FF_LTC_KB_CHANGE_GATE 与库行上的 governed 是**两道独立的锁**（同 T-P2-05
// kb_canary.go 的口径）：旗子 off ⇒ 召回 SQL 与今天逐字节相同，无论库里怎么配；
// 旗子 on 但某库 governed=false ⇒ 那个库一条都不隐藏。默认态对线上零影响，
// 出事关旗子即刻回到今天，不需要清任何数据。
package kbrelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// FlagEnv 读闸门总开关。可用值 off | shadow | on。
const FlagEnv = "FF_LTC_KB_CHANGE_GATE"

type mode string

const (
	modeOff    mode = "off"
	modeShadow mode = "shadow"
	modeOn     mode = "on"
)

// parseMode 解析开关值。布尔式真值（true/1/yes）只到 shadow：一把能改变生产召回集的
// 旗子不该因为有人按习惯写了 `=true` 就直接拿到"少返几条"的能力，要真拦必须显式 on。
// 认不出的值判 off 并告警。（与 service.parseKBCanaryMode 同族取舍。）
func parseMode(raw string) mode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return modeOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return modeOff
	case "shadow", "observe", "watch", "log", "report":
		return modeShadow
	case "on", "enforce", "active":
		return modeOn
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return modeShadow
		}
		return modeOff
	}
	switch v {
	case "yes", "y":
		return modeShadow
	}
	logger.Warnf("[kb-release] %s=%q 无法识别 ⇒ 按 off 处理（召回不受版本闸门约束）；可用值：off|shadow|on", FlagEnv, raw)
	return modeOff
}

func modeValue() mode { return parseMode(os.Getenv(FlagEnv)) }

// ModeForLog 供启动期日志与运营端点回显当前旗子态（每次读 env，支持热切）。
func ModeForLog() string { return string(modeValue()) }

// GateOn 生产召回是否真的挂闸门。shadow 返回 false —— 观察期必须什么都不改，
// 否则它就不是"什么都不改"，也就没有对照基线。
func GateOn() bool { return modeValue() == modeOn }

// hiddenPredicate 一条 chunk 相对其所属库的生效版本是否应当**隐藏**。
//
// 判据写成相关子查询而不是先查版本号再拼常量，理由有两个：
//  1. 全产品召回（product_id 为空的检索分支）一次要跨几十个库，只有相关子查询能
//     逐行按各自的库判定；
//  2. 发布/回滚因此只是 kb_releases 上的指针移动，召回侧不需要重建任何东西（AC②）。
//
// 两条恒等式是这套语义不伤存量的全部依据，改动前先读完 model.KBRelease 的文档：
//   - 没有 release 行的库 ⇒ NOT EXISTS ⇒ 一条都不隐藏；
//   - kb_version = 0（升级前的存量行）在 effective_version >= 0 时永不落入
//     "kb_version > effective"，只有被显式 retire 才会消失。
//     所以"启用发布制"这个动作本身不会让任何人已上线的内容凭空不可见。
const hiddenPredicate = `EXISTS (
	SELECT 1 FROM kb_releases kr
	WHERE kr.product_id = knowledge_chunks.product_id
	  AND kr.governed
	  AND (knowledge_chunks.kb_version > kr.effective_version
	       OR (knowledge_chunks.retired_version > 0
	           AND knowledge_chunks.retired_version <= kr.effective_version))
)`

// VisiblePredicate 给 GORM 链式 Where 用（自己不带 AND）。闸门未开时返回空串。
func VisiblePredicate() string {
	if !GateOn() {
		return ""
	}
	return "NOT " + hiddenPredicate
}

// AndVisible 给裸 SQL 拼接用（自带前导 AND）。闸门未开时返回**空串**——
// 拼接点必须在拿到非空时才追加，别把空串塞进 SQL 尾巴上。
func AndVisible() string {
	p := VisiblePredicate()
	if p == "" {
		return ""
	}
	return " AND " + p
}

// WhereVisible 给 GORM 链式调用用：闸门开着才挂条件，关着原样返回那条链。
//
// 必须有这一扇而不是让每个调用点自己写 `if pred != ""`：把 `Where("")` 直接挂上去
// 会让 GORM 生成一段空条件（不同版本表现不一致），而"忘判空"的失效方向是**查询报错**，
// 比少一层过滤更难查。判空收在唯一一处，11 个召回点只多一行。
func WhereVisible(q *gorm.DB) *gorm.DB {
	if q == nil {
		return nil
	}
	if p := VisiblePredicate(); p != "" {
		return q.Where(p)
	}
	return q
}

// EnsureDraftStamp 把一次 chunk 写入路由进该库的待发布桶，返回要打上的版本号。
//
// 返回 0 的两种情况都**必须**原样写进 kb_version（0 = 不受闸门管的存量语义）：
//   - productID 为空（跨库全量重建这类调用方没给归属）；
//   - 该库没有 release 行或 governed=false（还没进发布制）。
//
// 快路径是**一次唯一索引点查**，未 governance 的库永远走不到 UPSERT。
// 待发布桶一次分配的幂等性：draft 已存在就复用（同一批导入的多条 chunk 落同一个桶，
// 一次发布把它们整体上线）；为 0 才从 allocated 高水位往前挪一格。
func EnsureDraftStamp(ctx context.Context, db *gorm.DB, productID string) (int, error) {
	if db == nil || productID == "" {
		return 0, nil
	}
	var cur struct {
		Governed bool `gorm:"column:governed"`
		Draft    int  `gorm:"column:draft_version"`
	}
	if err := db.WithContext(ctx).Table("kb_releases").
		Select("governed, draft_version").
		Where("product_id = ?", productID).
		Scan(&cur).Error; err != nil {
		return 0, fmt.Errorf("kb_releases 待发布版本读取失败: %w", err)
	}
	if !cur.Governed {
		return 0, nil
	}
	if cur.Draft > 0 {
		return cur.Draft, nil
	}
	var got int
	// 分配新桶。ON CONFLICT 依赖 kb_releases.product_id 上的唯一索引（AutoMigrate 建），
	// 且整句是幂等的：并发两路导入同时走到这里，后者在行锁上排队，醒过来时
	// CASE 看到的已是前者推进过的 draft>0 ⇒ 两路拿到同一个桶号。
	if err := db.WithContext(ctx).Exec(AllocateDraftSQL, productID).Error; err != nil {
		return 0, fmt.Errorf("kb_releases 待发布版本分配失败: %w", err)
	}
	if err := db.WithContext(ctx).Table("kb_releases").
		Select("draft_version").
		Where("product_id = ?", productID).
		Scan(&got).Error; err != nil {
		return 0, fmt.Errorf("kb_releases 待发布版本分配后读取失败: %w", err)
	}
	if got <= 0 {
		// 分配套路走完仍是 0，只能是 governed 与 draft 之间被并发改过（例如同时被
		// 关掉治理）。宁可让写入方拿到错误，也不要静默写 0 —— 0 意味着"这条内容
		// 不受闸门管"，那是一次**放行**，不能由分配失败代答。
		return 0, fmt.Errorf("kb_releases 分配后 draft_version 仍为 %d（product=%s）", got, productID)
	}
	return got, nil
}

// AllocateDraftSQL 上一步真正执行分配的语句（ repository 侧与测试共用一份，避免两处各写一遍）。
//
// 参数 1 = product_id。语义：governed 为假时**不**建行（建了就把一个没进发布制的库
// 变成了 governed=false 的空行，读起来像"这个库被治理过"）；governed 为真且 draft>0
// 时原样返回已有桶号。
const AllocateDraftSQL = `
INSERT INTO kb_releases AS kr (product_id, governed, effective_version, previous_version,
                               recalled_version, draft_version, allocated_version, changed_by)
SELECT s.product_id, true, 0, 0, 0, s.allocated_version + 1, s.allocated_version + 1, 'system:import'
FROM kb_releases s
WHERE s.product_id = ? AND s.governed AND s.draft_version = 0
ON CONFLICT (product_id) DO UPDATE
SET draft_version = CASE WHEN kr.draft_version > 0 THEN kr.draft_version
                         ELSE kr.allocated_version + 1 END,
    allocated_version = CASE WHEN kr.draft_version > 0 THEN kr.allocated_version
                             ELSE kr.allocated_version + 1 END
`

// CountWouldHide 影子档 observability：这批召回结果里有多少条**在 on 档会被隐藏**。
//
// 只用于观察，不参与任何判定；失败由调用方按"出声即可"处理（见 LogWouldHide）。
// 表名写死 knowledge_chunks：片段里的相关子查询锚的就是这个裸表名，换表要重写片段。
func CountWouldHide(ctx context.Context, db *gorm.DB, chunkIDs []uint64) (int64, error) {
	if db == nil || len(chunkIDs) == 0 {
		return 0, nil
	}
	var n int64
	if err := db.WithContext(ctx).Table("knowledge_chunks").
		Select("COUNT(*)").
		Where("id IN ?", chunkIDs).
		Where(hiddenPredicate).
		Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// LogWouldHide 影子档出声：召回结果里有几条本该被闸门挡掉。
//
// 不返回错误是刻意的——观察通路绝不允许把一次检索弄红（口径同 RagSearcher 的
// loadChunkWeights 失败只 Warn）。
func LogWouldHide(ctx context.Context, db *gorm.DB, ids []uint64, productID, where string) {
	if modeValue() != modeShadow || len(ids) == 0 {
		return
	}
	n, err := CountWouldHide(ctx, db, ids)
	if err != nil {
		logger.Warnf("[kb-release] shadow 统计失败（%s product=%s）: %v", where, productID, err)
		return
	}
	if n > 0 {
		logger.Infof("[kb-release] shadow: %s product=%s 召回 %d 条中有 %d 条在 %s=on 时会被版本闸门隐藏",
			where, productID, len(ids), n, FlagEnv)
	}
}

// StampForWrite 把一次 chunk 写入按库路由进各自的待发布桶（就地改写传入切片的版本列）。
//
// 为什么打戳放在仓储这一层而不是各个导入入口：knowledge_chunks 的插入通路有好几条
// （文档导入、增量重建、商家手工补段、发布事务），漏掉任何一条的表现都是"这段内容
// 没进版本账"，而它一旦带着 kb_version=0 落库就**永远**不受闸门管 —— 那是 AC① 的
// 绕过口，且无声。收在写入点上一次做完，判据只有一处。
//
// 返回的 stamp 只在 version>0 时改写：0 的语义是"该库没进发布制"（见 EnsureDraftStamp），
// 把 0 写回去等于给一个未治理的库打上"待发布"，闸门会在第一次 on 时把它的存量挡掉。
//
// 按 product_id 分组各查一次：一批导入通常同属一个库，跨库全量重建才会走多组。
func StampForWrite(ctx context.Context, db *gorm.DB, chunks []model.KnowledgeChunk) error {
	if db == nil || len(chunks) == 0 {
		return nil
	}
	// off 档一行都不碰：闸门不参与判定时，写进版本列的号没有任何消费者，
	// 而一次写入多带一次点查就是纯开销（口径同 VisiblePredicate 在 off 档返回空串）。
	// shadow 档**要**打：观察期的 would-hide 读数必须按真实的版本号算出来，
	// 否则"切到 on 之后会发生什么"这个问题只能靠猜。
	if !ShadowsOrOn() {
		return nil
	}
	stamped := make(map[string]int, 4)
	for i := range chunks {
		pid := chunks[i].ProductID
		if pid == "" {
			continue
		}
		v, ok := stamped[pid]
		if !ok {
			var err error
			v, err = EnsureDraftStamp(ctx, db, pid)
			if err != nil {
				return err
			}
			stamped[pid] = v
		}
		if v <= 0 {
			continue
		}
		chunks[i].KBVersion = v
		chunks[i].RetiredVersion = 0
	}
	return nil
}

// ShadowsOrOn 闸门是否已进入"会影响写侧版本账"的档位（shadow 或 on）。
func ShadowsOrOn() bool {
	m := modeValue()
	return m == modeShadow || m == modeOn
}

// StampOneForWrite 单条插入的版本打戳（StampForWrite 的指针形状）。
//
// 单独一扇是因为 knowledge_chunks 的 Create 收的是 *KnowledgeChunk：直接对解引用的副本
// 打戳会改到一份马上被丢弃的拷贝，表现是"代码看着对、库里全是 0"。
func StampOneForWrite(ctx context.Context, db *gorm.DB, chunk *model.KnowledgeChunk) error {
	if chunk == nil {
		return nil
	}
	slice := []model.KnowledgeChunk{*chunk}
	if err := StampForWrite(ctx, db, slice); err != nil {
		return err
	}
	*chunk = slice[0]
	return nil
}

// ErrGovernedDirectWrite 已进发布制的库上，直接改写/删除语料的通路被拦在此处。
//
// 单独一个 error 而不是 fmt.Errorf：控制层要把它翻成 409（"这条路走不通，请走变更流程"）
// 而不是 500，而错误串比对是最脆的一种类型判断。
var ErrGovernedDirectWrite = errors.New("该知识库已启用发布制：请通过知识变更流程提交（新增/修订/下线），由审批与发布使其生效")

// DirectWriteBlocked 判断一次"就地改写 / 物理删除 chunk"是否该被拒。
//
// 只有**同时**满足两条才拒：旗子 on，且该库 governed=true。这与读路径的两道锁同构 ——
// 未开闸时拦一个运营正在用的编辑入口，等于让"读代码顺便改了写语义"这件事发生在人身上。
//
// 拒的三个理由，按代价从大到小：
//  1. 就地 UPDATE 覆盖 content 会改掉**已经在服**的那段文字，而闸门只看版本号，
//     看不见内容被换过 ⇒ 未审批的正文当场进线上检索，那是 AC① 的直接反例；
//     （版本化救不了它：老正文的字节已经被覆盖，回滚无可回。）
//  2. 物理 DELETE 抹掉在服行 ⇒ 该库的回滚窗口失去依据（as-of 快照要求行还在表里）；
//  3. 这两类动作都不产生 kb_change_requests 行，也就是说它们不会留下 AC③ 要的留痕。
//
// 与之相对，**插入**通路不拒：新行带着待发布版本号落库，在未发布前本来就不可见，
// 这正是"导入也走发布制"要的行为，拦它反而会把导入弄成静默失败。
//
// 读失败一律返回错误上抛（不"放行算了"）：拿不准该库是否受管时，写侧的保守方向是停下 ——
// 一次误放行的代价是一段未审批正文进了线上，而一次误拦的代价是运营重试一遍。
func DirectWriteBlocked(ctx context.Context, db *gorm.DB, productID string) (bool, error) {
	if db == nil || productID == "" || !GateOn() {
		return false, nil
	}
	var governed bool
	if err := db.WithContext(ctx).Table("kb_releases").
		Select("governed").
		Where("product_id = ?", productID).
		Scan(&governed).Error; err != nil {
		return false, fmt.Errorf("kb_releases 治理状态读取失败（product=%s）: %w", productID, err)
	}
	return governed, nil
}
