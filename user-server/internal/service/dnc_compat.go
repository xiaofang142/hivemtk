package service

import (
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/dnc"
)

// 兼容别名：退订判定已下沉到 internal/service/dnc（按域拆包的前置），
// 既有装配点/控制器/测试继续用 service.* 这一层，不改调用面。
type DoNotContactService = dnc.DoNotContactService

type PhoneToOneIDResolver = dnc.PhoneToOneIDResolver

const PhoneOneIDPrefix = dnc.PhoneOneIDPrefix

func NewDoNotContactService(repo repository.CustomerDoNotContactRepository) *DoNotContactService {
	return dnc.NewDoNotContactService(repo)
}
