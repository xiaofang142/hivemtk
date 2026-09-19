// order_draft_store.go 草稿存储的两副面孔（T-P2-01 / R-1）
//
// 为什么是"两副"而不是"直接换成 DB"：service.OrderDraft 今天被 25 个存量用例当作
// **活对象**在用（`draft.ExpiresAt = now-1h` 之后直接调 ExpireOverdue、
// `d1.Confidence = 0.5` 之后直接调 ListPending 断言排序）。这依赖"返回的指针就是
// 存储里那份"。落 DB 之后对象必然是副本，这类写法一条都不成立。
// 所以本卡的做法是：业务逻辑只走 draftStore 一条路，
//   - memoryDraftStore = 今天的行为，逐字保留（含指针别名），默认；
//   - dbDraftStore     =  durable 底座，显式注入才启用（装配点在 T-P2-06）。
//
// 两者的可观察差异由 TestOrderDraftStore_Parity 逐条钉住（见 order_draft_store_test.go），
// 不靠"看起来一样"。
package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// draftStore 草稿存储接口（方法集合 = service 需要的能力，不是表结构）
type draftStore interface {
	// durable 是否跨进程重启仍保留数据。端点/日志用它把"内存态"这件事摊开，
	// 而不是让人从"接口返回正常"推断"应该存下来了吧"。
	durable() bool

	put(ctx context.Context, d *OrderDraft) error
	get(ctx context.Context, id string) (*OrderDraft, error)
	pendingByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error)
	listPending(ctx context.Context, ownerID string, now time.Time, limit int) ([]*OrderDraft, error)
	listByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error)
	listByOwner(ctx context.Context, ownerID string) ([]*OrderDraft, error)

	// mutateIfPending 在"仍是 pending"这个前提下就地改一次草稿。
	// 返回 (改后的对象, 是否生效, 错误)；生效=false 表示草稿不存在或已不是 pending，
	// 调用方需自行区分两者（错误消息口径不同）。
	mutateIfPending(ctx context.Context, id string, fn func(*OrderDraft)) (*OrderDraft, bool, error)

	// expireOverdue 把所有"已到期且仍 pending"的草稿翻成 expired，对每一条被翻转的
	// 草稿回调 ev（供上层记统计事件），返回翻转条数。
	expireOverdue(ctx context.Context, now time.Time, ev func(*OrderDraft)) (int, error)

	purgeTerminal(ctx context.Context, before time.Time) (int, error)
	statusCounts(ctx context.Context) (map[string]int64, error)
}

const (
	// draftSweepBatch 单轮过期扫描/清理一次取多少行。分批不是为了好看：
	// 一次全表 UPDATE 在草稿量上来之后会长时间持锁，而它旁边就是销售工作台的读请求。
	draftSweepBatch = 200
	// draftSweepMaxRounds 一次调用的最大轮数（上限 = batch×rounds 行）。
	// 没有这个上界，"清完为止"在某些输入下就是一个不确定的长任务。
	draftSweepMaxRounds = 100

	// defaultDraftRetention 终态草稿的默认保留期。取 90 天是对齐"一个季度复盘还查得到
	// 为什么没成单"这个业务问题，而不是随手挑的整数；实际入口在 PurgeTerminal 的参数上，
	// 装配时可覆盖（见 T-P2-06）。
	defaultDraftRetention = 90 * 24 * time.Hour
)

// 三副底座的名字（OrderDraftService.StoreKind 的取值，也是观察端点回显的 store 字段）。
// 做成常量是为了让"端点说的"和"代码选的"不会各写各的字面量；导出给 router 包比对，
// 因为端点那侧的分支判据必须与这里同一份。
const (
	DraftStoreKindMemory = "memory"
	DraftStoreKindDB     = "db"
	DraftStoreKindShadow = "shadow"
)

// ---------------------------------------------------------------- 内存实现 ----

