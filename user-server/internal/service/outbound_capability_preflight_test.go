package service

import (
	"errors"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
)

func TestValidateBridgeOutboundCapabilityPreflight(t *testing.T) {
	for _, ch := range []string{"douyin", "douyin_web", "douyin_shop"} {
		if err := ValidateBridgeOutboundCapabilityPreflight(ch, "10"); err != nil {
			t.Errorf("%s 应已登记 message.text.send/bridge 能力格，实际=%v", ch, err)
		}
	}
	for _, ch := range []string{"", "personal_wx", "xhs", "weibo"} {
		err := ValidateBridgeOutboundCapabilityPreflight(ch, "10")
		if err == nil {
			t.Errorf("%q 无静态桥接发送格，应 fail fast", ch)
			continue
		}
		var be *bizerr.BizError
		if !errors.As(err, &be) || be.Code != bizerr.CodeParamInvalid {
			t.Errorf("%q 期望 CodeParamInvalid，实际=%v", ch, err)
		}
	}
}
