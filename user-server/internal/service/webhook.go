package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/channelbot/whatsapp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type WebhookService struct {
	eventRepo   *repository.WebhookEventRepository
	accountRepo *repository.IntegrationAccountRepository

	wecomRepo   *repository.WeComAccountRepository
	integration *WeComIntegrationService

	feishuIntegration *FeishuIntegrationService
	tgIntegration     *TelegramIntegrationService
	tgGate            *TelegramGateService
	waIntegration     *WhatsAppCloudIntegrationService
	qqIntegration     *QQIntegrationService

	wechatIntegration *WechatService

	telegramRepo *repository.TelegramAccountRepository
	qqRepo       *repository.QQAccountRepository

	feishuRepo *repository.FeishuAccountRepository
	waRepo     *repository.WhatsAppCloudAccountRepository

	messageHubRepo *repository.MessageHubRepository
	inboxConvRepo  *repository.InboxConversationRepository
	unifiedMsgRepo repository.UnifiedMessageRepository
	delayedRepo    *repository.DelayedOutboundRepository

	clueRepo repository.ClueRepository

	ingressSvc *InboxIngressService

	salesEngine       *SalesEngine
	smartOrchestrator *SmartCSOrchestrator

	agentBindingSvc *ChannelAgentBindingService

	mu        sync.Mutex
	rlMu      sync.Mutex
	rlBuckets map[string]*tokenBucket

	workerCount int
	queue       chan *webhookJob
	wg          sync.WaitGroup
	stopCh      chan struct{}
	stopped     bool

	replySem chan struct{}
}

func (b *tokenBucket) allow(ctx context.Context) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > float64(b.capacity) {
		b.tokens = float64(b.capacity)
	}
	b.lastRefill = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

type WebhookChannel string

func NewWebhookService(db *gorm.DB) *WebhookService {
	guardInsecureWebhookAtStartup()
	wecomRepo := repository.NewWeComAccountRepository()
	if db != nil {
		wecomRepo.SetDB(context.Background(), db)
	}
	accountRepo := repository.NewIntegrationAccountRepository()
	if db != nil {
		accountRepo.SetDB(context.Background(), db)
	}
	eventRepo := repository.NewWebhookEventRepository()
	if db != nil {
		repository.SetWebhookEventRepoDB(eventRepo, db)
	}
	telegramRepo := repository.NewTelegramAccountRepository()
	if db != nil {
		telegramRepo.SetDB(context.Background(), db)
	}
	qqRepo := repository.NewQQAccountRepository()
	if db != nil {
		qqRepo.SetDB(context.Background(), db)
	}
	feishuRepo := repository.NewFeishuAccountRepository()
	if db != nil {
		feishuRepo.SetDB(context.Background(), db)
	}
	waRepo := repository.NewWhatsAppCloudAccountRepository()
	if db != nil {
		waRepo.SetDB(context.Background(), db)
	}

	var messageHubRepo *repository.MessageHubRepository
	if db != nil {
		messageHubRepo = repository.NewMessageHubRepository()
		repository.SetMessageHubRepoDB(messageHubRepo, db)
	}
	var inboxConvRepo *repository.InboxConversationRepository
	if db != nil {
		inboxConvRepo = repository.NewInboxConversationRepository()
		repository.SetInboxConversationRepoDB(inboxConvRepo, db)
	}
	var unifiedMsgRepo repository.UnifiedMessageRepository
	if db != nil {
		unifiedMsgRepo = repository.NewUnifiedMessageRepositoryWithDB(db)
	}
	s := &WebhookService{
		eventRepo:         eventRepo,
		accountRepo:       accountRepo,
		wecomRepo:         wecomRepo,
		integration:       NewWeComIntegrationService(db),
		feishuIntegration: NewFeishuIntegrationService(db),
		tgIntegration:     NewTelegramIntegrationService(db),
		waIntegration:     NewWhatsAppCloudIntegrationService(db),
		qqIntegration:     NewQQIntegrationService(db),
		wechatIntegration: NewWechatService(db),
		telegramRepo:      telegramRepo,
		qqRepo:            qqRepo,
		tgGate:            NewTelegramGateService(db),
		feishuRepo:        feishuRepo,
		waRepo:            waRepo,
		messageHubRepo:    messageHubRepo,
		inboxConvRepo:     inboxConvRepo,
		unifiedMsgRepo:    unifiedMsgRepo,
		delayedRepo:       repository.NewDelayedOutboundRepository(db),
		ingressSvc:        NewInboxIngressServiceWithDB(db, nil),
		rlBuckets:         make(map[string]*tokenBucket),
		workerCount:       webhookEnvInt("WEBHOOK_WORKER_COUNT", WebhookWorkerCount),
		queue:             make(chan *webhookJob, webhookEnvInt("WEBHOOK_QUEUE_SIZE", WebhookQueueSize)),
		stopCh:            make(chan struct{}),
		replySem:          make(chan struct{}, webhookEnvInt("WEBHOOK_REPLY_CONCURRENCY", WebhookReplyConcurrency)),
	}
	s.startWorkers(context.Background())
	s.startRLJanitor(context.Background())

	s.startRecoveryScanner()
	// 免打扰（23:00-07:00）到期回复的投递循环必须在服务装配时就起来：原先唯一启动点
	// 藏在 enqueueDelayedOutbound 内部，即"只有再次命中免打扰才会有人消费"，
	// 进程重启后已到期的 AI 回复会永久搁置（T-P0-07）。
	s.startDelayedOutboundDispatch()

	// 审计 N-04：此处原先注册 globalReorderBuffer.FlushHandler（WhatsApp 乱序缓冲的
	// 稍后投递回调）。该回调随缓冲一起删除——缓冲的 delayed 分支不可达，回调实为
	// 死代码；留着它还等于在 handleJob 之外悄悄开了第二条 WhatsApp 派发+AI 触发路径。

	return s
}

