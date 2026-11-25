package service

// LeadMaintainerMap 维护人 × 账号映射（LM-15 的纯契约部分）。
// 按线索真实来源账号解析责任人；未绑定返回空，不猜测、不默认发全员。
type LeadMaintainerMap []LeadMaintainerEntry

type LeadMaintainerEntry struct {
	Maintainer string   `json:"maintainer"`
	Accounts   []string `json:"accounts"`
}

// MaintainersFor 返回该来源账号的全部维护人（保持配置顺序）。
func (m LeadMaintainerMap) MaintainersFor(account string) []string {
	var out []string
	for _, e := range m {
		for _, a := range e.Accounts {
			if a == account {
				out = append(out, e.Maintainer)
				break
			}
		}
	}
	return out
}