type memoryDraftStore struct {
	mu         sync.RWMutex
	byID       map[string]*OrderDraft
	byCustomer map[string][]*OrderDraft
	byOwner    map[string][]*OrderDraft
}

func newMemoryDraftStore() *memoryDraftStore {
	return &memoryDraftStore{
		byID:       make(map[string]*OrderDraft),
		byCustomer: make(map[string][]*OrderDraft),
		byOwner:    make(map[string][]*OrderDraft),
	}
}

func (m *memoryDraftStore) durable() bool { return false }

// put 存的是**传进来的那个指针**（别名语义即存量用例依赖的行为）。
// 同 ID 重复 put 时替换索引里的旧引用，与 DB 侧 upsert 对应收敛为同一语义。
func (m *memoryDraftStore) put(_ context.Context, d *OrderDraft) error {
	if d == nil || d.ID == "" {
		return errors.New("草稿 ID 为空，无法保存")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.byID[d.ID]; ok {
		m.dropLocked(old)
	}
	m.byID[d.ID] = d
	m.byCustomer[d.CustomerID] = append(m.byCustomer[d.CustomerID], d)
	m.byOwner[d.OwnerID] = append(m.byOwner[d.OwnerID], d)
	return nil
}

// dropLocked 从两张倒排索引里摘掉一条（调用方持锁）。
//
// 内存版原来只有插入、从不摘除，于是"取消/过期"只是把状态字段改了个值，
// 索引里的引用会一直长。这里补摘除是为了与 DB 侧的 purge 保持同一条契约：
// purgeTerminal 之后，同一个 store 不该还能把已清理的草稿列出来。
func (m *memoryDraftStore) dropLocked(d *OrderDraft) {
	if d == nil {
		return
	}
	m.byCustomer[d.CustomerID] = removeDraft(m.byCustomer[d.CustomerID], d.ID)
	if len(m.byCustomer[d.CustomerID]) == 0 {
		delete(m.byCustomer, d.CustomerID)
	}
	m.byOwner[d.OwnerID] = removeDraft(m.byOwner[d.OwnerID], d.ID)
	if len(m.byOwner[d.OwnerID]) == 0 {
		delete(m.byOwner, d.OwnerID)
	}
}

func removeDraft(list []*OrderDraft, id string) []*OrderDraft {
	out := list[:0]
	for _, d := range list {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
}

func (m *memoryDraftStore) get(_ context.Context, id string) (*OrderDraft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id], nil
}

func (m *memoryDraftStore) pendingByCustomer(_ context.Context, customerID string) ([]*OrderDraft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OrderDraft, 0)
	for _, d := range m.byCustomer[customerID] {
		if d.Status == DraftStatusPending {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return draftLessByCreatedAt(out[i], out[j]) })
	return out, nil
}

func draftLessByCreatedAt(a, b *OrderDraft) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

