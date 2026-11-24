package connector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type executionReader struct {
	connection model.MerchantConnection
	binding    model.CapabilitySourceBinding
	evidence   model.CapabilityEvidence
}

func (r *executionReader) FindCapabilityBinding(context.Context, uint, capregistry.CapabilitySource, bool) (model.CapabilitySourceBinding, error) {
	return r.binding, nil
}
func (r *executionReader) FindCapabilityConnection(context.Context, capregistry.CellKey) (model.MerchantConnection, error) {
	return r.connection, nil
}
func (r *executionReader) FindCapabilityEvidence(context.Context, capregistry.CellKey) (model.CapabilityEvidence, error) {
	return r.evidence, nil
}

type executionProof struct{}

func (executionProof) VerifyCapabilityProof(_ context.Context, q capregistry.CapabilityProofQuery) capregistry.CapabilityProof {
	return capregistry.CapabilityProof{RuntimeVerified: true, Identity: capregistry.CapabilityProofIdentity{Key: q.Key, Source: q.Source, Provenance: q.Evidence.Provenance, ProofRef: q.Evidence.RuntimeProofRef, CredentialContract: "contract-A", ConnectionID: 1, ConnectionVersion: 2, CredentialVersion: 3, ConsentVersion: 4}, PreflightSource: q.Source, PreflightObservedAt: q.At, PreflightExpires: q.At.Add(time.Minute)}
}

type executionBuilder func(context.Context, ConnectorConfig, EndpointConfig, map[string]any) (*http.Request, capregistry.CapabilityExecutionIdentity, error)

func (f executionBuilder) BuildCapabilityRequest(ctx context.Context, c ConnectorConfig, e EndpointConfig, a map[string]any) (*http.Request, capregistry.CapabilityExecutionIdentity, error) {
	return f(ctx, c, e, a)
}

type executionBody struct{ closed bool }

func (*executionBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *executionBody) Close() error           { b.closed = true; return nil }

// fake HTTP 只验证精确凭证绑定与最后一次授权重核，不构成任何真实平台证据。
func TestConnectorBoundExecution_Fences(t *testing.T) {
	for _, name := range []string{"精确绑定", "错凭证版本", "错契约", "构造期间撤权", "构造期间轮换", "跨租户", "无适配器", "不安全地址", "跨目标地址", "重定向"} {
		t.Run(name, func(t *testing.T) {
			var hits, redirects atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirects.Add(1); w.WriteHeader(http.StatusOK) }))
			defer redirect.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if name == "重定向" {
					http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
					return
				}
				if r.Header.Get("Authorization") != "Bearer connection-token" {
					t.Error("必须使用连接凭证")
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			expires := time.Now().Add(time.Hour)
			key := capregistry.CellKey{TenantID: 81, ShopID: "shop-A", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: capregistry.ModeAPIAdapter}
			def, _ := capregistry.FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
			reader := &executionReader{connection: model.MerchantConnection{ID: 1, TenantID: key.TenantID, Provider: key.Product, ShopID: key.ShopID, Version: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A", Scopes: def.RequiredScopes, ExpiresAt: &expires},
				binding:  model.CapabilitySourceBinding{ID: 1, TenantID: key.TenantID, Source: capregistry.SubmissionSourceAfterSale, Product: key.Product, ResourceID: "order-A", ShopID: key.ShopID, Region: key.Region, ConnectionID: 1, ConsentVersion: 4},
				evidence: model.CapabilityEvidence{ID: 1, TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier, ConnectionID: 1, ConnectionVersion: 2, CredentialVersion: 3, ConsentVersion: 4, ContractVersion: def.ContractVersion, DocumentationVersion: def.DocumentationVersion, Scopes: def.RequiredScopes, ExpiresAt: expires, Documentation: true, Implementation: true, Provenance: "test", RuntimeProofRef: "fake-proof"}}
			body := &executionBody{}
			built := false
			builder := executionBuilder(func(ctx context.Context, cfg ConnectorConfig, _ EndpointConfig, _ map[string]any) (*http.Request, capregistry.CapabilityExecutionIdentity, error) {
				identity := capregistry.CapabilityExecutionIdentity{ConnectionID: 1, ConnectionVersion: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A"}
				target := cfg.BaseURL + "/write"
				switch name {
				case "错凭证版本":
					identity.CredentialVersion--
				case "错契约":
					identity.CredentialContract = "other"
				case "构造期间撤权":
					reader.connection.Revoked = true
				case "构造期间轮换":
					reader.connection.CredentialVersion++
				case "不安全地址":
					target = "http://127.0.0.1:1/private"
				case "跨目标地址":
					target = redirect.URL
				}
				built = true
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
				if err == nil {
					req.Header.Set("Authorization", "Bearer connection-token")
				}
				return req, identity, err
			})
			resolver := capregistry.PersistentCapabilityResolver{Reader: reader, Verifier: executionProof{}}
			executor := NewExecutorWithCapabilityBinding(resolver, builder)
			if name == "无适配器" {
				executor = NewExecutorWithCapabilityBinding(resolver, nil)
			}
			executor.baseURLValidator = func(raw string) error {
				if len(raw) >= len(server.URL) && raw[:len(server.URL)] == server.URL {
					return nil
				}
				if name == "跨目标地址" && raw == redirect.URL {
					return nil
				}
				return errors.New("仅允许fake测试端点")
			}
			ctx := tenant.WithTenantID(context.Background(), key.TenantID)
			if name == "跨租户" {
				ctx = tenant.WithTenantID(context.Background(), 82)
			}
			_, err := executor.Call(ctx, ConnectorConfig{BaseURL: server.URL, Provider: "generic", Auth: ConnectorAuth{Type: "bearer", Key: "global-unbound-key"}}, EndpointConfig{Name: "write", Method: http.MethodPost, Path: "/write", Capabilities: []string{"modify_order"}, Cell: localWriteCellRef()}, map[string]any{"shop_id": key.ShopID, "order_id": "order-A"})
			expectedHits := int32(0)
			if name == "精确绑定" || name == "重定向" {
				expectedHits = 1
			}
			if name == "精确绑定" {
				if err != nil {
					t.Fatalf("精确绑定应仅调用fake端点一次，错误=%v", err)
				}
			} else if err == nil {
				t.Fatal("失效绑定必须拒绝")
			}
			if built && expectedHits == 0 && !body.closed {
				t.Fatal("构造后拒绝必须关闭未发送请求体")
			}
			if hits.Load() != expectedHits || redirects.Load() != 0 {
				t.Fatalf("拒绝不能外发或重定向，hits=%d redirects=%d", hits.Load(), redirects.Load())
			}
		})
	}
}
