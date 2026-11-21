package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Empty environment entries are common in systemd EnvironmentFile and
// Compose templates. They mean "not specified" while malformed nonempty
// entries still fail closed (covered in v24_test.go).
func TestV241EmptyEnvironmentOverridesAreIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("require_login = true\nsecure_cookies = true\nkeep_last = 3\ndev_mode = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"MINIWIKI_REQUIRE_LOGIN", "MINIWIKI_SECURE_COOKIES", "MINIWIKI_KEEP_LAST", "MINIWIKI_DEV_MODE",
	} {
		t.Setenv(key, "  ")
	}
	cfg, err := LoadChecked(path)
	if err != nil {
		t.Fatalf("empty environment overrides should not break startup: %v", err)
	}
	if !cfg.RequireLogin || !cfg.SecureCookies || !cfg.DevMode || cfg.KeepLast != 3 {
		t.Fatalf("empty environment variables should preserve file settings: %+v", cfg)
	}
}

func TestV241MalformedNonemptyEnvironmentOverridesStillRejected(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"MINIWIKI_REQUIRE_LOGIN", "ture"},
		{"MINIWIKI_SECURE_COOKIES", "nah"},
		{"MINIWIKI_DEV_MODE", "falze"},
		{"MINIWIKI_KEEP_LAST", "zero"},
		{"MINIWIKI_KEEP_LAST", "0"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			_, err := LoadChecked("")
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("incorrect override %s=%q should fail closed, got %v", tc.key, tc.value, err)
			}
		})
	}
}
