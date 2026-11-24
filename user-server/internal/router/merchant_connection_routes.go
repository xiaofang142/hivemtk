package router

import (
	"context"
	"os"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// fakeMerchantProvider is a test/dev-only simulated provider (opt-in). Never enable in production.
// It is intentionally fail-closed: Start can return an authorization URL shape in dev,
// but Exchange always rejects so no credentials are minted (no successful E2E completion).
type fakeMerchantProvider struct{}

func (f *fakeMerchantProvider) Contract(ctx context.Context) (service.MerchantOAuthContract, error) {
	return service.MerchantOAuthContract{
		Provider:              "fake_merchant",
		Version:               "v1",
		AuthorizationEndpoint: "https://example.com/oauth/authorize",
		RedirectURI:           "https://example.com/callback",
		ClientID:              "fake-client-id",
		PKCE:                  service.MerchantPKCENone,
		Scopes:                []string{"message.text.send"},
	}, nil
}

func (f *fakeMerchantProvider) Exchange(ctx context.Context, input service.MerchantOAuthExchange) (service.MerchantOAuthGrant, error) {
	// Fake cannot know/verify the claimed shop from arbitrary codes; reject to keep behavior explicit.
	return service.MerchantOAuthGrant{}, service.ErrMerchantConnectionExchange
}

// setupMerchantConnectionRoutes wires merchant connection APIs.
// Env/switch behavior (local/simulated only):
//   - MERCHANT_CONNECTION_FAKE=1: registers the in-memory fake_merchant provider (also when GIN_MODE=="debug").
//     The fake provider is fail-closed for exchange (see fakeMerchantProvider.Exchange) and must never be enabled in production.
//   - Without any providers, routes are mounted with an empty provider map (production assembly: register real providers here).
func setupMerchantConnectionRoutes(tenantAdmin *gin.RouterGroup, db *gorm.DB) {
	providers := map[string]service.MerchantConnectionProvider{}
	// Opt-in fake provider for dev/test only. Never enable in production by default.
	if os.Getenv("MERCHANT_CONNECTION_FAKE") == "1" || os.Getenv("GIN_MODE") == "debug" {
		providers["fake_merchant"] = &fakeMerchantProvider{}
	}
	svc := service.NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), providers)
	mountMerchantConnectionRoutes(tenantAdmin, svc)
}

func mountMerchantConnectionRoutes(tenantAdmin *gin.RouterGroup, svc service.MerchantConnectionService) {
	ctrl := controller.NewMerchantConnectionController(svc)
	// 显式前缀供静态 API 契约审计解析；隐私中间件与路由验收仍校验相同路径。
	group := tenantAdmin.Group("/v1/integrations/merchant-connections")
	group.GET("", ctrl.Status)
	group.POST("/start", ctrl.Start)
	group.POST("/complete", ctrl.Complete)
	group.POST("/refresh", ctrl.Refresh)
	group.POST("/revoke", ctrl.Revoke)
	// Handle trailing slashes directly. Gin's automatic redirect logs the raw URL in debug mode.
	group.GET("/", ctrl.Status)
	group.POST("/start/", ctrl.Start)
	group.POST("/complete/", ctrl.Complete)
	group.POST("/refresh/", ctrl.Refresh)
	group.POST("/revoke/", ctrl.Revoke)
}