func (s *WebhookService) SetIngressSvc(ingress *InboxIngressService) {
	if ingress != nil {
		s.ingressSvc = ingress
	}
}

// lazyDB 惰性获取底层连接：优先 eventRepo（构造器必设），兜底全局 repository。
// ARC-01：WebhookService 不再自持 db 字段，统一经此取源。
func (s *WebhookService) lazyDB() *gorm.DB {
	if s.eventRepo != nil {
		if db := s.eventRepo.GetDB(); db != nil {
			return db
		}
	}
	return repository.GetDB()
}

func (s *WebhookService) ensureReposFromDB(ctx context.Context) {
	db := s.lazyDB()
	if db == nil {
		return
	}
	if s.messageHubRepo == nil {
		s.messageHubRepo = repository.NewMessageHubRepository()
		repository.SetMessageHubRepoDB(s.messageHubRepo, db)
	}
	if s.inboxConvRepo == nil {
		s.inboxConvRepo = repository.NewInboxConversationRepository()
		repository.SetInboxConversationRepoDB(s.inboxConvRepo, db)
	}
	if s.unifiedMsgRepo == nil {
		s.unifiedMsgRepo = repository.NewUnifiedMessageRepositoryWithDB(db)
	}
	if s.clueRepo == nil {
		s.clueRepo = repository.NewClueRepositoryWithDB(db)
	}
}

func (s *WebhookService) SetAgentBindingService(ctx context.Context, svc *ChannelAgentBindingService) {
	s.agentBindingSvc = svc
}

type webhookIngressAdapter struct{ svc *InboxIngressService }

func (a webhookIngressAdapter) HandleIngressMessage(ctx context.Context, event *model.MessageEvent) error {
	if a.svc == nil {
		return nil
	}
	_, err := a.svc.HandleIngressMessage(ctx, event)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate")) {

		return nil
	}
	return err
}

func (s *WebhookService) ingressHandler(ctx context.Context) webhookIngressAdapter {
	return webhookIngressAdapter{svc: s.ingressSvc}
}

func (s *WebhookService) Stop(ctx context.Context) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:

	case <-time.After(2 * time.Second):

	}
}

func (s *WebhookService) startWorkers(ctx context.Context) {
	for i := 0; i < s.workerCount; i++ {
		s.wg.Add(1)

		id := i
		utils.SafeGo(ctx, "webhook.worker", func(ctx context.Context) {
			s.worker(ctx, id)
		})
	}
}

func (s *WebhookService) worker(ctx context.Context, id int) {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case job, ok := <-s.queue:
			if !ok {
				return
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[Webhook] worker-%d panic recovered, job dropped: %v", id, r)
					}
				}()
				s.handleJob(ctx, job)
			}()
		}
	}
}

type ReceiveRequest struct {
	Channel   WebhookChannel
	AccountID string
	Body      []byte
	Headers   map[string]string
	SourceIP  string
	Query     map[string]string
}

type ReceiveResult struct {
	Accepted     bool   `json:"accepted"`
	EventID      string `json:"event_id"`
	Duplicate    bool   `json:"duplicate"`
	RateLimit    bool   `json:"rate_limit"`
	VerifyFail   bool   `json:"verify_failed"`
	QueueFull    bool   `json:"queue_full,omitempty"`
	Reason       string `json:"reason,omitempty"`
	EventType    string `json:"event_type,omitempty"`
	Dispatched   bool   `json:"dispatched,omitempty"`
	HubMessageID string `json:"hub_message_id,omitempty"`
}

// ErrWebhookInboundNotWired 渠道在 dispatchToChannel 里没有入站分支。
// 走到这里说明能力表和路由分支漂移了（正常请求到不了：Receive 已按能力表提前拒绝），
// 唯一现实来源是恢复扫描器重放改造前入库的旧事件行。
var ErrWebhookInboundNotWired = errors.New("webhook inbound channel not wired")

// webhookInboundCapable 报告通用 webhook 路由能否真正服务该渠道的入站回调：
// dispatchToChannel 有分支 ⇒ 消息能落 message_hub、进收件箱、触发 AI 并出站。
//
// 不在表内的渠道一律按不支持处理（含未来新增的渠道常量）：宁可明确拒绝，
// 也不能静默收下（审计 D-03）。
func webhookInboundCapable(channel WebhookChannel) bool {
	switch channel {
	case ChannelWeCom, ChannelWhatsapp, ChannelTelegram, ChannelQQ,
		ChannelFeishu, ChannelDouyin, ChannelTiktok:
		return true
	default:
		return false
	}
}

