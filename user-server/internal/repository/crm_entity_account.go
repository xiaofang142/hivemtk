package repository

import "gorm.io/gorm"

// CRMEntityAccountFilter 在计数与分页之前限定关联实体的账号。
// 未挂账号的旧实体继承已验证的客户范围；显式账号必须单独授权。
type CRMEntityAccountFilter struct {
	RestrictedAccounts bool
	AccountIDs         []uint
	AllowUnassigned    bool
}

func applyCRMEntityAccounts(q *gorm.DB, filters []CRMEntityAccountFilter) *gorm.DB {
	for _, filter := range filters {
		if !filter.RestrictedAccounts {
			continue
		}
		if filter.AllowUnassigned {
			if len(filter.AccountIDs) == 0 {
				q = q.Where("channel_account_id IS NULL")
			} else {
				q = q.Where("channel_account_id IS NULL OR channel_account_id IN ?", filter.AccountIDs)
			}
		} else if len(filter.AccountIDs) == 0 {
			q = q.Where("1 = 0")
		} else {
			q = q.Where("channel_account_id IN ?", filter.AccountIDs)
		}
	}
	return q
}
