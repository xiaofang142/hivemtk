package dto

type ListSessionReq struct {
	Status string `form:"status" binding:"omitempty,oneof=created active completed failed stopped"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=200"`
}

type StopSessionReq struct {
	Reason string `json:"reason"`
}
