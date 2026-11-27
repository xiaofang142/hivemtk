package llm

import "testing"

func TestResolveRouteMode(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		route   string
		hasKey  bool
		want    string
		wantErr bool
	}{
		{name: "self_defaults_direct", source: "self", want: RouteModeDirect},
		{name: "empty_source_self_key_direct", source: "", hasKey: true, want: RouteModeDirect},
		{name: "explicit_proxy", source: "self", route: RouteModeProxy, want: RouteModeProxy},
		{name: "platform_forces_proxy_without_key", source: "platform", want: RouteModeProxy},
		{name: "platform_forces_proxy_with_key", source: "platform", hasKey: true, want: RouteModeProxy},
		{name: "platform_cannot_override_direct", source: "platform", route: RouteModeDirect, want: RouteModeProxy},
		{name: "invalid_route_rejected", source: "self", route: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &ProviderConfig{Source: tt.source, RouteMode: tt.route}
			if tt.hasKey {
				provider.APIKey = "local-key"
			}
			got, err := ResolveRouteMode(provider)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("非法 RouteMode 必须拒绝，实际 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("路由模式错误：got %q want %q", got, tt.want)
			}
		})
	}
}