// webhookInboundRejectHints 已声明枚举、但通用 webhook 路由不处理的渠道 → 拒绝理由（含正确入口）。
// 只是文案表，不参与能力判定；条目与实际能力漂移由
// TestWebhookInbound_RejectHintsStayConsistentWithCapability 拦下。
var webhookInboundRejectHints = map[WebhookChannel]string{
	ChannelKuaishou:    "快手无官方客服/私信服务端回调，入站只走浏览器桥 POST /api/bridge/ingest",
	ChannelXiaohongshu: "小红书无公开服务端回调，入站只走浏览器桥 POST /api/bridge/ingest",
	ChannelXianyu:      "闲鱼无任何公开 API，入站只走浏览器桥 POST /api/bridge/ingest",
	ChannelWechat:      "微信公众号入站走专用回调 POST /api/webhook/wechat/{account_id}（自行验签），通用路由不处理",
	ChannelDingTalk:    "钉钉入站走专用回调 POST /api/webhook/dingtalk/{account_id}（机器人/事件订阅报文），通用路由不处理",
	ChannelCustom:      "custom 无入站适配器：消息落库后既不进收件箱也不会触发 AI，通用路由不受理",
}

func webhookInboundRejectReason(channel WebhookChannel) string {
	if hint, ok := webhookInboundRejectHints[channel]; ok {
		return "渠道 " + string(channel) + " 不支持通用 webhook 入站：" + hint
	}
	return "渠道 " + string(channel) + " 无入站适配器，通用 webhook 不受理"
}

func (s *WebhookService) Receive(ctx context.Context, req *ReceiveRequest) (*ReceiveResult, error) {
	if req == nil || len(req.Body) == 0 {
		return &ReceiveResult{Accepted: false, Reason: "empty body"}, nil
	}
	if req.AccountID == "" {
		return &ReceiveResult{Accepted: false, Reason: "missing account_id"}, nil
	}
	if req.Channel == "" {
		return &ReceiveResult{Accepted: false, Reason: "missing channel"}, nil
	}
	if !webhookInboundCapable(req.Channel) {
		// 必须在这里拒绝，不能验签后收下回 200：渠道方收到 200 就不重投了，
		// 而这条客户消息既没进收件箱也不会被回复，等于凭空消失（审计 D-03）。
		return &ReceiveResult{Accepted: false, Reason: webhookInboundRejectReason(req.Channel)}, nil
	}

	verified, err := s.Verify(ctx, req.Channel, req.AccountID, req.Body, req.Headers, req.Query)
	if err != nil {
		logger.Errorf("[Webhook] 验签异常 channel=%s account=%s: %v", req.Channel, req.AccountID, err)
		return &ReceiveResult{Accepted: false, VerifyFail: true, Reason: "verify error: " + err.Error()}, nil
	}
	if !verified {
		return &ReceiveResult{Accepted: false, VerifyFail: true, Reason: "signature mismatch"}, nil
	}

	payload, err := s.ParsePayload(ctx, req.Channel, req.Body)
	if err != nil {
		return &ReceiveResult{Accepted: false, Reason: "parse error: " + err.Error()}, nil
	}
	if payload.EventID == "" {
		// 优先官方文档明示的重复投递判定键（update_id / event_id / MsgId / wamid 集合），
		// 整包字节哈希只在该键缺失时兜底：渠道重试时重排字段或补写字段都会让哈希变化，
		// 同一条客户消息被判成新事件而二次驱动 AI。
		payload.EventID = officialEventID(req.Channel, req.AccountID, req.Body)
	}
	if payload.EventID == "" {
		payload.EventID = s.generateEventID(ctx, req.Channel, req.AccountID, req.Body)
	}
	if payload.EventType == "" {
		payload.EventType = "unknown"
	}

	if s.isDuplicate(ctx, payload.EventID) {
		return &ReceiveResult{
			Accepted:  true,
			EventID:   payload.EventID,
			Duplicate: true,
		}, nil
	}

	key := string(req.Channel) + ":" + req.AccountID
	if !s.allowRate(ctx, key) {
		return &ReceiveResult{Accepted: false, RateLimit: true, Reason: "rate limited"}, nil
	}

	evt := &model.WebhookEvent{
		Platform:  string(req.Channel),
		EventID:   payload.EventID,
		EventType: payload.EventType,
		AccountID: req.AccountID,
		RawData:   s.TruncateForStore(ctx, req.Body),
		Processed: false,
	}
	if s.eventRepo != nil {
		if err := s.eventRepo.Create(ctx, evt); err != nil {
			// uni_webhook_events_event_id 唯一约束冲突 = 该事件已落库处理过。
			// 场景：Redis 去重仅 5 分钟 TTL，而 Telegram 对非 2xx 的重试窗口远超 5 分钟，
			// 长窗口重发会撞这里。必须按"重复事件"幂等放行（accepted=true），
			// 否则 accepted=false 会让 TG 无限重试同一 update。
			if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
				return &ReceiveResult{Accepted: true, Duplicate: true, EventID: payload.EventID}, nil
			}
			return &ReceiveResult{Accepted: false, Reason: "persist error: " + err.Error()}, nil
		}
	}

	job := &webhookJob{
		event:   evt,
		raw:     req.Body,
		header:  req.Headers,
		source:  req.SourceIP,
		channel: req.Channel,
		account: req.AccountID,
		payload: payload,
	}

	if s.queue == nil {
		return &ReceiveResult{
			Accepted:  true,
			EventID:   payload.EventID,
			EventType: payload.EventType,
		}, nil
	}
	select {
	case s.queue <- job:
	default:
		return &ReceiveResult{
			Accepted:  false,
			QueueFull: true,
			Reason:    "queue full, please retry after a short delay",
			EventID:   payload.EventID,
			EventType: payload.EventType,
		}, nil
	}

	return &ReceiveResult{
		Accepted:  true,
		EventID:   payload.EventID,
		EventType: payload.EventType,
	}, nil
}

