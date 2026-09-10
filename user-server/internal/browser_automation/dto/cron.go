package dto

type CreateCronReq struct {
	TaskID   uint   `json:"task_id" binding:"required"`
	CronExpr string `json:"cron_expr" binding:"required,max=128"`
	Enabled  *bool  `json:"enabled"`
}

type UpdateCronReq struct {
	CronExpr string `json:"cron_expr" binding:"omitempty,max=128"`
}
