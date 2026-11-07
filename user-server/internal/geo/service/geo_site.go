package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
)

// geoSiteRootDir 返回静态站允许写入的根目录。
// 默认落在系统临时目录，生产通过 GEO_SITE_ROOT 指向持久卷。
func geoSiteRootDir() string {
	if v := strings.TrimSpace(os.Getenv("GEO_SITE_ROOT")); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), "hivemtk-geo-sites")
}

// validateHugoPath 把 hugo_path 限制在 GEO_SITE_ROOT/<tenant_id>/ 之下，
// 防止租户把导出/部署目录指到系统目录或其他租户目录。
// 返回清理后的绝对路径；目录不存在也允许（导出时会创建）。
// validateGitRepoURL 只接受 git 远端地址（https:// 或 ssh 形态），
// 拒绝 file:// / 本地路径等会让 git 推到服务器任意目录的形态。
func validateGitRepoURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if strings.HasPrefix(v, "https://") {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%w: Git 仓库地址非法", ErrSiteConfigInvalid)
		}
		return v, nil
	}
	// git@host:path 或 ssh://git@host/path
	if strings.HasPrefix(v, "git@") || strings.HasPrefix(v, "ssh://") {
		return v, nil
	}
	return "", fmt.Errorf("%w: Git 仓库仅支持 https:// 或 git@/ssh:// 形态", ErrSiteConfigInvalid)
}

func validateHugoPath(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return "", fmt.Errorf("%w: 无法确定当前租户", ErrSiteConfigInvalid)
	}
	root, err := filepath.Abs(geoSiteRootDir())
	if err != nil {
		return "", fmt.Errorf("%w: 站点根目录无效", ErrSiteConfigInvalid)
	}
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = resolved
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("%w: 站点目录无效", ErrSiteConfigInvalid)
	}
	// 路径已存在时解析真实路径；不存在时解析父目录，防止通过软链接越界。
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = resolved
	} else if parent, evalErr := filepath.EvalSymlinks(filepath.Dir(abs)); evalErr == nil {
		abs = filepath.Join(parent, filepath.Base(abs))
	}
	allowed := filepath.Join(root, fmt.Sprintf("%d", tenantID))
	rel, err := filepath.Rel(allowed, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: 站点目录必须位于 GEO_SITE_ROOT 的当前租户子目录（%s）之下，当前为 %s", ErrSiteConfigInvalid, allowed, raw)
	}
	return abs, nil
}

// SiteConfigInput 站点配置的可编辑字段。
//
// 指针区分「未传」与「传了零值」（Update 只覆盖传了的字段）；
// id/tenant_id/health_score/last_deploy_at 由系统维护，不接受客户端输入。
type SiteConfigInput struct {
	Domain    *string
	Provider  *string
	HugoPath  *string
	GitRepo   *string
	DeployCmd *string
	Active    *bool
}

// SiteConfigService 站点部署配置读写编排（配置 CRUD；导出/部署见 SiteDeployService）
type SiteConfigService struct {
	repo repository.GeoSiteRepository
}

func NewSiteConfigService(repo repository.GeoSiteRepository) *SiteConfigService {
	return &SiteConfigService{repo: repo}
}

// List 分页查询；limit 上限 200。
func (s *SiteConfigService) List(ctx context.Context, page, limit int) ([]*model.GeoSite, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	return s.repo.List(ctx, page, limit)
}

func (s *SiteConfigService) Get(ctx context.Context, id string) (*model.GeoSite, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *SiteConfigService) Create(ctx context.Context, in SiteConfigInput) (*model.GeoSite, error) {
	domain, err := requiredTrimmed(in.Domain, "域名")
	if err != nil {
		return nil, err
	}
	site := &model.GeoSite{
		Domain:   domain,
		Provider: "cloudflare",
		Active:   true,
	}
	if in.Provider != nil && strings.TrimSpace(*in.Provider) != "" {
		site.Provider = strings.TrimSpace(*in.Provider)
	}
	if in.HugoPath != nil {
		hugoPath, err := validateHugoPath(ctx, *in.HugoPath)
		if err != nil {
			return nil, err
		}
		site.HugoPath = hugoPath
	}
	if in.GitRepo != nil {
		repo, err := validateGitRepoURL(*in.GitRepo)
		if err != nil {
			return nil, err
		}
		site.GitRepo = repo
	}
	if in.DeployCmd != nil {
		cmd := strings.TrimSpace(*in.DeployCmd)
		if cmd != "" {
			return nil, fmt.Errorf("%w: 部署命令已停用：服务器不会执行租户自定义命令（RCE 风险）。推送由 Git 仓库字段驱动，构建由托管平台（GitHub Actions/Cloudflare Pages）完成", ErrSiteConfigInvalid)
		}
		site.DeployCmd = ""
	}
	if in.Active != nil {
		site.Active = *in.Active
	}
	if err := s.repo.Create(ctx, site); err != nil {
		return nil, err
	}
	return site, nil
}

func (s *SiteConfigService) Update(ctx context.Context, id string, in SiteConfigInput) (*model.GeoSite, error) {
	site, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Domain != nil {
		domain, err := requiredTrimmed(in.Domain, "域名")
		if err != nil {
			return nil, err
		}
		site.Domain = domain
	}
	if in.Provider != nil {
		v := strings.TrimSpace(*in.Provider)
		if v == "" {
			return nil, errors.New("provider 不能为空")
		}
		site.Provider = v
	}
	if in.HugoPath != nil {
		hugoPath, err := validateHugoPath(ctx, *in.HugoPath)
		if err != nil {
			return nil, err
		}
		site.HugoPath = hugoPath
	}
	if in.GitRepo != nil {
		repo, err := validateGitRepoURL(*in.GitRepo)
		if err != nil {
			return nil, err
		}
		site.GitRepo = repo
	}
	if in.DeployCmd != nil {
		cmd := strings.TrimSpace(*in.DeployCmd)
		if cmd != "" {
			return nil, fmt.Errorf("%w: 部署命令已停用：服务器不会执行租户自定义命令（RCE 风险）。推送由 Git 仓库字段驱动，构建由托管平台（GitHub Actions/Cloudflare Pages）完成", ErrSiteConfigInvalid)
		}
		site.DeployCmd = ""
	}
	if in.Active != nil {
		site.Active = *in.Active
	}
	if err := s.repo.Update(ctx, site); err != nil {
		return nil, err
	}
	site.UpdatedAt = time.Now()
	return site, nil
}

func (s *SiteConfigService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
