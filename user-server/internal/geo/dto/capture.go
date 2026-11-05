package dto

// CaptureAskRequest 公开落地页 FAQ 提问。
type CaptureAskRequest struct {
	Question string `json:"question" binding:"required"`
}

// CaptureLeadRequest 公开落地页留资。
type CaptureLeadRequest struct {
	Contact     string `json:"contact" binding:"required"`
	ContactType string `json:"contact_type"`
	Message     string `json:"message"`
}
