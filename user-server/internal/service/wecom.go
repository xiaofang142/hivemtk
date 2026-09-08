package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/repository"
	"time"

	"context"

	"gorm.io/gorm"
)

var wecomTokenLocks sync.Map

func wecomTokenLock(accountID uint) *sync.Mutex {
	v, _ := wecomTokenLocks.LoadOrStore(accountID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type WeComService struct {
	accountRepo  *repository.WeComAccountRepository
	customerRepo *repository.WeComCustomerRepository
	groupRepo    *repository.WeComGroupRepository
	memberRepo   *repository.WeComGroupMemberRepository
	messageRepo  *repository.WeComMessageRepository
	tagRepo      *repository.WeComTagRepository
}

// NewWeComService 创建企业微信服务实例(无参,内部用 dbUtil.GetDB())
func NewWeComService() *WeComService {
	return NewWeComServiceWithDB(dbUtil.GetDB())
}

// NewWeComServiceWithDB 创建带 DB 的企业微信服务实例(显式注入 db,兼容旧调用)
func NewWeComServiceWithDB(db *gorm.DB) *WeComService {
	return &WeComService{
		accountRepo:  repository.NewWeComAccountRepository(),
		customerRepo: repository.NewWeComCustomerRepository(),
		groupRepo:    repository.NewWeComGroupRepository(),
		memberRepo:   repository.NewWeComGroupMemberRepository(),
		messageRepo:  repository.NewWeComMessageRepository(),
		tagRepo:      repository.NewWeComTagRepository(db),
	}
}

// WeComTokenResponse 获取访问令牌响应
type WeComTokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (s *WeComService) GetAccessToken(ctx context.Context, account *model.WeComAccount) (string, error) {
	if account == nil {
		return "", errors.New("账户不能为空")
	}

	if account.AccessToken != "" && time.Now().Before(account.TokenExpires) {
		return account.AccessToken, nil
	}

	lock := wecomTokenLock(account.ID)
	lock.Lock()
	defer lock.Unlock()

	if account.AccessToken != "" && time.Now().Before(account.TokenExpires) {
		return account.AccessToken, nil
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		account.CorpID, account.CorpSecret)

	resp, err := httpclient.Client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp WeComTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	if tokenResp.ErrCode != 0 {
		return "", errors.New(tokenResp.ErrMsg)
	}

	expiresTime := time.Now().Add(time.Duration(tokenResp.ExpiresIn-600) * time.Second)
	s.accountRepo.UpdateToken(ctx, account.ID, tokenResp.AccessToken, expiresTime)

	return tokenResp.AccessToken, nil
}

// CreateAccountRequest 创建账号请求
type CreateAccountRequest struct {
	CorpID         string `json:"corp_id" binding:"required"`
	// CorpSecret 更新时允许留空=保留原值（与 UpdateAccount 的密钥保留语义配套）；创建由 handler 单独校验
	CorpSecret     string `json:"corp_secret"`
	AgentID        int    `json:"agent_id"`
	AgentSecret    string `json:"agent_secret"`
	CallbackToken  string `json:"callback_token"`
	EncodingAESKey string `json:"encoding_aes_key"`
	WebhookEnabled bool   `json:"webhook_enabled"`
	AIAgentEnabled bool   `json:"ai_agent_enabled"`
	WebhookPath    string `json:"webhook_path"`
}

// CreateAccount 创建企业微信账号
func (s *WeComService) CreateAccount(ctx context.Context, req *CreateAccountRequest) (*model.WeComAccount, error) {
	if req.CorpSecret == "" {
		return nil, fmt.Errorf("corp_secret 不能为空")
	}
	account := &model.WeComAccount{
		CorpID:         req.CorpID,
		CorpSecret:     req.CorpSecret,
		AgentID:        req.AgentID,
		AgentSecret:    req.AgentSecret,
		CallbackToken:  req.CallbackToken,
		EncodingAESKey: req.EncodingAESKey,
		WebhookEnabled: req.WebhookEnabled,
		AIAgentEnabled: req.AIAgentEnabled,
		WebhookPath:    req.WebhookPath,
		Status:         1,
	}

	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, err
	}

	return account, nil
}

// GetAccountList 获取账号列表
func (s *WeComService) GetAccountList(ctx context.Context) ([]*model.WeComAccount, error) {
	return s.accountRepo.GetByMerchant(ctx)
}

// GetAccountByID 获取账号详情
func (s *WeComService) GetAccountByID(ctx context.Context, id uint) (*model.WeComAccount, error) {
	return s.accountRepo.GetByID(ctx, id)
}

// UpdateAccount 更新账号
func (s *WeComService) UpdateAccount(ctx context.Context, id uint, req *CreateAccountRequest) (*model.WeComAccount, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	account.CorpID = req.CorpID
	account.AgentID = req.AgentID
	// 密钥类字段空串=保留原值（前端虽然从列表回填明文，但密钥不该被意外清空）
	if req.CorpSecret != "" {
		account.CorpSecret = req.CorpSecret
	}
	if req.AgentSecret != "" {
		account.AgentSecret = req.AgentSecret
	}
	if req.CallbackToken != "" {
		account.CallbackToken = req.CallbackToken
	}
	if req.EncodingAESKey != "" {
		account.EncodingAESKey = req.EncodingAESKey
	}
	account.WebhookEnabled = req.WebhookEnabled
	account.AIAgentEnabled = req.AIAgentEnabled
	if req.WebhookPath != "" {
		account.WebhookPath = req.WebhookPath
	}

	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}

	return account, nil
}

