package app

import (
	"sync"

	"gorm.io/gorm"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

var (
	inferenceDeliveryOnce   sync.Once
	globalInferenceDelivery service.InferenceDeliveryService
)

// InitInferenceDeliveryService 在路由及推理编排器之前装配持久账本。
// 外部 sender 与策略尚未配置时仅持久记录 blocked，不能把旧同步端口当确认回执。
func InitInferenceDeliveryService(db *gorm.DB) {
	if db == nil {
		return
	}
	inferenceDeliveryOnce.Do(func() {
		globalInferenceDelivery = service.NewInferenceDeliveryService(repository.NewInferenceDeliveryRepository(db), nil, nil)
	})
}

func GetInferenceDeliveryService() service.InferenceDeliveryService { return globalInferenceDelivery }
