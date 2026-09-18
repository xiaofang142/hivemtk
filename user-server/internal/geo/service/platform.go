package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/crypto"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// 发布状态常量
const (
	PublishStatusPending = "pending"
	PublishStatusCopied  = "copied"
	PublishStatusFailed  = "failed"
)

type platformDef struct {
	Name        string
	DisplayName string
	URL         string
	Path        string
	Branch      string
	AuthType    string
	Enabled     bool

	Capability string
}

func defaultPlatforms() []platformDef {
	return []platformDef{
		{Name: "github_readme", DisplayName: "GitHub README", URL: "https://github.com", Path: "README.md", Branch: "master", AuthType: "token", Enabled: true, Capability: "real_api"},
		{Name: "github_blog", DisplayName: "GitHub Blog", URL: "https://github.com", Path: "blog/", Branch: "master", AuthType: "token", Enabled: true, Capability: "real_api"},
		{Name: "medium", DisplayName: "Medium", URL: "https://medium.com", AuthType: "oauth", Enabled: true, Capability: "real_api"},
		{Name: "wordpress", DisplayName: "WordPress", AuthType: "xmlrpc", Enabled: true, Capability: "real_api"},
		{Name: "custom", DisplayName: "自定义平台", AuthType: "custom", Enabled: false, Capability: "stub"},
	}
}