// DeleteAccount 删除账号
func (s *WeComService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accountRepo.Delete(ctx, id)
}

// SyncCustomers 同步客户
func (s *WeComService) SyncCustomers(ctx context.Context, account *model.WeComAccount) (int, error) {
	token, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/externalcontact/list?access_token=%s", token)
	resp, err := httpclient.Client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		ErrCode        int      `json:"errcode"`
		ErrMsg         string   `json:"errmsg"`
		ExternalUserID []string `json:"external_userid_list"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.ErrCode != 0 {
		return 0, errors.New(result.ErrMsg)
	}

	count := 0
	for _, userID := range result.ExternalUserID {
		customer, err := s.getCustomerDetail(ctx, token, userID)
		if err != nil {
			continue
		}
		customer.AccountID = account.ID

		existing, _ := s.customerRepo.GetByExternalUserID(ctx, userID)
		if existing != nil {
			customer.ID = existing.ID
			s.customerRepo.Update(ctx, customer)
		} else {
			s.customerRepo.Create(ctx, customer)
		}
		count++
	}

	s.accountRepo.UpdateSyncTime(ctx, account.ID)
	return count, nil
}

func (s *WeComService) getCustomerDetail(ctx context.Context, token, userID string) (*model.WeComCustomer, error) {
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/externalcontact/get?access_token=%s&external_userid=%s", token, userID)
	resp, err := httpclient.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		ErrCode         int    `json:"errcode"`
		ErrMsg          string `json:"errmsg"`
		ExternalContact struct {
			ExternalUserID string `json:"external_userid"`
			Name           string `json:"name"`
			Avatar         string `json:"avatar"`
			Gender         int    `json:"gender"`
			UnionID        string `json:"union"`
			Type           int    `json:"type"`
		} `json:"external_contact"`
		FollowUser []struct {
			UserID   string `json:"userid"`
			Nickname string `json:"nickname"`
			AddTime  int64  `json:"add_time"`
		} `json:"follow_user"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	customer := &model.WeComCustomer{
		ExternalUserID: result.ExternalContact.ExternalUserID,
		Name:           result.ExternalContact.Name,
		Avatar:         result.ExternalContact.Avatar,
		Gender:         result.ExternalContact.Gender,
		UnionID:        result.ExternalContact.UnionID,
		Type:           result.ExternalContact.Type,
	}

	if len(result.FollowUser) > 0 {
		customer.EmployeeID = result.FollowUser[0].UserID
		customer.EmployeeName = result.FollowUser[0].Nickname
		customer.AddTime = time.Unix(result.FollowUser[0].AddTime, 0)
	}

	return customer, nil
}

// GetCustomerList 获取客户列表
func (s *WeComService) GetCustomerList(ctx context.Context, page, pageSize int) ([]*model.WeComCustomer, int64, error) {
	return s.customerRepo.GetByMerchant(ctx, page, pageSize)
}

