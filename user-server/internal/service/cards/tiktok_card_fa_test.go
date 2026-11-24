package cards

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// faCardRepo 假卡片仓储：嵌入接口只覆写用到的方法。
type faCardRepo struct {
	repository.TikTokCardRepository
	card        *model.TikTokCard
	updateErr   error
	updateCalls int
}

func (f *faCardRepo) GetByID(_ context.Context, _ uint) (*model.TikTokCard, error) {
	return f.card, nil
}
func (f *faCardRepo) Update(_ context.Context, c *model.TikTokCard) (*model.TikTokCard, error) {
	f.updateCalls++
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.card.ShortLinkID = c.ShortLinkID
	return f.card, nil
}

// faShortLinkRepo 假短链仓储：内存 map + 可控创建失败 + 记录删除。
type faShortLinkRepo struct {
	repository.ShortLinkRepository
	links       map[uint]*model.ShortLink
	nextID      uint
	createErr   error
	createCalls int
	deleted     []uint
}

func newFAShortLinkRepo() *faShortLinkRepo {
	return &faShortLinkRepo{links: map[uint]*model.ShortLink{}, nextID: 1}
}
func (f *faShortLinkRepo) GetByID(_ context.Context, id uint) (*model.ShortLink, error) {
	if l, ok := f.links[id]; ok {
		return l, nil
	}
	return nil, errors.New("not found")
}
func (f *faShortLinkRepo) Create(_ context.Context, sl *model.ShortLink) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	sl.ID = f.nextID
	f.nextID++
	cp := *sl
	f.links[sl.ID] = &cp
	return nil
}
func (f *faShortLinkRepo) Delete(_ context.Context, id uint) error {
	f.deleted = append(f.deleted, id)
	delete(f.links, id)
	return nil
}

func faNewSvc(cardRepo *faCardRepo, slRepo *faShortLinkRepo) *tiktokCardService {
	return &tiktokCardService{repo: cardRepo, shortLinkRepo: slRepo}
}

func faCard(oldLink uint) *model.TikTokCard {
	return &model.TikTokCard{
		ID: 7, ShortLinkID: oldLink, RedirectURL: "https://example.com/go",
		Title: "t", Description: "d", DomainPoolID: 1,
	}
}

// 旧链接先存在于仓储中。
func faWithOld(old uint) *faShortLinkRepo {
	r := newFAShortLinkRepo()
	if old != 0 {
		r.links[old] = &model.ShortLink{ID: old, ShortCode: "old", OriginalURL: "https://example.com/go"}
		r.nextID = old + 1
	}
	return r
}

// Create 失败：旧链接继续可用，卡片关联不变。
func TestFACard_CreateFailureKeepsOld(t *testing.T) {
	cardRepo := &faCardRepo{card: faCard(11)}
	slRepo := faWithOld(11)
	slRepo.createErr = errors.New("create boom")
	svc := faNewSvc(cardRepo, slRepo)
	if _, err := svc.GenerateShortLink(context.Background(), 7); err == nil {
		t.Fatal("创建失败应返回错误")
	}
	if cardRepo.card.ShortLinkID != 11 {
		t.Fatalf("创建失败不应改卡片关联, got %d", cardRepo.card.ShortLinkID)
	}
	if _, ok := slRepo.links[11]; !ok {
		t.Fatal("创建失败不应删除旧短链")
	}
}

// Update(关联)失败：回收新候选，旧链接不动，卡片关联复位。
func TestFACard_AssocFailureReclaimsNewKeepsOld(t *testing.T) {
	cardRepo := &faCardRepo{card: faCard(11), updateErr: errors.New("update boom")}
	slRepo := faWithOld(11)
	svc := faNewSvc(cardRepo, slRepo)
	if _, err := svc.GenerateShortLink(context.Background(), 7); err == nil {
		t.Fatal("关联失败应返回错误")
	}
	if cardRepo.card.ShortLinkID != 11 {
		t.Fatalf("关联失败应复位为旧关联, got %d", cardRepo.card.ShortLinkID)
	}
	if _, ok := slRepo.links[11]; !ok {
		t.Fatal("关联失败不应删除旧短链")
	}
	// 新候选(12)应被回收。
	if _, ok := slRepo.links[12]; ok {
		t.Fatal("关联失败应回收新候选短链")
	}
}

// 双重建：成功时卡片只关联一个有效目标，旧链接被回收。
func TestFACard_RebuildSingleActiveLink(t *testing.T) {
	cardRepo := &faCardRepo{card: faCard(0)}
	slRepo := faWithOld(0)
	svc := faNewSvc(cardRepo, slRepo)
	if _, err := svc.GenerateShortLink(context.Background(), 7); err != nil {
		t.Fatalf("首次重建: %v", err)
	}
	first := cardRepo.card.ShortLinkID
	if first == 0 {
		t.Fatal("首次重建应有有效关联")
	}
	if _, err := svc.GenerateShortLink(context.Background(), 7); err != nil {
		t.Fatalf("二次重建: %v", err)
	}
	second := cardRepo.card.ShortLinkID
	if second == 0 || second == first {
		t.Fatalf("二次重建应切换到新短链, first=%d second=%d", first, second)
	}
	if _, ok := slRepo.links[second]; !ok {
		t.Fatal("成功时卡片关联的短链应存在")
	}
	if _, ok := slRepo.links[first]; ok {
		t.Fatal("成功时旧短链应被回收")
	}
}

// 重试：失败后再次成功，最终单一有效关联。
func TestFACard_RetryEventuallySucceeds(t *testing.T) {
	cardRepo := &faCardRepo{card: faCard(0)}
	slRepo := faWithOld(0)
	svc := faNewSvc(cardRepo, slRepo)
	slRepo.createErr = errors.New("transient")
	if _, err := svc.GenerateShortLink(context.Background(), 7); err == nil {
		t.Fatal("瞬时失败应返回错误")
	}
	slRepo.createErr = nil
	if _, err := svc.GenerateShortLink(context.Background(), 7); err != nil {
		t.Fatalf("重试应成功: %v", err)
	}
	final := cardRepo.card.ShortLinkID
	if final == 0 {
		t.Fatal("重试成功后应有有效关联")
	}
	if _, ok := slRepo.links[final]; !ok {
		t.Fatal("最终关联的短链应存在")
	}
}
