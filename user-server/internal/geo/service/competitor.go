package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

type GeoCompetitorService struct {
	repo repository.GeoCompetitorRepository
}

func NewGeoCompetitorService(r repository.GeoCompetitorRepository) *GeoCompetitorService {
	return &GeoCompetitorService{repo: r}
}

type CompetitorDTO struct {
	ID       uint     `json:"id,omitempty"`
	Name     string   `json:"name"`
	Domain   string   `json:"domain"`
	Paths    []string `json:"paths"`
	Category string   `json:"category"`
	Priority int      `json:"priority"`
	Status   string   `json:"status"`
	Notes    string   `json:"notes"`
}

func (s *GeoCompetitorService) ListCompetitors(ctx context.Context, status string) ([]CompetitorDTO, error) {
	list, err := s.repo.List(ctx, status)
	if err != nil {
		return nil, err
	}
	out := make([]CompetitorDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCompetitorDTO(c))
	}
	return out, nil
}

func (s *GeoCompetitorService) GetCompetitor(ctx context.Context, id uint) (*CompetitorDTO, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := toCompetitorDTO(c)
	return &dto, nil
}

// normalizeCompetitorDomain 只接受裸域名/主机名，拒绝 scheme、路径、用户信息
// 与内网字面量。爬虫会把它拼成 https://<domain><path>，任何多余字符都可能
// 改变目标主机（如 "example.com@127.0.0.1"）。
func normalizeCompetitorDomain(raw string) (string, error) {
	d := strings.TrimSpace(strings.ToLower(raw))
	if d == "" {
		return "", fmt.Errorf("竞品域名不能为空")
	}
	if strings.ContainsAny(d, "/\\?#@ \t\r\n") {
		return "", fmt.Errorf("竞品域名只能是主机名（不要带协议、路径或参数）")
	}
	u, err := url.Parse("https://" + d)
	if err != nil || u.Hostname() == "" || u.Host != d {
		return "", fmt.Errorf("竞品域名格式非法: %s", raw)
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return "", fmt.Errorf("竞品域名不能指向内网主机")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return "", fmt.Errorf("竞品域名不能指向内网地址")
		}
	}
	return d, nil
}

func normalizeCompetitorPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.ContainsAny(p, "\r\n") {
			return nil, fmt.Errorf("竞品路径必须以 / 开头且不能包含 ..: %s", p)
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *GeoCompetitorService) CreateCompetitor(ctx context.Context, dto CompetitorDTO) (*CompetitorDTO, error) {
	if dto.Name == "" || dto.Domain == "" {
		return nil, fmt.Errorf("name and domain are required")
	}
	domain, err := normalizeCompetitorDomain(dto.Domain)
	if err != nil {
		return nil, err
	}
	paths, err := normalizeCompetitorPaths(dto.Paths)
	if err != nil {
		return nil, err
	}
	c := &model.GeoCompetitor{
		Name:     dto.Name,
		Domain:   domain,
		Paths:    strSliceToJSON(paths),
		Category: dto.Category,
		Priority: dto.Priority,
		Status:   dto.Status,
		Notes:    dto.Notes,
	}
	if c.Priority == 0 {
		c.Priority = 5
	}
	if c.Status == "" {
		c.Status = "active"
	}
	if c.Category == "" {
		c.Category = "direct"
	}
	if err := s.repo.Create(ctx, c); err != nil {
		logger.Error(err, "CreateCompetitor failed")
		return nil, err
	}
	dtoOut := toCompetitorDTO(c)
	return &dtoOut, nil
}

func (s *GeoCompetitorService) UpdateCompetitor(ctx context.Context, id uint, dto CompetitorDTO) (*CompetitorDTO, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	domain, err := normalizeCompetitorDomain(dto.Domain)
	if err != nil {
		return nil, err
	}
	paths, err := normalizeCompetitorPaths(dto.Paths)
	if err != nil {
		return nil, err
	}
	c.Name = dto.Name
	c.Domain = domain
	c.Paths = strSliceToJSON(paths)
	c.Category = dto.Category
	c.Priority = dto.Priority
	c.Status = dto.Status
	c.Notes = dto.Notes
	if err := s.repo.Update(ctx, c); err != nil {
		logger.Error(err, "UpdateCompetitor failed")
		return nil, err
	}
	dtoOut := toCompetitorDTO(c)
	return &dtoOut, nil
}

func (s *GeoCompetitorService) DeleteCompetitor(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func toCompetitorDTO(c *model.GeoCompetitor) CompetitorDTO {
	return CompetitorDTO{
		ID:       c.ID,
		Name:     c.Name,
		Domain:   c.Domain,
		Paths:    jsonToStrSlice(c.Paths),
		Category: c.Category,
		Priority: c.Priority,
		Status:   c.Status,
		Notes:    c.Notes,
	}
}

func strSliceToJSON(in []string) []byte {
	if in == nil {
		return []byte("[]")
	}
	b, _ := json.Marshal(in)
	return b
}

func jsonToStrSlice(in []byte) []string {
	if len(in) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(in, &out); err != nil {
		return []string{}
	}
	return out
}
