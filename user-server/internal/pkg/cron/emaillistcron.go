package cron

import (
	"context"
	"fmt"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/mail"
	"hivemtk-user/internal/pkg/utils/logger"
	"time"

	"github.com/google/uuid"
)

func EmailListCron() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[email_list_cron] panic recovered: %v", r)
		}
	}()
	emailListService := email.NewEmailListService()
	emailListList, err := emailListService.GetUnsentEmailList(context.Background(), 10)
	if err != nil {
		logger.Info(fmt.Sprintf("获取未发送的email列表失败 %s", err.Error()))
		return
	}

	emailSmtpService := email.NewEmailSmtpService()

	for _, emailList := range emailListList {
		emailSmtp, err := emailSmtpService.GetRandEmailSmtp(context.Background())
		if err != nil {
			logger.Info(fmt.Sprintf("获取随机smtp失败 %s", err.Error()))
			continue
		}

		cfg := mail.Config{
			From:     emailSmtp.Username,
			Password: emailSmtp.Password,
		}

		err = mail.SendMail(cfg, []string{emailList.To}, emailList.Subject, emailList.Content, true)

		is_success := 1
		if err != nil {
			is_success = 0
			logger.Info(fmt.Sprintf("发送失败:%s", err.Error()))
		}

		emailList.IsSend = 1
		emailList.SendTime = time.Now()
		emailList.From = emailSmtp.Username
		emailList.IsSuccess = is_success
		emailListService.UpdateEmailList(context.Background(), *emailList)

		jobs_id := emailList.JobsID
		if jobs_id == uuid.Nil {
			logger.Error(fmt.Errorf("email list %s not found", emailList.ID), "查找邮件列表失败")
			continue
		}
		emailJobService := email.NewEmailJobsService()
		emailJobService.IncreaseSendTotal(context.Background(), jobs_id)
		if is_success == 1 {
			emailJobService.IncreaseSuccessTotal(context.Background(), jobs_id)
		}
		if is_success == 0 {
			emailJobService.IncreaseFailTotal(context.Background(), jobs_id)
		}
	}
}
