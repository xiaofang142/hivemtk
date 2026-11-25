package service

import "testing"

func TestTestCred_DefaultsAndEnvOverride(t *testing.T) {
	t.Setenv("HIVEMTK_TEST_CRED_OLD_KEY", "")
	for key, want := range map[string]string{
		"key":         "test_key",
		"old_key":     "old_key",
		"keep_secret": "keep_secret",
		"new_key":     "new_key",
		"unknown":     "test_key",
	} {
		if got := testCred(key); got != want {
			t.Errorf("testCred(%q)=%q，期望 %q", key, got, want)
		}
	}
	t.Setenv("HIVEMTK_TEST_CRED_OLD_KEY", "env-old-key")
	if got := testCred("old-key"); got != "env-old-key" {
		t.Errorf("env override 未生效，实际 %q", got)
	}
}
