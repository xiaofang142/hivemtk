package app

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

func InitBridgeLeadRuntime(database *gorm.DB, ingress *service.InboxIngressService) (*service.BridgeLeadWorker, error) {
	if database == nil || ingress == nil || ingress.BridgeLeadProcessor() == nil {
		return nil, errors.New("bridge lead runtime requires database and a ready processor")
	}
	worker := service.NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), ingress.BridgeLeadProcessor())
	if err := worker.Start(context.Background()); err != nil {
		return nil, err
	}
	return worker, nil
}

func StopBridgeLeadRuntime(worker *service.BridgeLeadWorker) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return worker.Stop(ctx)
}