// SyncGroups 同步客户群
func (s *WeComService) SyncGroups(ctx context.Context, account *model.WeComAccount) (int, error) {
	token, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/externalcontact/groupchat/list?access_token=%s", token)

	reqBody := map[string]any{
		"status_filter": 0,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := httpclient.Client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		ErrCode       int    `json:"errcode"`
		ErrMsg        string `json:"errmsg"`
		GroupChatList []struct {
			ChatID string `json:"chat_id"`
			Status int    `json:"status"`
		} `json:"group_chat_list"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.ErrCode != 0 {
		return 0, errors.New(result.ErrMsg)
	}

	count := 0
	for _, g := range result.GroupChatList {
		group, err := s.getGroupDetail(ctx, token, g.ChatID)
		if err != nil {
			continue
		}
		group.AccountID = account.ID

		existing, _ := s.groupRepo.GetByChatID(ctx, g.ChatID)
		if existing != nil {
			group.ID = existing.ID
			s.groupRepo.Update(ctx, group)
		} else {
			s.groupRepo.Create(ctx, group)
		}
		count++
	}

	return count, nil
}

func (s *WeComService) getGroupDetail(ctx context.Context, token, chatID string) (*model.WeComGroup, error) {
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/externalcontact/groupchat/get?access_token=%s", token)

	reqBody := map[string]any{
		"chat_id": chatID,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := httpclient.Client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		ErrCode   int    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		GroupChat struct {
			ChatID      string `json:"chat_id"`
			Name        string `json:"name"`
			Owner       string `json:"owner"`
			MemberCount int    `json:"member_count"`
			CreateTime  int64  `json:"create_time"`
		} `json:"group_chat"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	group := &model.WeComGroup{
		ChatID:      result.GroupChat.ChatID,
		Name:        result.GroupChat.Name,
		OwnerID:     result.GroupChat.Owner,
		MemberCount: result.GroupChat.MemberCount,
		CreateTime:  time.Unix(result.GroupChat.CreateTime, 0),
		Status:      1,
	}

	return group, nil
}

// GetGroupList 获取群列表
func (s *WeComService) GetGroupList(ctx context.Context, page, pageSize int) ([]*model.WeComGroup, int64, error) {
	return s.groupRepo.GetByMerchant(ctx, page, pageSize)
}

// WeComSendMessageRequest 发送消息请求
type WeComSendMessageRequest struct {
	ToUser  string `json:"to_user"`
	ToParty string `json:"to_party"`
	MsgType string `json:"msg_type" binding:"required"`
	Content string `json:"content"`
	MediaID string `json:"media_id"`
	Title   string `json:"title"`
	Desc    string `json:"desc"`
	URL     string `json:"url"`
	PicURL  string `json:"pic_url"`
}

// SendMessage 发送消息
func (s *WeComService) SendMessage(ctx context.Context, account *model.WeComAccount, req *WeComSendMessageRequest) (string, error) {
	token, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/externalcontact/message/send?access_token=%s", token)

	externalUserIDs := []string{}
	if v := strings.TrimSpace(req.ToUser); v != "" {
		for _, id := range strings.Split(v, "|") {
			if t := strings.TrimSpace(id); t != "" {
				externalUserIDs = append(externalUserIDs, t)
			}
		}
	}

	msgData := map[string]any{
		"external_userid": externalUserIDs,
		"msgtype":         req.MsgType,
		"agentid":         account.AgentID,
	}

	switch req.MsgType {
	case "text":
		msgData["text"] = map[string]string{"content": req.Content}
	case "markdown":
		msgData["markdown"] = map[string]string{"content": req.Content}
	case "textcard":
		// textcard 约定：Content=描述，Title=标题，URL=跳转链接，PicURL=配图
		msgData["textcard"] = map[string]any{
			"title":       req.Title,
			"description": req.Content,
			"url":         req.URL,
			"btntxt":      "查看详情",
		}
		if req.PicURL != "" {
			msgData["textcard"].(map[string]any)["picurl"] = req.PicURL
		}
	case "image":
		msgData["image"] = map[string]string{"media_id": req.MediaID}
	case "link":
		msgData["link"] = map[string]string{
			"title":  req.Title,
			"desc":   req.Desc,
			"url":    req.URL,
			"picurl": req.PicURL,
		}
	}

	bodyBytes, _ := json.Marshal(msgData)
	resp, err := httpclient.Client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MsgID   string `json:"msgid"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.ErrCode != 0 {
		return "", errors.New(result.ErrMsg)
	}

	now := time.Now()
	message := &model.WeComMessage{
		AccountID: account.ID,
		MsgID:     result.MsgID,
		MsgType:   req.MsgType,
		ToUser:    req.ToUser,
		Content:   req.Content,
		Status:    1,
		SendTime:  &now,
	}
	s.messageRepo.Create(ctx, message)

	return result.MsgID, nil
}

// GetMessageList 获取消息列表
func (s *WeComService) GetMessageList(ctx context.Context, page, pageSize int) ([]*model.WeComMessage, int64, error) {
	return s.messageRepo.GetByMerchant(ctx, page, pageSize)
}

// GetTagList 获取标签列表
func (s *WeComService) GetTagList(ctx context.Context) ([]*model.WeComTag, error) {
	return s.tagRepo.GetByMerchant(ctx)
}

// SyncTags 同步标签
func (s *WeComService) SyncTags(ctx context.Context, account *model.WeComAccount) (int, error) {
	token, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/tag/list?access_token=%s", token)
	resp, err := httpclient.Client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		TagList []struct {
			TagID     int    `json:"tagid"`
			TagName   string `json:"tagname"`
			UserCount int    `json:"user_count"`
		} `json:"taglist"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.ErrCode != 0 {
		return 0, errors.New(result.ErrMsg)
	}

	count := 0
	for _, t := range result.TagList {
		tag := &model.WeComTag{
			AccountID:     account.ID,
			TagID:         fmt.Sprintf("%d", t.TagID),
			TagName:       t.TagName,
			CustomerCount: t.UserCount,
		}
		s.tagRepo.Create(ctx, tag)
		count++
	}

	return count, nil
}
