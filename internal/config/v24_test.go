package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV24RejectMisconfiguredSecuritySettings(t *testing.T) {
	tests := []struct{ name, text, wants string }{
		{"misspelled key", "require_logon = true\n", "unknown configuration key"},
		{"misspelled bool", "require_login = ture\n", "invalid boolean"},
		{"cookie bool", "secure_cookies = ture\n", "invalid boolean"},
		{"zero retention", "keep_last = 0\n", "positive integer"},
		{"negative retention", "keep_last = -5\n", "positive integer"},
		{"invalid integer", "keep_last = seven\n", "positive integer"},
		{"syntax", "site_name\n", "expected key"},
		{"section", "[something]\n", "not supported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(f, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadChecked(f); err == nil || !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("LoadChecked(%q)=%v, wanted %q", tc.text, err, tc.wants)
			}
		})
	}
}
func TestV24RejectInvalidEnvironmentOverrides(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"MINIWIKI_REQUIRE_LOGIN", "ture"}, {"MINIWIKI_SECURE_COOKIES", "yess"},
		{"MINIWIKI_DEV_MODE", "falze"}, {"MINIWIKI_KEEP_LAST", "zero"}, {"MINIWIKI_KEEP_LAST", "0"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			t.Setenv(tc.key, tc.val)
			if _, err := LoadChecked(""); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected %s validation error, got %v", tc.key, err)
			}
		})
	}
}
func TestV24ConfigValidRetentionAndStrictBools(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(f, []byte("require_login = true\nsecure_cookies = yes\nkeep_last = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadChecked(f)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireLogin || !cfg.SecureCookies || cfg.KeepLast != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
