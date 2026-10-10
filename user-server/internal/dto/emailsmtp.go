package dto

type CreateEmailSmtpRequest struct {
	Name     string `json:"name" binding:"required"`
	Server   string `json:"server" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Limit    int64  `json:"limit" binding:"required"`
}

type GetEmailSmtpListResponse struct {
	Total int64                `json:"total"`
	List  []*EmailSmtpResponse `json:"list"`
}

type EmailSmtpResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Limit    int64  `json:"limit"`
}

type UpdateEmailSmtpRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Limit    int64  `json:"limit"`
}

type DeleteEmailSmtpRequest struct {
	ID string `uri:"id" binding:"required"`
}