func findPlatform(name string) *platformDef {
	defs := defaultPlatforms()
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

// PlatformService GEO 平台同步发布服务（迁移自 AIGEOTOOLS platform/service.go）
type PlatformService struct {
	accountRepo repository.GeoPlatformAccountRepository
	recordRepo  repository.GeoPublishRecordRepository
	articleRepo repository.GeoArticleRepository
	httpClient  *http.Client
}

// NewPlatformService 创建平台同步发布服务
func NewPlatformService(
	ar repository.GeoPlatformAccountRepository,
	rr repository.GeoPublishRecordRepository,
	gr repository.GeoArticleRepository,
) *PlatformService {
	return &PlatformService{
		accountRepo: ar,
		recordRepo:  rr,
		articleRepo: gr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ListPlatforms 获取支持的平台列表 + 真实能力标记 + DB 账号状态
func (s *PlatformService) ListPlatforms(ctx context.Context) []dto.PlatformInfo {
	defs := defaultPlatforms()

	activeAccounts, _, err := s.accountRepo.GetList("", 1, 100)
	hasAccount := make(map[string]bool)
	if err == nil {
		for _, a := range activeAccounts {
			if a.Status == "active" {
				hasAccount[a.Platform] = true
			}
		}
	}

	result := make([]dto.PlatformInfo, 0, len(defs))
	for _, p := range defs {

		enabled := p.Enabled
		if p.Capability == "cookie_gray" {
			enabled = hasAccount[p.Name]
		}
		result = append(result, dto.PlatformInfo{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			URL:         p.URL,
			AuthType:    p.AuthType,
			Enabled:     enabled,
			Capability:  p.Capability,
			HasAccount:  hasAccount[p.Name],
		})
	}
	return result
}

func (s *PlatformService) validateAccount(req *dto.SavePlatformAccountRequest) error {
	switch req.Platform {
	case "github_readme", "github_blog":
		if (req.Credentials == nil || (req.Credentials["api_key"] == "" && req.Credentials["access_token"] == "")) &&
			os.Getenv("GEO_GITHUB_TOKEN") == "" {
			return fmt.Errorf("GitHub 平台需要提供 access_token 或配置 GEO_GITHUB_TOKEN")
		}
	case "wordpress":
		if req.Credentials == nil || req.Credentials["api_key"] == "" {
			return fmt.Errorf("WordPress 需要提供 api_key")
		}
	}
	return nil
}

// SaveAccount 新增/更新平台账号（同平台同名账号覆盖更新）
func (s *PlatformService) SaveAccount(ctx context.Context, req *dto.SavePlatformAccountRequest) (*dto.PlatformAccountResponse, error) {
	if err := s.validateAccount(req); err != nil {
		return nil, err
	}

	status := req.Status
	if status == "" {
		status = "active"
	}

	configJSON := ""
	if len(req.Credentials) > 0 {
		b, err := json.Marshal(req.Credentials)
		if err != nil {
			return nil, fmt.Errorf("序列化凭证失败: %w", err)
		}

		configJSON = encryptCredentials(string(b))
	}

	existing, err := s.accountRepo.GetByPlatformAndName(req.Platform, req.AccountName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		existing.Status = status
		if configJSON != "" {
			existing.Config = configJSON
		}
		if req.AccountID != "" {
			existing.AccountID = req.AccountID
		}
		if err := s.accountRepo.Update(existing); err != nil {
			return nil, err
		}
		return toAccountResponse(existing), nil
	}

	account := &model.GeoPlatformAccount{
		Platform:    req.Platform,
		AccountID:   req.AccountID,
		AccountName: req.AccountName,
		Status:      status,
		Config:      configJSON,
	}
	if err := s.accountRepo.Create(account); err != nil {
		return nil, err
	}
	return toAccountResponse(account), nil
}

func encryptCredentials(plain string) string {
	if plain == "" {
		return ""
	}
	enc, err := crypto.Encrypt(plain)
	if err != nil {
		logger.Errorf("平台凭据加密失败（检查 FIELD_ENCRYPTION_KEY，需≥32字节），降级为明文存储: %v", err)
		return plain
	}
	return enc
}

func decryptCredentials(stored string) string {
	if stored == "" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(stored), "{") {
		return stored
	}
	plain, err := crypto.Decrypt(stored)
	if err != nil {
		logger.Errorf("平台凭据解密失败（检查 FIELD_ENCRYPTION_KEY 是否与加密时一致），按未配置凭据处理: %v", err)
		return ""
	}
	return plain
}

func (s *PlatformService) latestAccountCredentials(platforms ...string) map[string]string {
	for _, p := range platforms {
		acc, err := s.accountRepo.GetLatestByPlatform(p)
		if err != nil || acc == nil || acc.Config == "" {
			continue
		}
		var creds map[string]string
		if err := json.Unmarshal([]byte(decryptCredentials(acc.Config)), &creds); err != nil {
			continue
		}
		if len(creds) > 0 {
			return creds
		}
	}
	return nil
}

func toAccountResponse(acc *model.GeoPlatformAccount) *dto.PlatformAccountResponse {
	return &dto.PlatformAccountResponse{
		ID:             acc.ID,
		Platform:       acc.Platform,
		AccountID:      acc.AccountID,
		AccountName:    acc.AccountName,
		Status:         acc.Status,
		HasCredentials: acc.Config != "",
		CreatedAt:      acc.CreatedAt,
		UpdatedAt:      acc.UpdatedAt,
	}
}

// ListAccounts 平台账号列表（分页，返回脱敏 DTO）
func (s *PlatformService) ListAccounts(ctx context.Context, platform string, page, limit int) ([]*dto.PlatformAccountResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	accounts, total, err := s.accountRepo.GetList(platform, page, limit)
	if err != nil {
		return nil, 0, err
	}
	list := make([]*dto.PlatformAccountResponse, 0, len(accounts))
	for _, acc := range accounts {
		list = append(list, toAccountResponse(acc))
	}
	return list, total, nil
}

// DeleteAccount 删除平台账号
func (s *PlatformService) DeleteAccount(ctx context.Context, id string) error {
	return s.accountRepo.Delete(id)
}

// Publish 发布文章到平台（迁移自 Publish + publishGitHub）
func (s *PlatformService) Publish(ctx context.Context, req *dto.PublishRequest) (*dto.PublishResponse, error) {

	article, err := s.articleRepo.GetByID(req.ArticleID)
	if err != nil {
		return nil, fmt.Errorf("文章不存在: %w", err)
	}

	def := findPlatform(req.Platform)

	path := req.Path
	if path == "" && def != nil {
		path = def.Path
	}
	if path == "" {
		path = req.Filename
	}
	if path == "" {
		path = "README.md"
	}
	branch := req.Branch
	if branch == "" && def != nil {
		branch = def.Branch
	}
	if branch == "" {
		branch = "main"
	}
	commitMsg := req.CommitMsg
	if commitMsg == "" {
		commitMsg = "update " + path
	}

	switch req.Platform {
	case "github_readme", "github_blog", "github":
		return s.publishGitHub(ctx, article, req, path, branch, commitMsg)
	}

	publishID := fmt.Sprintf("%s_%d", req.Platform, time.Now().UnixMilli())
	record := &model.GeoPublishRecord{
		ArticleID: article.ID,
		Platform:  req.Platform,
		Status:    PublishStatusPending,
	}
	if err := s.recordRepo.Create(record); err != nil {
		return nil, err
	}
	url := ""
	if def != nil {
		url = def.URL
	}
	return &dto.PublishResponse{
		ID:           record.ID,
		Status:       PublishStatusPending,
		PublishedURL: url,
		Message:      publishID + ": 请手动粘贴内容到目标平台",
	}, nil
}

func (s *PlatformService) publishGitHub(
	ctx context.Context,
	article *model.GeoArticle,
	req *dto.PublishRequest,
	path, branch, commitMsg string,
) (*dto.PublishResponse, error) {

	credPlatforms := []string{req.Platform}
	if req.Platform == "github" {
		credPlatforms = []string{"github_readme", "github_blog"}
	}
	token := ""
	if creds := s.latestAccountCredentials(credPlatforms...); creds != nil {
		token = creds["access_token"]
		if token == "" {
			token = creds["api_key"]
		}
	}
	if token == "" {
		token = os.Getenv("GEO_GITHUB_TOKEN")
	}
	if token == "" {
		token = req.AuthToken
	}
	user := os.Getenv("GEO_GITHUB_USER")
	repo := req.Repo
	if token == "" || user == "" {
		return nil, fmt.Errorf("未配置 GitHub 凭证：请先保存平台账号（含 access_token/api_key），或配置 GEO_GITHUB_TOKEN/GEO_GITHUB_USER 环境变量")
	}
	if repo == "" {
		return nil, fmt.Errorf("缺少 repo 参数")
	}

	var safeRepoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	if !safeRepoRe.MatchString(repo) || strings.Contains(repo, "..") {
		return nil, fmt.Errorf("repo 参数非法: %q", repo)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return nil, fmt.Errorf("path 参数非法: %q", path)
		}
		if !safeRepoRe.MatchString(seg) {
			return nil, fmt.Errorf("path 段非法: %q", seg)
		}
	}
	apiBase := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s",
		url.PathEscape(user), url.PathEscape(repo), url.PathEscape(path))

	var sha string
	if getReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase, nil); getReq != nil {
		getReq.Header.Set("Authorization", "Bearer "+token)
		getReq.Header.Set("Accept", "application/vnd.github+json")
		resp, err := s.httpClient.Do(getReq)
		if err != nil {

			logger.Errorf("查询 GitHub 文件元数据失败 path=%s（按新文件继续发布）: %v", path, err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var meta struct {
					SHA string `json:"sha"`
				}
				if json.Unmarshal(body, &meta) == nil {
					sha = meta.SHA
				}
			}
		}
	}

	payload := map[string]any{
		"message": commitMsg,
		"content": base64.StdEncoding.EncodeToString([]byte(article.Content)),
		"branch":  branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	body, _ := json.Marshal(payload)
	putReq, _ := http.NewRequestWithContext(ctx, http.MethodPut, apiBase, bytes.NewReader(body))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Accept", "application/vnd.github+json")
	putReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(putReq)
	if err != nil {
		s.saveFailedRecord(article.ID, req.Platform, err.Error())
		return nil, fmt.Errorf("GitHub 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(respBody))
		s.saveFailedRecord(article.ID, req.Platform, msg)
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, msg)
	}

	var r struct {
		Content struct {
			HTMLURL string `json:"html_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &r); err != nil {
		s.saveFailedRecord(article.ID, req.Platform, "解析 GitHub 响应失败: "+err.Error())
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}

	record := &model.GeoPublishRecord{
		ArticleID:    article.ID,
		Platform:     req.Platform,
		Status:       PublishStatusCopied,
		PublishedURL: r.Content.HTMLURL,
		PublishedAt:  time.Now(),
	}
	if err := s.recordRepo.Create(record); err != nil {
		return nil, err
	}
	return &dto.PublishResponse{
		ID:           record.ID,
		Status:       PublishStatusCopied,
		PublishedURL: r.Content.HTMLURL,
	}, nil
}

func (s *PlatformService) saveFailedRecord(articleID, platform, errMsg string) {
	_ = s.recordRepo.Create(&model.GeoPublishRecord{
		ArticleID: articleID,
		Platform:  platform,
		Status:    PublishStatusFailed,
	})
}

// ListPublishRecords 发布记录列表（分页）
func (s *PlatformService) ListPublishRecords(ctx context.Context, articleID, platform string, page, limit int) ([]*model.GeoPublishRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.recordRepo.GetList(articleID, platform, page, limit)
}
