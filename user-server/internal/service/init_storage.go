package service

import (
	"context"
	"os"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/storage"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// InitDefaultStorageIfEmpty 启动时检查 obs_config 表是否为空，为空则 seed 一条默认 local 配置。
//
// 私有化部署零云依赖的核心保证：无论用户有没有手动配置，系统都能立即可用。
// 幂等：只在 count == 0 时 seed；后续启动不会重复插入。
func InitDefaultStorageIfEmpty(gdb *gorm.DB) {
	if gdb == nil {
		logger.Warn("[OBS] InitDefaultStorageIfEmpty: gdb is nil, skip")
		return
	}

	var count int64
	if err := gdb.Model(&model.ObsConfig{}).Count(&count).Error; err != nil {
		logger.Warnf("[OBS] InitDefaultStorageIfEmpty: count failed: %v", err)
		return
	}
	if count > 0 {
		logger.Infof("[OBS] obs_config already has %d records, skip seed", count)
		return
	}

	baseDir, publicURL, _ := storage.LocalSource()

	cfg := &model.ObsConfig{
		ID:        uuid.New().String(),
		Name:      "默认本地存储",
		Provider:  model.ObsProviderLocal,
		Endpoint:  baseDir,
		Domain:    publicURL,
		IsDefault: true,
		Status:    model.ObsStatusActive,
		MaxSize:   100 * 1024 * 1024,
		MaxCount:  10000,
		AccessKey: "",
		SecretKey: "",
		Bucket:    "",
	}

	if err := gdb.WithContext(context.Background()).Create(cfg).Error; err != nil {
		logger.Errorf("[OBS] InitDefaultStorageIfEmpty: seed failed: %v", err)
		return
	}

	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		logger.Warnf("[OBS] InitDefaultStorageIfEmpty: mkdir %s failed: %v", baseDir, err)
	}

	logger.Infof("[OBS] ✅ seeded default local storage: baseDir=%s, publicURL=%s", baseDir, publicURL)
}
