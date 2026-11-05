package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/health"
	"hivemtk-user/internal/pkg/db"

	"github.com/gin-gonic/gin"
)

type errorPinger struct {
	err error
}

func (f *errorPinger) Ping(ctx context.Context) error {
	return f.err
}

func noDBProbe() health.DBProbe {
	return func(ctx context.Context) (bool, error) { return false, nil }
}

func TestHealthCheck_NoDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevDB := db.GetDB()
	db.SetTestDB(nil)
	t.Cleanup(func() { db.SetTestDB(prevDB) })

	r := gin.New()
	r.GET("/health", health.New(nil, noDBProbe()).Check)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHealthCheck_RedisDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", health.New(&errorPinger{err: errors.New("connection refused")}, noDBProbe()).Check)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusOK {
		t.Errorf("expected 200 or 503, got %d", w.Code)
	}
}

func TestLivenessCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", health.New(nil, noDBProbe()).Live)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestReadinessCheck_NoDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/readyz", health.New(nil, noDBProbe()).Ready)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no DB, got %d", w.Code)
	}
}
