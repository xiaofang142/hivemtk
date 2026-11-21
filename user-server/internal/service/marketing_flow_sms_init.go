package service

import (
	"context"
	"errors"
	"fmt"
	smssvc "hivemtk-user/internal/service/sms"
	"sync"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	smsSvcOnce     sync.Once
	smsSvcInstance smssvc.SmsService
	smsSvcInitErr  error
)

// 注册职责已随 FA-20 移到 marketing_flow_reach_gate.go：营销流程短信发送器
// 现在注册的是「闸门版」（DNC → W-1 审批 → E1 合规 → lazySendSms），
// 本文件只保留真实短信通道（lazySendSms）与其依赖装配。

func initSmsService() (smssvc.SmsService, error) {
	smsSvcOnce.Do(func() {
		smsSvcInstance = smssvc.NewSmsService(repository.NewSmsRepository())
		if smsSvcInstance == nil {
			smsSvcInitErr = errors.New("初始化短信服务失败：仓储创建返回 nil")
		}
	})
	return smsSvcInstance, smsSvcInitErr
}

func lazySendSms(parent context.Context, phone, content string) error {
	if phone == "" {
		return errors.New("phone 不能为空")
	}
	if content == "" {
		return errors.New("content 不能为空")
	}

	svc, err := initSmsService()
	if err != nil {
		return fmt.Errorf("初始化短信服务失败：%w", err)
	}

	// 发短信要读本租户的渠道配置并落 sms_records：解绑流程取消，但租户不能丢。
	ctx := tenant.BackgroundWithTenant(parent)

	req := &dto.SmsSendRequest{
		Phone:   phone,
		Content: content,
	}
	if err := svc.SendSms(ctx, req); err != nil {
		return fmt.Errorf("发送短信失败：%w", err)
	}
	return nil
}
