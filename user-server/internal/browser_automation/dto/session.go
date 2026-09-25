package dto

type ListSessionReq struct {
	Status string `form:"status" binding:"omitempty,oneof=created active completed failed stopped"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=200"`
}

type StopSessionReq struct {
	Reason string `json:"reason"`
}

// ConfirmSessionReq D7 放行请求（批20 A5）：payload_hash 是**被批准的那份载荷**的指纹，
// 由 GET /sessions/:id/confirm-gate 取到后原样带回。它不是可选字段——一条不带载荷的放行
// 与一张空白支票同构：批准的内容可以换，也可以被下一个挂起点消费掉。
type ConfirmSessionReq struct {
	PayloadHash string `json:"payload_hash"`
}