func insecureWebhookStartupError(appEnv, mode, ginMode, allowInsecure string) error {
	if allowInsecure != "true" {
		return nil
	}
	env := strings.ToLower(strings.TrimSpace(appEnv))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(mode))
	}
	if env == "" {
		// 未声明 APP_ENV/MODE 时按 GIN_MODE 判定，仍不明确则取生产姿态（与
		// config.IsDevelopmentEnv 同口径）：跳过验签的开关必须来自显式的开发意图。
		if strings.EqualFold(strings.TrimSpace(ginMode), "debug") {
			return nil
		}
		return errors.New("ALLOW_INSECURE_WEBHOOK=true 但环境未显式声明为开发：" +
			"请设置 APP_ENV=development（或 GIN_MODE=debug），或在生产环境移除该变量")
	}
	switch env {
	case "dev", "development", "debug", "test", "testing", "local":
		return nil
	default:
		return errors.New("ALLOW_INSECURE_WEBHOOK=true 禁止在非开发环境(APP_ENV/MODE=" + env + ")使用：" +
			"该开关会跳过渠道 webhook 验签。请移除该环境变量，并为企业微信/飞书/Telegram 等" +
			"各渠道账号配置正确的 CallbackToken/AppSecret 后重启")
	}
}

var insecureWebhookGuardOnce sync.Once

// insecureWebhookAllowed 开发联调开关：显式置 true 才跳过渠道验签（受启动环境护栏约束）。
//
// 豁免范围严格限定为「该渠道账号压根没配密钥」；已配置密钥的账号无论开关如何
// 都走真实验签，否则这个开关就成了伪造签名的通用后门（审计 S-02）。
func insecureWebhookAllowed() bool {
	return os.Getenv("ALLOW_INSECURE_WEBHOOK") == "true"
}

// logInsecureWebhookBypass 给每一次豁免留痕，避免开发开关长期静默生效。
func logInsecureWebhookBypass(channel, accountID string, cause error) {
	logger.Warnf("[Webhook] %s 渠道密钥未配置(account=%s)，ALLOW_INSECURE_WEBHOOK=true 已启用，跳过本次验签 cause=%v",
		channel, accountID, cause)
}

func guardInsecureWebhookAtStartup() {
	insecureWebhookGuardOnce.Do(func() {
		// 护栏要拦的是常驻服务进程，不是 go test：判定只看 APP_ENV/MODE/GIN_MODE，
		// 而仓库 .env 里 GIN_MODE=release，用例又各自 t.Setenv(ALLOW_INSECURE_WEBHOOK)，
		// 于是"本进程第一个 NewWebhookService 来自哪个用例"决定整个测试二进制会不会
		// 被 log.Fatalf 打死 —— 全量跑绿、按 -run 过滤跑就只剩一行无声 FAIL。
		if runningUnderGoTest() {
			return
		}
		if err := insecureWebhookStartupError(
			os.Getenv("APP_ENV"), os.Getenv("MODE"), os.Getenv("GIN_MODE"), os.Getenv("ALLOW_INSECURE_WEBHOOK"),
		); err != nil {
			log.Fatalf("[SECURITY] %v", err)
		}
	})
}

// runningUnderGoTest 与 utils 里同名判断同一口径（go test 产物以 .test 结尾）。
// utils 那份是包内私有函数，service 包取不到，只能按同一判据本地取一份。
func runningUnderGoTest() bool {
	return strings.HasSuffix(os.Args[0], ".test") || strings.HasSuffix(os.Args[0], ".test.exe")
}

