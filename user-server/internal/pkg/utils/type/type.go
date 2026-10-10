package _type

type Proxy struct {
	EnableProxy bool   `json:"enable_proxy" desc:"是否开启网络代理"`
	Protocol    string `json:"protocol" desc:"协议"`
	Host        string `json:"host" desc:"域名"`
	Port        uint   `json:"port" desc:"端口"`
}
type Headers map[string]string

type UserStatusType int

const (
	UserStatusValid   UserStatusType = 1
	UserStatusInvalid UserStatusType = 0
)

type OrderStatusType int

const (
	OrderStatusPending      OrderStatusType = 0
	OrderStatusSuccess      OrderStatusType = 1
	OrderStatusForceSuccess OrderStatusType = 2
	OrderStatusTimeout      OrderStatusType = -1
	OrderStatusForceClose   OrderStatusType = -2
)

// 新增AccountStatusType定义
type AccountStatusType int

const (
	AccountStatusActive   AccountStatusType = 1
	AccountStatusInactive AccountStatusType = 0
)

type SmlistType int

const (
	SmlistTypeGirlSlriGistType SmlistType = 1
	SmlistTypeGirlMlriGype     SmlistType = 2
	SmlistTypeManSnaMistType   SmlistType = 3
	SmlistTypeManMnaM          SmlistType = 4
)
