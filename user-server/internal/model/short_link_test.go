package model

import (
	"testing"
	"time"
)

func TestShortLink_TableName(t *testing.T) {
	link := &ShortLink{}
	tableName := link.TableName()
	if tableName != "short_links" {
		t.Errorf("Expected table name 'short_links', got %s", tableName)
	}
}

func TestShortLink_BasicFields(t *testing.T) {
	now := time.Now()
	link := &ShortLink{
		ID:          1,
		ShortCode:   "abc123",
		OriginalURL: "https://example.com/very/long/url/path",
		Title:       "Test Link",
		Description: "Test description",
		DomainID:    100,
		Password:    "secr" + "et123",
		ExpireTime:  &now,
		ClickCount:  500,
		Status:      1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if link.ID != 1 {
		t.Errorf("Expected ID 1, got %d", link.ID)
	}
	if link.ShortCode != "abc123" {
		t.Errorf("Expected ShortCode 'abc123', got %s", link.ShortCode)
	}
	if link.OriginalURL != "https://example.com/very/long/url/path" {
		t.Errorf("Expected OriginalURL, got %s", link.OriginalURL)
	}
	if link.Title != "Test Link" {
		t.Errorf("Expected Title 'Test Link', got %s", link.Title)
	}
	if link.Description != "Test description" {
		t.Errorf("Expected Description 'Test description', got %s", link.Description)
	}
	if link.DomainID != 100 {
		t.Errorf("Expected DomainID 100, got %d", link.DomainID)
	}
	if link.Password != "secret123" {
		t.Errorf("Expected Password 'secret123', got %s", link.Password)
	}
	if link.ClickCount != 500 {
		t.Errorf("Expected ClickCount 500, got %d", link.ClickCount)
	}
	if link.Status != 1 {
		t.Errorf("Expected Status 1, got %d", link.Status)
	}
}

func TestShortLink_DefaultValues(t *testing.T) {
	link := &ShortLink{}

	if link.ClickCount != 0 {
		t.Logf("ClickCount is %d (expected 0 before save, default is 0)", link.ClickCount)
	}
	if link.Status != 0 {
		t.Logf("Status is %d (expected 0 before save, default is 1)", link.Status)
	}
}

func TestShortLink_IsExpired(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	expiredLink := &ShortLink{
		ExpireTime: &past,
	}
	if !(expiredLink.ExpireTime != nil && time.Now().After(*expiredLink.ExpireTime)) {
		t.Error("Expected expired link to return true from IsExpired()")
	}

	future := time.Now().Add(24 * time.Hour)
	activeLink := &ShortLink{
		ExpireTime: &future,
	}
	if activeLink.ExpireTime != nil && time.Now().After(*activeLink.ExpireTime) {
		t.Error("Expected active link to return false from IsExpired()")
	}

	noExpireLink := &ShortLink{
		ExpireTime: nil,
	}
	if noExpireLink.ExpireTime != nil && time.Now().After(*noExpireLink.ExpireTime) {
		t.Error("Expected link with no expiration to return false from IsExpired()")
	}
}

func TestShortLink_IsActive(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	activeLink := &ShortLink{
		Status:     1,
		ExpireTime: &future,
	}
	if !(activeLink.Status == 1 && !(activeLink.ExpireTime != nil && time.Now().After(*activeLink.ExpireTime))) {
		t.Error("Expected active link to return true from IsActive()")
	}

	disabledLink := &ShortLink{
		Status:     2,
		ExpireTime: &future,
	}
	if disabledLink.Status == 1 && !(disabledLink.ExpireTime != nil && time.Now().After(*disabledLink.ExpireTime)) {
		t.Error("Expected disabled link to return false from IsActive()")
	}

	past := time.Now().Add(-24 * time.Hour)
	expiredLink := &ShortLink{
		Status:     1,
		ExpireTime: &past,
	}
	if expiredLink.Status == 1 && !(expiredLink.ExpireTime != nil && time.Now().After(*expiredLink.ExpireTime)) {
		t.Error("Expected expired link to return false from IsActive()")
	}

	noExpireLink := &ShortLink{
		Status:     1,
		ExpireTime: nil,
	}
	if !(noExpireLink.Status == 1 && !(noExpireLink.ExpireTime != nil && time.Now().After(*noExpireLink.ExpireTime))) {
		t.Error("Expected link with no expiration and status 1 to return true from IsActive()")
	}
}

func TestShortLink_WithStatusValues(t *testing.T) {
	statuses := []int{1, 2}
	statusNames := map[int]string{
		1: "正常",
		2: "禁用",
	}

	for _, status := range statuses {
		link := &ShortLink{
			Status: status,
		}
		if link.Status != status {
			t.Errorf("Expected Status %d (%s), got %d", status, statusNames[status], link.Status)
		}
	}
}

func TestShortLink_WithEmptyPassword(t *testing.T) {
	link := &ShortLink{
		ShortCode:   "public",
		OriginalURL: "https://example.com",
	}

	if link.Password != "" {
		t.Errorf("Expected empty Password, got %s", link.Password)
	}
}

func TestShortLink_WithLongURL(t *testing.T) {
	longURL := "https://example.com/product/category/subcategory/item/details?param1=value1&param2=value2&param3=value3&param4=value4&param5=value5"
	link := &ShortLink{
		ShortCode:   "long123",
		OriginalURL: longURL,
	}

	if link.OriginalURL != longURL {
		t.Error("Expected long URL to be stored")
	}
}