func (s *WebhookService) Verify(ctx context.Context, channel WebhookChannel, accountID string, body []byte, headers map[string]string, query map[string]string) (bool, error) {
	switch channel {
	case ChannelWeCom:
		token, aesKey, err := s.getWeComSecrets(ctx, accountID)
		if err != nil || token == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass("wecom", accountID, err)
				return true, nil
			}
			return false, fmt.Errorf("wecom token missing: %v", err)
		}
		return verifyWeCom(token, aesKey, body, query)
	case ChannelWechat:

		token, _ := s.getWechatSecrets(ctx, accountID)
		if token == "" {
			// fail-closed：secret 未配置时拒绝验签（与其他渠道一致）。
			// 仅当显式 ALLOW_INSECURE_WEBHOOK=true（受启动环境护栏限制）才放行。
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass("wechat", accountID, errors.New("callback token 未配置"))
				return true, nil
			}
			return false, fmt.Errorf("wechat 验签 secret 未配置 account=%s，已拒绝请求；请为该账号配置 CallbackToken", accountID)
		}
		return verifyWechat(token, body, headers), nil
	case ChannelDouyin:
		secret, serr := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass(string(channel), accountID, serr)
				return true, nil
			}
			return false, fmt.Errorf("%s webhook secret 未配置(account=%s)，已拒绝请求；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true", channel, accountID)
		}
		// 官方口径（审计 §16.1 A 档原文）：sha1(client_secret ‖ 原始 body) 的 hex，
		// 放在 X-Douyin-Signature。此前这里用 HMAC-SHA256(secret, body)，与官方永不相等
		// ⇒ 真实回调 100% 被拒；而 verifyHMAC 的第二个参数 "Signature" 更不是抖音契约里的头。
		return verifyDouyinWebhook(secret, body, headers)
	case ChannelTiktok:
		secret, serr := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass(string(channel), accountID, serr)
				return true, nil
			}
			return false, fmt.Errorf("%s webhook secret 未配置(account=%s)，已拒绝请求；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true", channel, accountID)
		}
		return verifyTiktokWebhook(secret, body, headers)
	case ChannelTelegram:

		secret := s.getTelegramWebhookSecret(ctx, accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass("telegram", accountID, errors.New("webhook_secret 未配置"))
				return true, nil
			}
			return false, errors.New("telegram webhook secret 未配置；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true")
		}
		headerSecret := headers["X-Telegram-Bot-Api-Secret-Token"]
		if headerSecret == "" {
			headerSecret = headers["x-telegram-bot-api-secret-token"]
		}
		if headerSecret == "" {
			return false, errors.New("missing X-Telegram-Bot-Api-Secret-Token header")
		}
		return telegram.VerifyWebhook(secret, headerSecret), nil
	case ChannelQQ:
		secret := s.getQQWebhookSecret(ctx, accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass("qq", accountID, errors.New("BotSecret 未配置"))
				return true, nil
			}
			return false, errors.New("qq webhook secret 未配置（q.qq.com 管理端 BotSecret）")
		}
		sig := headers["X-Signature-Ed25519"]
		if sig == "" {
			sig = headers["X-Signature-ed25519"]
		}
		ts := headers["X-Signature-Timestamp"]
		if sig == "" || ts == "" {
			return false, errors.New("missing X-Signature-Ed25519/X-Signature-Timestamp header")
		}
		return qq.VerifySignature(secret, sig, ts, body), nil
	case ChannelFeishu:

		secret, _ := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {

			secret = s.getFeishuEncryptKey(ctx, accountID)
		}
		if secret == "" {
			// 未配 Encrypt Key ⇒ 飞书以明文推送且不发 X-Lark-Signature。
			// 此时唯一的来源凭证是事件体自带的 Verification Token
			// （v2.0 在 header.token、v1.0 在顶层 token），必须逐位比对，
			// 不得整段放行（审计 S-01：原实现在此 return true 是 fail-open）。
			vtoken := s.getFeishuVerificationToken(ctx, accountID)
			if vtoken == "" {
				if insecureWebhookAllowed() {
					logInsecureWebhookBypass("feishu", accountID, errors.New("encrypt_key 与 verification_token 均未配置"))
					return true, nil
				}
				return false, errors.New("feishu encrypt_key 与 verification_token 均未配置，无法验签；请为该账号配置 Verification Token")
			}
			return feishuPlaintextTokenMatches(vtoken, body), nil
		}
		return verifyFeishu(secret, body, headers), nil
	case ChannelKuaishou, ChannelXiaohongshu, ChannelXianyu:
		secret, serr := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass(string(channel), accountID, serr)
				return true, nil
			}
			return false, fmt.Errorf("%s webhook secret 未配置(account=%s)，已拒绝请求；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true", channel, accountID)
		}
		return verifyHMAC(secret, body, headers, "X-Signature", "Signature", "X-Hub-Signature-256"), nil
	case ChannelWhatsapp:

		secret, serr := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass("whatsapp", accountID, serr)
				return true, nil
			}
			return false, errors.New("whatsapp app secret 未配置；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true")
		}
		return whatsapp.VerifyWebhook(secret, body, headers["X-Hub-Signature-256"]), nil
	default:
		secret, serr := s.getAccountSecret(ctx, string(channel), accountID)
		if secret == "" {
			if insecureWebhookAllowed() {
				logInsecureWebhookBypass(string(channel), accountID, serr)
				return true, nil
			}
			return false, errors.New("webhook secret 未配置(channel=" + string(channel) + ")；开发环境请显式设置 ALLOW_INSECURE_WEBHOOK=true")
		}
		return verifyHMAC(secret, body, headers, "X-Signature", "Signature", "X-Hub-Signature-256"), nil
	}
}

