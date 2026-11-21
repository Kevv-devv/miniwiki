package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr           string
	DBPath         string
	SiteName       string
	SecureCookies  bool
	RequireLogin   bool
	BackupDir      string
	BackupEvery    string
	KeepLast       int
	TrustedProxies string // comma-separated IPs or CIDRs permitted to supply X-Forwarded-For
	UploadDir      string
	BaseURL        string
	SMTPHost       string
	SMTPPort       string
	SMTPUsername   string
	SMTPPassword   string
	SMTPFrom       string
	DevMode        bool
}

// Load is the checked entry point; configuration errors must never be ignored.
func Load(path string) (Config, error) { return LoadChecked(path) }

// LoadChecked reports malformed configuration rather than quietly changing credentials.
func LoadChecked(path string) (Config, error) {
	cfg := Config{
		Addr:           getenv("MINIWIKI_ADDR", ":8080"),
		DBPath:         getenv("MINIWIKI_DB", "wiki.db"),
		SiteName:       getenv("MINIWIKI_SITE_NAME", "MiniWiki"),
		SecureCookies:  envBool("MINIWIKI_SECURE_COOKIES", false),
		RequireLogin:   envBool("MINIWIKI_REQUIRE_LOGIN", false),
		BackupDir:      getenv("MINIWIKI_BACKUP_DIR", "backups"),
		BackupEvery:    getenv("MINIWIKI_BACKUP_EVERY", ""),
		KeepLast:       7,
		TrustedProxies: getenv("MINIWIKI_TRUSTED_PROXIES", ""),
		UploadDir:      getenv("MINIWIKI_UPLOAD_DIR", "uploads"),
		BaseURL:        getenv("MINIWIKI_BASE_URL", "http://localhost:8080"),
		SMTPHost:       getenv("MINIWIKI_SMTP_HOST", ""),
		SMTPPort:       getenv("MINIWIKI_SMTP_PORT", "587"),
		SMTPUsername:   getenv("MINIWIKI_SMTP_USERNAME", ""),
		SMTPPassword:   getenv("MINIWIKI_SMTP_PASSWORD", ""),
		SMTPFrom:       getenv("MINIWIKI_SMTP_FROM", ""),
		DevMode:        envBool("MINIWIKI_DEV_MODE", false),
	}
	if path != "" {
		if err := applyFile(path, &cfg); err != nil {
			return cfg, err
		}
	}
	// Environment variables win over config file values.
	var err error
	cfg.Addr = getenv("MINIWIKI_ADDR", cfg.Addr)
	cfg.DBPath = getenv("MINIWIKI_DB", cfg.DBPath)
	cfg.SiteName = getenv("MINIWIKI_SITE_NAME", cfg.SiteName)
	if cfg.SecureCookies, err = envBoolChecked("MINIWIKI_SECURE_COOKIES", cfg.SecureCookies); err != nil {
		return cfg, err
	}
	if cfg.RequireLogin, err = envBoolChecked("MINIWIKI_REQUIRE_LOGIN", cfg.RequireLogin); err != nil {
		return cfg, err
	}
	cfg.BackupDir = getenv("MINIWIKI_BACKUP_DIR", cfg.BackupDir)
	cfg.BackupEvery = getenv("MINIWIKI_BACKUP_EVERY", cfg.BackupEvery)
	if cfg.KeepLast, err = envIntChecked("MINIWIKI_KEEP_LAST", cfg.KeepLast); err != nil {
		return cfg, err
	}
	cfg.TrustedProxies = getenv("MINIWIKI_TRUSTED_PROXIES", cfg.TrustedProxies)
	cfg.UploadDir = getenv("MINIWIKI_UPLOAD_DIR", cfg.UploadDir)
	cfg.BaseURL = getenv("MINIWIKI_BASE_URL", cfg.BaseURL)
	cfg.SMTPHost = getenv("MINIWIKI_SMTP_HOST", cfg.SMTPHost)
	cfg.SMTPPort = getenv("MINIWIKI_SMTP_PORT", cfg.SMTPPort)
	cfg.SMTPUsername = getenv("MINIWIKI_SMTP_USERNAME", cfg.SMTPUsername)
	cfg.SMTPPassword = getenv("MINIWIKI_SMTP_PASSWORD", cfg.SMTPPassword)
	cfg.SMTPFrom = getenv("MINIWIKI_SMTP_FROM", cfg.SMTPFrom)
	if cfg.DevMode, err = envBoolChecked("MINIWIKI_DEV_MODE", cfg.DevMode); err != nil {
		return cfg, err
	}
	if cfg.KeepLast < 1 {
		return cfg, fmt.Errorf("keep_last must be a positive integer")
	}
	return cfg, nil
}