func (m *memoryDraftStore) listPending(_ context.Context, ownerID string, now time.Time, limit int) ([]*OrderDraft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OrderDraft, 0)
	for _, d := range m.byID {
		if d.Status != DraftStatusPending {
			continue
		}
		if ownerID != "" && d.OwnerID != ownerID {
			continue
		}
		if !now.Before(d.ExpiresAt) {
			continue
		}
		out = append(out, d)
	}
	// 排序必须与 DB 侧 ORDER BY 逐字同构（含 id 兜底）：内存版遍历 map 本身是随机序，
	// 不加末位兜底时同置信度同金额的草稿两条路会给不同顺序，"换底座不改行为"就是空话。
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].TotalAmount != out[j].TotalAmount {
			return out[i].TotalAmount > out[j].TotalAmount
		}
		if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryDraftStore) listByCustomer(_ context.Context, customerID string) ([]*OrderDraft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OrderDraft, len(m.byCustomer[customerID]))
	copy(out, m.byCustomer[customerID])
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (m *memoryDraftStore) listByOwner(_ context.Context, ownerID string) ([]*OrderDraft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OrderDraft, len(m.byOwner[ownerID]))
	copy(out, m.byOwner[ownerID])
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i].Status == DraftStatusPending, out[j].Status == DraftStatusPending
		if pi != pj {
			return pi
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (m *memoryDraftStore) mutateIfPending(_ context.Context, id string, fn func(*OrderDraft)) (*OrderDraft, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byID[id]
	if !ok || d == nil {
		return nil, false, nil
	}
	if d.Status != DraftStatusPending {
		return d, false, nil
	}
	fn(d)
	return d, true, nil
}

func (m *memoryDraftStore) expireOverdue(_ context.Context, now time.Time, ev func(*OrderDraft)) (int, error) {
	m.mu.Lock()
	victims := make([]*OrderDraft, 0)
	for _, d := range m.byID {
		if d.Status == DraftStatusPending && !now.Before(d.ExpiresAt) {
			d.Status = DraftStatusExpired
			d.UpdatedAt = now
			victims = append(victims, d)
		}
	}
	m.mu.Unlock()
	if ev != nil {
		for _, d := range victims {
			ev(d)
		}
	}
	return len(victims), nil
}

func (m *memoryDraftStore) purgeTerminal(_ context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var doomed []*OrderDraft
	for _, d := range m.byID {
		if isTerminalDraftStatus(d.Status) && d.UpdatedAt.Before(before) {
			doomed = append(doomed, d)
		}
	}
	for _, d := range doomed {
		delete(m.byID, d.ID)
		m.dropLocked(d)
	}
	return len(doomed), nil
}

func (m *memoryDraftStore) statusCounts(_ context.Context) (map[string]int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int64, 4)
	for _, d := range m.byID {
		out[string(d.Status)]++
	}
	return out, nil
}