// feishuPlaintextTokenMatches 校验飞书明文事件的 Verification Token。
// 官方布局：v2.0 schema 的 token 在 header.token，v1.0 在顶层 token；
// 常量时间比对，缺 token / 不匹配 / 非 JSON 一律不通过。
func feishuPlaintextTokenMatches(verificationToken string, body []byte) bool {
	if verificationToken == "" {
		return false
	}
	var probe struct {
		Token  string `json:"token"`
		Header struct {
			Token string `json:"token"`
		} `json:"header"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	got := probe.Header.Token
	if got == "" {
		got = probe.Token
	}
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(verificationToken)) == 1
}

func verifyFeishu(encryptKey string, body []byte, headers map[string]string) bool {
	if encryptKey == "" {
		return false
	}
	sig := headers["X-Lark-Signature"]
	if sig == "" {
		sig = headers["x-lark-signature"]
	}
	ts := headers["X-Lark-Timestamp"]
	if ts == "" {
		ts = headers["x-lark-timestamp"]
	}
	nonce := headers["X-Lark-Nonce"]
	if nonce == "" {
		nonce = headers["x-lark-nonce"]
	}
	if sig == "" || ts == "" || nonce == "" {
		return false
	}
	h := sha256.New()
	h.Write([]byte(encryptKey + ts + nonce))
	h.Write(body)
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

func verifyHMAC(secret string, body []byte, headers map[string]string, headerNames ...string) bool {
	if secret == "" {
		return false
	}
	var sig string
	for _, h := range headerNames {
		if v := headers[h]; v != "" {
			sig = v
			break
		}
	}
	if sig == "" {
		return false
	}
	sig = strings.TrimPrefix(sig, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

// tiktokSignatureHeader 是 TikTok 官方回调携带签名的请求头名。
// 注意 Go 的 textproto 规范化会把它写成 Tiktok-Signature，所以查头一律走
// headerFold，不能按字面量直取。
const tiktokSignatureHeader = "TikTok-Signature"

// verifyTiktokWebhook 按官方契约校 TikTok 事件回调。
//
// 官方原文（https://developers.tiktok.com/doc/webhooks-verification）：签名放在
// `TikTok-Signature` 头里，值是逗号分隔的 `t=<timestamp>,s=<signature>`；
// `signed_payload` = 时间戳字符串 + `.` + 请求体原始 JSON；
// 摘要 = HMAC-SHA256(client_secret, signed_payload) 的 hex。
//
// 审计 D-04 修的是「形似可用实则整条死路」：此前 tiktok 与抖音共用 verifyHMAC，
// 只签 body、不带 timestamp、也不查这个头 —— 真实回调 100% 验不过，
// 而 HTTP 层回的是普通 400，看不出是契约不符。这里把结构缺失与摘要不符分开报错，
// 缺头/缺段这类配置问题能直接在响应 reason 里暴露出来（同批A 的白名单教训：
// 控制器不转发该头时，service 层单测再绿也测不到）。
//
// 刻意不加时间戳新鲜度窗：官方只说「自行判断差值是否可接受」，未给数值（§6），
// 无依据的窗口会误杀渠道方对同一条事件的合法重投；重投由 S-04 的官方事件键兜底。
func verifyTiktokWebhook(secret string, body []byte, headers map[string]string) (bool, error) {
	sig, ts, err := parseTiktokSignature(headerFold(headers, tiktokSignatureHeader))
	if err != nil {
		return false, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1, nil
}

// parseTiktokSignature 拆 `t=<ts>,s=<sig>`。两段都必须存在且时间戳为纯数字，
// 否则无法复原被签字符串 —— 结构问题必须报错，不能静默判「签名不对」。
func parseTiktokSignature(header string) (sig, ts string, err error) {
	if header == "" {
		return "", "", fmt.Errorf("missing %s header", tiktokSignatureHeader)
	}
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "t":
			ts = strings.TrimSpace(v)
		case "s":
			sig = strings.TrimSpace(v)
		}
	}
	if ts == "" || sig == "" {
		return "", "", fmt.Errorf("%s 必须同时带 t=<timestamp> 与 s=<signature>，got %q", tiktokSignatureHeader, header)
	}
	if _, convErr := strconv.ParseInt(ts, 10, 64); convErr != nil {
		return "", "", fmt.Errorf("%s 的 t= 段不是数字时间戳: %q", tiktokSignatureHeader, ts)
	}
	return sig, ts, nil
}

// douyinSignatureHeader 是抖音开放平台回调携带签名的请求头名。
// 同 tiktok：HTTP 层会把头名规范化成 X-douyin-signature，取值必须走 headerFold。
const douyinSignatureHeader = "X-Douyin-Signature"

// verifyDouyinWebhook 按官方契约校抖音 dop 事件回调。
//
// 官方原文（developer.open-douyin.com/docs/resource/zh-CN/dop/develop/webhooks/summarize）：
// 「抖音服务端会将应用的(client secret + 消息体)使用 sha1 哈希作为 X-Douyin-Signature
// header 的 value」，并给出 go 实现 h.Write(clientSecret); h.Write(body);
// fmt.Sprintf("%x", h.Sum(nil)) —— 即 hex(sha1(client_secret ‖ 原始 body))。
//
// 这里的 sha1 不是「我们挑选的哈希」，而是渠道方规定的被签摘要；换成更"强"的算法
// 不会更安全，只会让 100% 的真实回调验不过（审计 D-04 的同一失效模式：形似可用、
// 实则整条死路）。缺头单独报错而非返回 false，配置问题才能直接出现在响应 reason 里。
func verifyDouyinWebhook(secret string, body []byte, headers map[string]string) (bool, error) {
	sig := headerFold(headers, douyinSignatureHeader)
	if sig == "" {
		return false, fmt.Errorf("missing %s header", douyinSignatureHeader)
	}
	return subtle.ConstantTimeCompare([]byte(sig), []byte(douyinSignature(secret, body))) == 1, nil
}

// douyinSignature 官方被签字符串：client_secret 直接前置拼接原始 body，无分隔符、无时间戳。
func douyinSignature(secret string, body []byte) string {
	h := sha1.New()
	_, _ = h.Write([]byte(secret))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// headerFold 在头 map 里按大小写不敏感取值：调用方可能是 HTTP 层（键为 Go 规范形
// 式 Tiktok-Signature），也可能是直接构造 map 的内部调用方（键为官方拼写）。
func headerFold(headers map[string]string, name string) string {
	if v := headers[name]; v != "" {
		return v
	}
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// 审计 D-04 对快手/小红书/闲鱼/custom 的处置：这几家**没有可引用的服务端回调
// 验签契约**（§3.8：快手无客服私信 API、小红书无公开私信 API、闲鱼无任何公开 API），
// 且自 D-03 起通用 webhook 路由已在 Receive 入口按能力表把它们拒掉，下面的
// verifyHMAC 分支从 HTTP 侧不可达（Verify 的唯一生产调用点是 Receive）。
// 因此这里保留通用 HMAC 作为「将来真的接入时的占位口径」，但不得据其声称已按
// 官方验签收口 —— 网上流传的 kwaisign=MD5(body+secret) 只找到支付/非回调出处，
// 未取到可引用原文，按 §6 记为缺口，不照抄进代码。

type ParsedPayload struct {
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	Sender    string         `json:"sender,omitempty"`
	Content   string         `json:"content,omitempty"`
	ChatID    string         `json:"chat_id,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

func (s *WebhookService) ParsePayload(ctx context.Context, channel WebhookChannel, body []byte) (*ParsedPayload, error) {
	raw, err := webhookEnvelopeMap(channel, body)
	if err != nil {
		return nil, err
	}
	p := &ParsedPayload{Extra: raw}
	p.EventID = getString(raw, "event_id", "EventID", "msg_id", "MsgId")
	p.EventType = getString(raw, "event_type", "EventType", "event", "Event", "type", "Type", "msg_type", "MsgType")
	p.Sender = getString(raw, "from_user", "FromUserName", "sender", "sender_id", "from")
	p.Content = getString(raw, "content", "text", "Text", "Content", "message")
	p.ChatID = getString(raw, "chat_id", "ChatID", "conversation_id", "to_user", "ToUserName")
	return p, nil
}

// webhookEnvelopeMap 按渠道把回调外壳解成 map。除企微外一律 JSON（现状不变）；
// 企微额外认官方 <xml> 外壳（审计 N-08）：此前这里 json.Unmarshal 失败会让 Receive
// 直接返回 "parse error"，加密回调在**验签之前**就 400，整个渠道的 XML 形态进不来。
func webhookEnvelopeMap(channel WebhookChannel, body []byte) (map[string]any, error) {
	if channel == ChannelWeCom {
		if m := wecomEnvelopeMap(body); m != nil {
			return m, nil
		}
		return nil, fmt.Errorf("wecom body is neither JSON nor <xml> envelope")
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ToUnifiedMessage 转成统一消息
func (s *WebhookService) ToUnifiedMessage(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload) *model.UnifiedMessage {
	return &model.UnifiedMessage{
		MessageID:   s.genMessageID(ctx, channel, accountID, p),
		Platform:    model.Platform(channel),
		AccountID:   accountID,
		ChatID:      p.ChatID,
		SenderID:    p.Sender,
		Content:     p.Content,
		ContentType: model.MessageTypeText,
		RawData:     "",
		Status:      model.MessageStatusPending,
	}
}

func (s *WebhookService) handleJob(ctx context.Context, job *webhookJob) {
	channel := job.channel
	if channel == "" {
		channel = WebhookChannel(job.event.Platform)
	}
	payload := job.payload
	if payload == nil {
		var err error
		payload, err = s.ParsePayload(ctx, channel, job.raw)
		if err != nil {
			logger.Errorf("[Webhook] 处理失败 event=%s: %v", job.event.EventID, err)
			return
		}
	}

	hubMsg, tgExtra, dispatchErr := s.dispatchToChannel(ctx, channel, job.account, payload, job.raw, job.header)
	if errors.Is(dispatchErr, ErrWebhookInboundNotWired) {
		// 只有改造前入库的旧事件行会走到这里（Receive 已按能力表拒绝新请求）。
		// 不能再往下写 unified_messages：那等于把一条没人能处理的消息记成"已收到"，
		// 收件箱看得到、永远没人回复；就地标记处理完，让恢复扫描器不再重投。
		s.markProcessed(ctx, job.event)
		return
	}
	if dispatchErr != nil {
		logger.Errorf("[Webhook] dispatch %s failed event=%s: %v", channel, job.event.EventID, dispatchErr)
	}

	if hubMsg == nil && dispatchErr == nil {
		// 有适配器的渠道这次没产出 hub 行 = 非消息类事件（授权/关注/撤回…），确认跳过。
		// 判定改用能力表：原先写死 5 个渠道，抖系加了解析器却没进清单，
		// 无 sender 的事件会继续往下走、落一条空内容的 unified_message（审计 D-03）。
		logger.Infof("[Webhook] skip non-message event channel=%s event=%s", channel, job.event.EventID)
		s.markProcessed(ctx, job.event)
		return
	}

	um := s.ToUnifiedMessage(ctx, channel, job.account, payload)
	if um.AccountID == "" {
		um.AccountID = job.event.EventID
	}
	if err := s.dispatchToUnified(ctx, um); err != nil {
		s.retryWithBackoff(ctx, job, payload, err)
		return
	}

	triggerAI := hubMsg != nil && s.shouldTriggerAI(ctx, channel, job.account)
	// QQ 渠道 AI 触发已由 dispatchQQ → Ingress（aiTrigger=webhookSvc.TriggerInboundAI）
	// 完成，这里不再走 triggerSalesEngine，避免同一事件双触发 AI（双重回复）。
	if channel == ChannelTelegram && tgExtra != nil && tgExtra.GateHandled {
		triggerAI = false // /start 网关验证已消费
	}
	if triggerAI && channel != ChannelQQ {
		// Telegram 群消息：先把 @mention/商机 元信息塞进 ctx → 让 sendOutbound 能 @mention 原发言人
		aiCtx := ctx
		if channel == ChannelTelegram && hubMsg.IsGroup && tgExtra != nil {
			aiCtx = TelegramReplyMetaToContext(ctx, &TelegramReplyMeta{
				FromUsername:  tgExtra.FromUsername,
				FromName:      tgExtra.FromName,
				FromUserID:    tgExtra.FromUserID,
				ReplyToMsgID:  tgExtra.ReplyToMsgID,
				TriggerReason: tgExtra.TriggerReason,
			})
		}

		if channel != ChannelTelegram || !hubMsg.IsGroup {
			s.triggerSalesEngine(aiCtx, channel, job.account, payload, hubMsg)
		} else {
			mentioned := tgExtra != nil && tgExtra.Mentioned
			newOpp := tgExtra != nil && tgExtra.NewOpportunity
			switch {
			case mentioned:
				s.triggerSalesEngine(aiCtx, channel, job.account, payload, hubMsg)
			case newOpp && s.tgLeadOutreachAllowed(ctx, job.account, payload.ChatID, payload.Sender):
				s.triggerSalesEngine(aiCtx, channel, job.account, payload, hubMsg)
			}
		}
	}

	s.markProcessed(ctx, job.event)
}

func (s *WebhookService) dispatchToChannel(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, raw []byte, headers map[string]string) (*model.MessageHub, *tgDispatchExtra, error) {
	switch channel {
	case ChannelWeCom:
		hub, err := s.dispatchWeCom(ctx, accountID, p, raw, headers)
		return hub, nil, err
	case ChannelWhatsapp:
		hub, err := s.dispatchWhatsApp(ctx, accountID, p, raw)
		return hub, nil, err
	case ChannelTelegram:
		return s.dispatchTelegram(ctx, accountID, p, raw)
	case ChannelQQ:
		hub, err := s.dispatchQQ(ctx, accountID, p, raw)
		return hub, nil, err
	case ChannelFeishu:
		hub, err := s.dispatchFeishu(ctx, accountID, p, raw)
		return hub, nil, err
	case ChannelDouyin, ChannelTiktok:
		return s.dispatchDouyin(ctx, channel, accountID, p, raw)
	default:
		// 原先这里是静默 return nil, nil, nil：事件已被 Receive 收下并回 200，
		// 却没有任何适配器处理它（审计 D-03）。能力表漏配必须报出来，不能靠人翻日志。
		logger.Errorf("[Webhook] 渠道 %s 无入站适配器，事件未处理 account=%s：能力表 webhookInboundCapable 与 dispatchToChannel 分支已漂移",
			channel, accountID)
		return nil, nil, fmt.Errorf("%w: %s", ErrWebhookInboundNotWired, channel)
	}
}