// parseValue supports TOML string scalars and inline comments without losing
// significant quote characters in SMTP passwords.
func parseValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw[0] == '"' {
		// strconv.Unquote consumes escapes and detects unbalanced quotes.
		escaped := false
		for i := 1; i < len(raw); i++ {
			if !escaped && raw[i] == '"' {
				value, err := strconv.Unquote(raw[:i+1])
				if err != nil {
					return "", err
				}
				suffix := strings.TrimSpace(raw[i+1:])
				if suffix != "" && !strings.HasPrefix(suffix, "#") {
					return "", fmt.Errorf("unexpected text after quoted value")
				}
				return value, nil
			}
			if raw[i] == '\\' {
				escaped = !escaped
			} else {
				escaped = false
			}
		}
		return "", fmt.Errorf("unterminated quoted value")
	}
	if raw[0] == '\'' {
		if i := strings.IndexByte(raw[1:], '\''); i >= 0 {
			i++
			suffix := strings.TrimSpace(raw[i+1:])
			if suffix != "" && !strings.HasPrefix(suffix, "#") {
				return "", fmt.Errorf("unexpected text after literal value")
			}
			return raw[1:i], nil
		}
		return "", fmt.Errorf("unterminated literal value")
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw), nil
}

func applyFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	lineNum := 0
	for s.Scan() {
		lineNum++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return fmt.Errorf("%s:%d: config sections are not supported", path, lineNum)
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key = value", path, lineNum)
		}
		key := strings.TrimSpace(k)
		val, err := parseValue(v)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNum, err)
		}
		switch key {
		case "addr":
			cfg.Addr = val
		case "db_path":
			cfg.DBPath = val
		case "site_name":
			cfg.SiteName = val
		case "secure_cookies":
			cfg.SecureCookies, err = parseBoolChecked(val)
			if err != nil {
				return fmt.Errorf("%s:%d: secure_cookies: %w", path, lineNum, err)
			}
		case "backup_dir":
			cfg.BackupDir = val
		case "backup_every":
			cfg.BackupEvery = val
		case "keep_last":
			cfg.KeepLast, err = strconv.Atoi(val)
			if err != nil || cfg.KeepLast < 1 {
				return fmt.Errorf("%s:%d: keep_last must be a positive integer", path, lineNum)
			}
		case "require_login":
			cfg.RequireLogin, err = parseBoolChecked(val)
			if err != nil {
				return fmt.Errorf("%s:%d: require_login: %w", path, lineNum, err)
			}
		case "trusted_proxies":
			cfg.TrustedProxies = val
		case "upload_dir":
			cfg.UploadDir = val
		case "base_url":
			cfg.BaseURL = val
		case "smtp_host":
			cfg.SMTPHost = val
		case "smtp_port":
			cfg.SMTPPort = val
		case "smtp_username":
			cfg.SMTPUsername = val
		case "smtp_password":
			cfg.SMTPPassword = val
		case "smtp_from":
			cfg.SMTPFrom = val
		case "dev_mode":
			cfg.DevMode, err = parseBoolChecked(val)
			if err != nil {
				return fmt.Errorf("%s:%d: dev_mode: %w", path, lineNum, err)
			}
		default:
			return fmt.Errorf("%s:%d: unknown configuration key %q", path, lineNum, key)
		}
	}
	return s.Err()
}

func envIntChecked(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}
func envBoolChecked(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	result, err := parseBoolChecked(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}
func parseBoolChecked(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q (use true or false)", v)
	}
}

func envInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return parseBool(v)
	}
	return fallback
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
