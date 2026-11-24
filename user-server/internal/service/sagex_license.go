package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var (
	ErrSageXLicenseNotFound         = errors.New("sagex_license_not_found")
	ErrSageXLicenseInvalidated      = errors.New("sagex_license_invalidated")
	ErrSageXLicenseDeviceMismatch   = errors.New("sagex_license_device_mismatch")
	ErrSageXLicenseAlreadyActivated = errors.New("sagex_license_already_activated")
)

type SageXLicenseService struct {
	codeRepo      *repository.SageXActivationCodeRepository
	heartbeatRepo *repository.SageXHeartbeatRepository
}

func NewSageXLicenseService() *SageXLicenseService {
	return &SageXLicenseService{
		codeRepo:      repository.NewSageXActivationCodeRepository(),
		heartbeatRepo: repository.NewSageXHeartbeatRepository(),
	}
}

func (s *SageXLicenseService) SetRepos(codeRepo *repository.SageXActivationCodeRepository, hbRepo *repository.SageXHeartbeatRepository) {
	if codeRepo != nil {
		s.codeRepo = codeRepo
	}
	if hbRepo != nil {
		s.heartbeatRepo = hbRepo
	}
}

func generateSageXActivationCode() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// IssueCode 签发一个新的授权码（issued 状态，未激活）。
func (s *SageXLicenseService) IssueCode(ctx context.Context) (string, error) {
	code, err := generateSageXActivationCode()
	if err != nil {
		return "", err
	}
	now := time.Now()
	rec := &model.SageXActivationCode{
		Code:   code,
		Status: "issued",
	}
	// 防止极小概率碰撞：重试
	for i := 0; i < 3; i++ {
		if err := s.codeRepo.Create(ctx, rec); err == nil {
			return code, nil
		} else {
			// regenerate
			code, _ = generateSageXActivationCode()
			rec.Code = code
		}
		_ = now
	}
	return "", errors.New("issue_code_failed")
}

func (s *SageXLicenseService) ListCodes(ctx context.Context, status string) ([]*model.SageXActivationCode, error) {
	return s.codeRepo.List(ctx, status)
}

func (s *SageXLicenseService) InvalidateCode(ctx context.Context, code, reason string) error {
	rec, err := s.codeRepo.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if rec == nil {
		return ErrSageXLicenseNotFound
	}
	now := time.Now()
	rec.Status = "invalidated"
	rec.InvalidatedAt = &now
	rec.InvalidationReason = reason
	return s.codeRepo.Update(ctx, rec)
}

type SageXActivateRequest struct {
	Code              string `json:"code"`
	DeviceFingerprint string `json:"device_fingerprint"`
	DeviceModel       string `json:"device_model"`
	IP                string `json:"ip"`
	Phone             string `json:"phone"`
}

// Activate 首次激活：将授权码与设备指纹绑定。单次激活。
func (s *SageXLicenseService) Activate(ctx context.Context, req SageXActivateRequest) (string, error) {
	if req.Code == "" || req.DeviceFingerprint == "" {
		return "", errors.New("invalid_request")
	}
	rec, err := s.codeRepo.GetByCode(ctx, req.Code)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", ErrSageXLicenseNotFound
	}
	if rec.Status == "invalidated" {
		return "", ErrSageXLicenseInvalidated
	}
	if rec.Status == "activated" {
		if rec.DeviceFingerprint != req.DeviceFingerprint {
			return "", ErrSageXLicenseDeviceMismatch
		}
		return rec.Status, nil
	}
	// issued -> activated：条件更新，防止并发竞态双激活
	now := time.Now()
	updated := &model.SageXActivationCode{
		Code:             rec.Code,
		Status:           "activated",
		DeviceFingerprint: req.DeviceFingerprint,
		DeviceModel:      req.DeviceModel,
		IP:               req.IP,
		Phone:            req.Phone,
		ActivatedAt:      &now,
	}
	if req.IP == "" {
		updated.IP = rec.IP
	}
	if req.Phone == "" {
		updated.Phone = rec.Phone
	}
	// 条件更新：仅当仍为 issued 时才写入
	rows, err := s.codeRepo.ActivateIfIssued(ctx, rec.Code, req.DeviceFingerprint, req.DeviceModel, req.IP, req.Phone, now)
	if err != nil {
		return "", err
	}
	if rows == 0 {
		// 并发已被他人激活：重新读取比对
		rec2, _ := s.codeRepo.GetByCode(ctx, req.Code)
		if rec2 == nil {
			return "", ErrSageXLicenseNotFound
		}
		if rec2.Status == "invalidated" {
			return "", ErrSageXLicenseInvalidated
		}
		if rec2.DeviceFingerprint != req.DeviceFingerprint {
			return "", ErrSageXLicenseDeviceMismatch
		}
		return rec2.Status, nil
	}
	_ = updated
	return "activated", nil
}

// Verify 静默校验：授权码与设备指纹必须一致且未失效。
func (s *SageXLicenseService) Verify(ctx context.Context, code, deviceFingerprint string) (bool, string, error) {
	rec, err := s.codeRepo.GetByCode(ctx, code)
	if err != nil {
		return false, "", err
	}
	if rec == nil {
		return false, "", ErrSageXLicenseNotFound
	}
	if rec.Status == "invalidated" {
		return false, rec.Status, ErrSageXLicenseInvalidated
	}
	if rec.Status != "activated" {
		return false, rec.Status, nil
	}
	if rec.DeviceFingerprint != deviceFingerprint {
		return false, rec.Status, ErrSageXLicenseDeviceMismatch
	}
	return true, rec.Status, nil
}

type SageXHeartbeatRequest struct {
	Code              string `json:"code"`
	DeviceFingerprint string `json:"device_fingerprint"`
	DeviceModel       string `json:"device_model"`
	IP                string `json:"ip"`
	Phone             string `json:"phone"`
	AppVersion        string `json:"app_version"`
}

// Heartbeat 记录心跳，并在授权码失效时返回错误。
func (s *SageXLicenseService) Heartbeat(ctx context.Context, req SageXHeartbeatRequest) error {
	ok, status, err := s.Verify(ctx, req.Code, req.DeviceFingerprint)
	if err != nil {
		return err
	}
	if !ok || status != "activated" {
		if status == "invalidated" {
			return ErrSageXLicenseInvalidated
		}
		return ErrSageXLicenseNotFound
	}
	hb := &model.SageXHeartbeat{
		Code:              req.Code,
		DeviceFingerprint: req.DeviceFingerprint,
		DeviceModel:       req.DeviceModel,
		IP:                req.IP,
		Phone:             req.Phone,
		AppVersion:        req.AppVersion,
		ReportedAt:        time.Now(),
	}
	return s.heartbeatRepo.Create(ctx, hb)
}

func (s *SageXLicenseService) ListHeartbeats(ctx context.Context, code string) ([]*model.SageXHeartbeat, error) {
	return s.heartbeatRepo.List(ctx, code)
}
