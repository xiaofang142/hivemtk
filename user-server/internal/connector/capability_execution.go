package connector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/pkg/tenant"
)

// CapabilityRequestBuilder 仅由服务端适配器装配，必须用所返回连接修订的凭证构造请求。
// 不得发起网络调用或把调用参数当作授权；出站由 Executor 在重新裁决后执行。
type CapabilityRequestBuilder interface {
	BuildCapabilityRequest(context.Context, ConnectorConfig, EndpointConfig, map[string]any) (*http.Request, capregistry.CapabilityExecutionIdentity, error)
}

type capabilityExecution struct {
	resolver capregistry.PersistentCapabilityResolver
	builder  CapabilityRequestBuilder
}

func NewExecutorWithCapabilityBinding(resolver capregistry.PersistentCapabilityResolver, builder CapabilityRequestBuilder) *Executor {
	executor := NewExecutor()
	executor.execution = &capabilityExecution{resolver: resolver, builder: builder}
	return executor
}

func (e *Executor) boundWriteRequest(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, args map[string]any) (*http.Request, error) {
	if e.execution == nil || e.execution.builder == nil || !validCapabilityCellRef(ep.Cell) || ep.Cell.Carrier != capregistry.ModeAPIAdapter {
		return nil, ErrCapabilityCellDenied
	}
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return nil, ErrCapabilityCellDenied
	}
	shopArgument := ep.Cell.ShopArgument
	if shopArgument == "" {
		shopArgument = "shop_id"
	}
	shop, _ := args[shopArgument].(string)
	order, _ := args["order_id"].(string)
	if strings.TrimSpace(shop) == "" || strings.TrimSpace(order) == "" {
		return nil, ErrCapabilityCellDenied
	}
	key := capregistry.CellKey{TenantID: id, ShopID: shop, Product: ep.Cell.Product, Region: ep.Cell.Region, Action: ep.Cell.Action, Carrier: ep.Cell.Carrier}
	source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceAfterSale, Product: key.Product, ResourceID: order}
	if err := e.execution.resolver.Authorize(ctx, source, key.Action, key.Carrier, key.ShopID, key.Region, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCapabilityCellDenied, err)
	}
	req, identity, err := e.execution.builder.BuildCapabilityRequest(ctx, cfg, ep, args)
	accepted := false
	defer func() {
		if !accepted && req != nil && req.Body != nil {
			_ = req.Body.Close()
		}
	}()
	if err != nil {
		return nil, err
	}
	if req == nil || req.URL == nil || req.URL.User != nil || req.URL.Fragment != "" || req.Host != "" && req.Host != req.URL.Host {
		return nil, ErrCapabilityCellDenied
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme != req.URL.Scheme || base.Host != req.URL.Host {
		return nil, ErrCapabilityCellDenied
	}
	validator := e.baseURLValidator
	if validator == nil {
		validator = tooluse.ValidateOpenAPIBaseURL
	}
	if err := validator(req.URL.String()); err != nil {
		return nil, err
	}
	if err := e.execution.resolver.AuthorizeExecution(ctx, source, key, identity, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCapabilityCellDenied, err)
	}
	accepted = true
	return req, nil
}