func isTerminalDraftStatus(s DraftStatus) bool {
	for _, t := range model.OrderDraftTerminalStatuses {
		if string(s) == t {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------- DB 版 ----

type dbDraftStore struct {
	repo repository.OrderDraftRepository
}

func newDBDraftStore(repo repository.OrderDraftRepository) *dbDraftStore {
	return &dbDraftStore{repo: repo}
}

// newDraftStoreForDB 按句柄选底座：拿到可用 DB 才走持久化，否则回内存并告警。
//
// 回退必须出声：静默回内存 = "以为重启后草稿还在、其实没了"，
// 而这正是本卡开工前整个功能的实际状态（一句话都不说过）。
func newDraftStoreForDB(db *gorm.DB) draftStore {
	if db == nil {
		logger.Warnf("[order-draft] ⚠️ 未拿到 DB 句柄 ⇒ 草稿退回内存态（重启即丢、多副本各看各的）")
		return newMemoryDraftStore()
	}
	return newDBDraftStore(repository.NewOrderDraftRepositoryWithDB(db))
}

func (s *dbDraftStore) durable() bool { return s.repo.Available() }

func (s *dbDraftStore) put(ctx context.Context, d *OrderDraft) error {
	if d == nil || d.ID == "" {
		return errors.New("草稿 ID 为空，无法保存")
	}
	return s.repo.Upsert(ctx, draftToModel(d))
}

func (s *dbDraftStore) get(ctx context.Context, id string) (*OrderDraft, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil || m == nil {
		return nil, err
	}
	return draftFromModel(m), nil
}

func (s *dbDraftStore) pendingByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	rows, err := s.repo.ListPendingByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	return draftsFromModels(rows), nil
}

func (s *dbDraftStore) listPending(ctx context.Context, ownerID string, now time.Time, limit int) ([]*OrderDraft, error) {
	rows, err := s.repo.ListPending(ctx, ownerID, now, limit)
	if err != nil {
		return nil, err
	}
	return draftsFromModels(rows), nil
}

func (s *dbDraftStore) listByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	rows, err := s.repo.ListByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	return draftsFromModels(rows), nil
}

func (s *dbDraftStore) listByOwner(ctx context.Context, ownerID string) ([]*OrderDraft, error) {
	rows, err := s.repo.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return draftsFromModels(rows), nil
}

func (s *dbDraftStore) mutateIfPending(ctx context.Context, id string, fn func(*OrderDraft)) (*OrderDraft, bool, error) {
	var out *OrderDraft
	applied, err := s.repo.MutatePending(ctx, id, func(m *model.OrderDraft) {
		d := draftFromModel(m)
		fn(d)
		*m = *draftToModel(d)
		out = d
	})
	if err != nil {
		return nil, false, err
	}
	if !applied {
		// 未生效时 out 可能已被回调改过（不存在/非 pending 两种情形里 fn 都没跑，
		// 但落败回滚路径要交回 nil，免得调用方把一份没写进去的副本当成已保存的状态）。
		return nil, false, nil
	}
	return out, true, nil
}

func (s *dbDraftStore) expireOverdue(ctx context.Context, now time.Time, ev func(*OrderDraft)) (int, error) {
	total := 0
	for round := 0; round < draftSweepMaxRounds; round++ {
		rows, err := s.repo.ExpirePendingBatch(ctx, now, draftSweepBatch)
		if err != nil {
			return total, err
		}
		for _, m := range rows {
			if ev != nil {
				ev(draftFromModel(m))
			}
		}
		total += len(rows)
		if len(rows) < draftSweepBatch {
			return total, nil
		}
	}
	logger.Warnf("[order-draft] 过期扫描达到单轮上限 %d×%d，剩余到期草稿留待下次调用",
		draftSweepBatch, draftSweepMaxRounds)
	return total, nil
}

func (s *dbDraftStore) purgeTerminal(ctx context.Context, before time.Time) (int, error) {
	total := 0
	for round := 0; round < draftSweepMaxRounds; round++ {
		n, err := s.repo.PurgeTerminal(ctx, before, draftSweepBatch)
		if err != nil {
			return total, err
		}
		total += int(n)
		if n < draftSweepBatch {
			return total, nil
		}
	}
	logger.Warnf("[order-draft] 终态清理达到单轮上限 %d×%d，剩余留待下次调用",
		draftSweepBatch, draftSweepMaxRounds)
	return total, nil
}

func (s *dbDraftStore) statusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repo.CountByStatus(ctx)
}

// ------------------------------------------------------------ 影子底座 -------

// shadowDraftStore 读走内存、写同时镜像一份到 DB（FF_LTC_ORDER_DRAFT_DB=shadow 用）。
//
// 为什么要这么一副"半新半旧"的底座：灰度期真正想知道的问题是"换成 DB 底座之后，
// 读到的东西和内存里这套对不对得上"，而不是"新版能不能跑"。所以
//   - 读一律走内存 ⇒ 对调用方零行为变化（包括多副本下各看各的这份内存，与今天一致）；
//   - 写两边都做 ⇒ order_drafts 里有真实行可查，端点上两个口径的计数能并排看；
//   - 镜像失败**不影响**业务：只累计计数 + 记最近一次错误，由观察端点摊开。
//
// durable() 在这里刻意返回 false，即使库里此刻真有行：影子期权威的那一份是内存，
// 进程重启后读到的还是空。把 true 报出去就是让运维以为"现在重启不丢草稿了"，
// 而这句话要到 on 档才成立。
type shadowDraftStore struct {
	primary *memoryDraftStore
	mirror  repository.OrderDraftRepository

	mu        sync.Mutex
	failures  int64
	lastError string
}

func newShadowDraftStore(db *gorm.DB) *shadowDraftStore {
	return &shadowDraftStore{
		primary: newMemoryDraftStore(),
		mirror:  repository.NewOrderDraftRepositoryWithDB(db),
	}
}

