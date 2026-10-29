package cards

// 契约：卡片「是否激活」的三态必须分得清 —— 缺省 / 显式 true / 显式 false。
//
// 坑：模型字段是非指针 bool 且带 gorm `default:true`。GORM 认为「零值 = 没赋值」，
// 于是把 false 从 INSERT 里剔除交给列默认值兜底，而列默认值是 true ——
// 「建一张禁用的卡片」会落库成激活，接口响应也回 true。
// 用户在弹窗里把「激活」开关关掉再保存，界面显示它还活着。
//
// 修法是两层一起动，缺一不可：
//  1. 模型层去掉 `default:true`（只负责原样落库）；
//  2. DTO 层改 *bool + service 层按「nil ⇒ 默认开」取值。
//
// 本文件锁三条腿：
//  1. 显式 false ⇒ 落库 false（缺陷本体）；
//  2. 缺省（不传）⇒ 落库 true（不能因为修了显式 false 就把"不传 = 默认开"改掉）；
//  3. 更新时缺省 ⇒ 保留原值（PATCH 语义，不许被缺省值顺手改掉）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// boolPtr 随卡片域拆包迁入本包（原在根包 service 的测试 helper，未跟迁）。
func boolPtr(v bool) *bool { return &v }

// readDouyinCardIsActive 用独立零值 struct 读回（复用已填充 struct 会把旧字段并进 WHERE）。
func readDouyinCardIsActive(t *testing.T, db *gorm.DB, id uint) bool {
	t.Helper()
	var got model.DouyinCard
	if err := db.WithContext(context.Background()).Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatalf("读回卡片 %d 失败：%v", id, err)
	}
	return got.IsActive
}

// TestDouyinCardCreateExplicitInactiveStaysInactive 显式 is_active:false ⇒ 落库 false。
func TestDouyinCardCreateExplicitInactiveStaysInactive(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	svc := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:       "card-inactive",
		Description: "显式禁用",
		RedirectURL: "https://www.douyin.com",
		Tags:        "test",
		IsActive:    boolPtr(false),
	}
	card, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create 失败：%v", err)
	}
	if card.IsActive {
		t.Errorf("响应体 is_active 应为 false，实际 true")
	}
	if got := readDouyinCardIsActive(t, database, card.ID); got {
		t.Errorf("落库 is_active 应为 false，实际 true —— 显式禁用被 GORM 的 default:true 标签吞掉了")
	}
}

// TestDouyinCardCreateWithoutIsActiveDefaultsToActive 缺省 ⇒ 落库 true。
func TestDouyinCardCreateWithoutIsActiveDefaultsToActive(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	svc := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:       "card-default-active",
		RedirectURL: "https://www.douyin.com",
		Tags:        "test",
	}
	card, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create 失败：%v", err)
	}
	if !card.IsActive {
		t.Errorf("缺省 is_active 应为 true，实际 false")
	}
	if got := readDouyinCardIsActive(t, database, card.ID); !got {
		t.Errorf("落库 is_active 应为 true，实际 false —— 缺省语义归 service 层，模型层不得越权改")
	}
}

// TestDouyinCardUpdateWithoutIsActiveKeepsExisting 更新时缺省 ⇒ 保留原值。
func TestDouyinCardUpdateWithoutIsActiveKeepsExisting(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	svc := NewDouyinCardService(database)

	created, err := svc.Create(context.Background(), &dto.DouyinCardCreateRequest{
		Title:       "card-update-keep",
		RedirectURL: "https://www.douyin.com",
		Tags:        "test",
		IsActive:    boolPtr(false),
	})
	if err != nil {
		t.Fatalf("Create 失败：%v", err)
	}
	if created.IsActive {
		t.Fatalf("前置条件不成立：新建的卡片本应是禁用的")
	}

	// 更新时**不传** is_active：不得把已有的 false 顺手改成 true。
	updated, err := svc.Update(context.Background(), &dto.DouyinCardUpdateRequest{
		ID:          created.ID,
		Title:       "card-update-keep-v2",
		RedirectURL: "https://www.douyin.com",
	})
	if err != nil {
		t.Fatalf("Update 失败：%v", err)
	}
	if updated.IsActive {
		t.Errorf("更新时缺省 is_active 应保留原值 false，实际 true")
	}
	if got := readDouyinCardIsActive(t, database, created.ID); got {
		t.Errorf("落库 is_active 应保留 false，实际 true —— 缺省字段把用户的禁用状态抹掉了")
	}
}
