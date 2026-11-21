package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShippedExampleParses(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	example := filepath.Join(filepath.Dir(thisFile), "..", "..", "config.example.toml")
	cfg, err := LoadChecked(example)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupEvery != "" {
		t.Fatalf("backup_every expected empty, got %q", cfg.BackupEvery)
	}
	if cfg.KeepLast != 7 {
		t.Fatalf("keep_last got %d", cfg.KeepLast)
	}
}
func TestInlineCommentsAndSignificantQuoteCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `backup_every = "24h" # comment
smtp_password = "abc'\\\"" # comment
smtp_username = 'literal\"' # literal doesn't interpret escapes
site_name = "Hello # not comment"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadChecked(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupEvery != "24h" {
		t.Errorf("backup_every %q", cfg.BackupEvery)
	}
	if cfg.SMTPPassword != `abc'\"` {
		t.Errorf("smtp_password %q", cfg.SMTPPassword)
	}
	if cfg.SMTPUsername != `literal\"` {
		t.Errorf("smtp_username %q", cfg.SMTPUsername)
	}
	if cfg.SiteName != "Hello # not comment" {
		t.Errorf("site name %q", cfg.SiteName)
	}
}
func TestUnterminatedQuotedValueRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("smtp_password = \"incomplete\n"), 0600)
	_, err := LoadChecked(path)
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected useful parse error: %v", err)
	}
}