func (s *shadowDraftStore) durable() bool { return false }

// recordMirrorFailure 累计一次镜像写失败。业务侧不感知（返回值刻意不给），
// 但必须可查：静默吞掉的镜像失败会让整段灰度期的对照数据少于一半而无人知晓。
func (s *shadowDraftStore) recordMirrorFailure(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.failures++
	s.lastError = err.Error()
	s.mu.Unlock()
}

// MirrorStats 最近一次读到的镜像失败数与最后一条错误。
func (s *shadowDraftStore) MirrorStats() (failures int64, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures, s.lastError
}

// mirrorAvailable 报告镜像句柄是否可用（端点用它区分"没写进去"和"根本没地方写"）。
func (s *shadowDraftStore) mirrorAvailable() bool { return s.mirror.Available() }

// mirrorCounts 读库侧各状态行数，供与内存侧并排对照。错误照直回给调用方，
// 这里不降级成空 map —— 空 map 会被读成"库里一条都没有"。
func (s *shadowDraftStore) mirrorCounts(ctx context.Context) (map[string]int64, error) {
	return s.mirror.CountByStatus(ctx)
}

func (s *shadowDraftStore) put(ctx context.Context, d *OrderDraft) error {
	if err := s.primary.put(ctx, d); err != nil {
		return err
	}
	s.recordMirrorFailure(s.mirror.Upsert(ctx, draftToModel(d)))
	return nil
}

func (s *shadowDraftStore) get(ctx context.Context, id string) (*OrderDraft, error) {
	return s.primary.get(ctx, id)
}

func (s *shadowDraftStore) pendingByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	return s.primary.pendingByCustomer(ctx, customerID)
}

func (s *shadowDraftStore) listPending(ctx context.Context, ownerID string, now time.Time, limit int) ([]*OrderDraft, error) {
	return s.primary.listPending(ctx, ownerID, now, limit)
}

func (s *shadowDraftStore) listByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	return s.primary.listByCustomer(ctx, customerID)
}

func (s *shadowDraftStore) listByOwner(ctx context.Context, ownerID string) ([]*OrderDraft, error) {
	return s.primary.listByOwner(ctx, ownerID)
}

func (s *shadowDraftStore) mutateIfPending(ctx context.Context, id string, fn func(*OrderDraft)) (*OrderDraft, bool, error) {
	d, applied, err := s.primary.mutateIfPending(ctx, id, fn)
	if err != nil || !applied {
		return d, applied, err
	}
	// 镜像走 Upsert 而不是再来一次 MutatePending：权威判定已经在内存做过，
	// 库里再锁一次只会把"本进程不认这条是 pending"的多副本分歧变成第二次错误来源。
	s.recordMirrorFailure(s.mirror.Upsert(ctx, draftToModel(d)))
	return d, applied, nil
}

func (s *shadowDraftStore) expireOverdue(ctx context.Context, now time.Time, ev func(*OrderDraft)) (int, error) {
	n, err := s.primary.expireOverdue(ctx, now, ev)
	if err != nil {
		return n, err
	}
	s.sweepMirrorExpiry(ctx, now)
	return n, nil
}

// sweepMirrorExpiry 把库侧的到期行也翻成 expired，与内存侧同一批节拍。
// 不回调 ev：expired 统计事件由内存侧那份发一次，两边各发一遍等于把"过期草稿数"翻倍。
func (s *shadowDraftStore) sweepMirrorExpiry(ctx context.Context, now time.Time) {
	for round := 0; round < draftSweepMaxRounds; round++ {
		rows, err := s.mirror.ExpirePendingBatch(ctx, now, draftSweepBatch)
		if err != nil {
			s.recordMirrorFailure(err)
			return
		}
		if len(rows) < draftSweepBatch {
			return
		}
	}
	logger.Warnf("[order-draft] 影子镜像的过期扫描达到单轮上限 %d×%d，剩余留待下次",
		draftSweepBatch, draftSweepMaxRounds)
}

