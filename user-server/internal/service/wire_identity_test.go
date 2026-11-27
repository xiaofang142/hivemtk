package service

import (
	"testing"

	"hivemtk-user/internal/pkg/channelbus"
)

// TestServiceAckStatusUsesCanonicalWireIdentity 钉住适配层回执状态与线路协议同源：
// SageX / webhook 不得复制 accepted/duplicate/retry/rejected 字面量。
func TestServiceAckStatusUsesCanonicalWireIdentity(t *testing.T) {
	checks := map[string][2]string{
		"sagex.accepted":    {SageXAckAccepted, channelbus.StatusAccepted},
		"sagex.duplicate":   {SageXAckDuplicate, channelbus.StatusDuplicate},
		"sagex.retry":       {SageXAckRetry, channelbus.StatusRetry},
		"sagex.rejected":    {SageXAckRejected, channelbus.StatusRejected},
		"webhook.accepted":  {WebhookIngestAckAccepted, channelbus.StatusAccepted},
		"webhook.duplicate": {WebhookIngestAckDuplicate, channelbus.StatusDuplicate},
		"webhook.retry":     {WebhookIngestAckRetry, channelbus.StatusRetry},
		"webhook.rejected":  {WebhookIngestAckRejected, channelbus.StatusRejected},
		"sagex.protocol":    {SageXProtocolVersion, channelbus.SchemaVersion},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s 漂移: 适配层=%q channelbus=%q", name, pair[0], pair[1])
		}
	}
}
