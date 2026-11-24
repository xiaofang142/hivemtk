package service

import (
	"errors"
	"fmt"
)

// ErrPlatformProjectionStale 表示平台侧动作已生效，但本地状态投影落库失败：
// 平台实态与数据库里的可见状态不一致，需要以「待对账」语义返回给调用方，
// 绝不能包装成刷新成功（FA-ASSET-SUBMIT-01）。
var ErrPlatformProjectionStale = errors.New("平台状态本地投影未同步")

// PlatformProjectionStaleError 同时携带平台实态与本地投影，供控制器返回
// 结构化的 202 Accepted 响应，供前端分别展示、提示重试对账。
type PlatformProjectionStaleError struct {
	PlatformAssetID    int64
	PlatformStatus     string
	PlatformReviewNote string
	LocalStatus        string
	Cause              error
}

func (e *PlatformProjectionStaleError) Error() string {
	return fmt.Sprintf("平台实态=%s（资产 %d），本地投影=%s，本地同步失败：%v",
		e.PlatformStatus, e.PlatformAssetID, e.LocalStatus, e.Cause)
}

func (e *PlatformProjectionStaleError) Unwrap() error { return e.Cause }

func (e *PlatformProjectionStaleError) Is(target error) bool {
	return target == ErrPlatformProjectionStale
}