func (s *shadowDraftStore) purgeTerminal(ctx context.Context, before time.Time) (int, error) {
	n, err := s.primary.purgeTerminal(ctx, before)
	if err != nil {
		return n, err
	}
	for round := 0; round < draftSweepMaxRounds; round++ {
		deleted, e := s.mirror.PurgeTerminal(ctx, before, draftSweepBatch)
		if e != nil {
			s.recordMirrorFailure(e)
			return n, nil
		}
		if deleted < draftSweepBatch {
			return n, nil
		}
	}
	return n, nil
}

func (s *shadowDraftStore) statusCounts(ctx context.Context) (map[string]int64, error) {
	return s.primary.statusCounts(ctx)
}

// newShadowDraftStoreForDB 拿到句柄才起影子，否则退回纯内存并出声。
//
// 这里不退成"影子但没有镜像"：那会交付一副看起来在对照、实际什么都没记的底座，
// 端点上 mirror_available=false 与"今天就是没写进库"两种情况再也分不开。
func newShadowDraftStoreForDB(db *gorm.DB) draftStore {
	if db == nil {
		logger.Warnf("[order-draft] ⚠️ shadow 档未拿到 DB 句柄 ⇒ 退回纯内存（无镜像可对照）")
		return newMemoryDraftStore()
	}
	return newShadowDraftStore(db)
}

// ------------------------------------------------------------ 双向转换 -------

func draftToModel(d *OrderDraft) *model.OrderDraft {
	meta := model.JSONMap{}
	for k, v := range d.Metadata {
		meta[k] = v
	}
	return &model.OrderDraft{
		ID:           d.ID,
		CustomerID:   d.CustomerID,
		OneID:        d.OneID,
		OwnerID:      d.OwnerID,
		ProductName:  d.ProductName,
		ProductID:    d.ProductID,
		Category:     d.Category,
		Quantity:     d.Quantity,
		UnitPrice:    d.UnitPrice,
		TotalAmount:  d.TotalAmount,
		Confidence:   d.Confidence,
		Source:       d.Source,
		SourceText:   d.SourceText,
		IntentID:     d.IntentID,
		Status:       string(d.Status),
		OrderID:      d.OrderID,
		Note:         d.Note,
		CancelReason: d.CancelReason,
		Metadata:     meta,
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
		ExpiresAt:    d.ExpiresAt,
		ConfirmedAt:  d.ConfirmedAt,
		CancelledAt:  d.CancelledAt,
	}
}

func draftFromModel(m *model.OrderDraft) *OrderDraft {
	meta := make(map[string]any, len(m.Metadata))
	for k, v := range m.Metadata {
		meta[k] = v
	}
	return &OrderDraft{
		ID:           m.ID,
		CustomerID:   m.CustomerID,
		OneID:        m.OneID,
		OwnerID:      m.OwnerID,
		ProductName:  m.ProductName,
		ProductID:    m.ProductID,
		Category:     m.Category,
		Quantity:     m.Quantity,
		UnitPrice:    m.UnitPrice,
		TotalAmount:  m.TotalAmount,
		Confidence:   m.Confidence,
		Source:       m.Source,
		SourceText:   m.SourceText,
		IntentID:     m.IntentID,
		Status:       DraftStatus(m.Status),
		OrderID:      m.OrderID,
		Note:         m.Note,
		CancelReason: m.CancelReason,
		Metadata:     meta,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		ExpiresAt:    m.ExpiresAt,
		ConfirmedAt:  m.ConfirmedAt,
		CancelledAt:  m.CancelledAt,
	}
}

func draftsFromModels(rows []*model.OrderDraft) []*OrderDraft {
	out := make([]*OrderDraft, 0, len(rows))
	for _, m := range rows {
		out = append(out, draftFromModel(m))
	}
	return out
}
