// Package config loads Blair's runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Registration modes (SPEC §9.1).
const (
	RegistrationClosed = "closed"
	RegistrationInvite = "invite"
	RegistrationOpen   = "open"
)

type Config struct {
	Addr           string
	DataDir        string
	BaseURL        *url.URL
	SecretKey      string
	Registration   string
	Require2FA     bool
	TrustedProxies []netip.Prefix
	BackupInterval time.Duration
	BackupKeep     int
	LogLevel       string
	Prometheus     bool
	MetricsToken   string
	DebugAddr      string
	SecureCookies  bool
	MCPEnabled     bool           // serve /mcp and /oauth (ADR-0016)
	OAuthRedirects []string       // extra OAuth callbacks, for local testing only
	MCPCIDRs       []netip.Prefix // when set, /mcp and the OAuth data endpoints only answer these addresses
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Load reads the environment. It never logs secret values.
func Load() (*Config, error) {
	c := &Config{
		Addr:         env("BLAIR_ADDR", ":8080"),
		DataDir:      env("BLAIR_DATA_DIR", "./data"),
		SecretKey:    os.Getenv("BLAIR_SECRET_KEY"),
		Registration: env("BLAIR_REGISTRATION", RegistrationClosed),
		LogLevel:     env("BLAIR_LOG_LEVEL", "info"),
		MetricsToken: os.Getenv("BLAIR_METRICS_TOKEN"),
		DebugAddr:    os.Getenv("BLAIR_DEBUG_ADDR"),
	}
	switch c.Registration {
	case RegistrationClosed, RegistrationInvite, RegistrationOpen:
	default:
		return nil, fmt.Errorf("BLAIR_REGISTRATION must be closed, invite or open, got %q", c.Registration)
	}

	base := env("BLAIR_BASE_URL", "http://localhost:8080")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("BLAIR_BASE_URL must be an absolute URL, got %q", base)
	}
	c.BaseURL = u
	c.SecureCookies = u.Scheme == "https" || u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"

	var perr error
	if c.Require2FA, perr = parseBool("BLAIR_REQUIRE_2FA", false); perr != nil {
		return nil, perr
	}
	if c.Prometheus, perr = parseBool("BLAIR_PROMETHEUS", false); perr != nil {
		return nil, perr
	}
	if c.MCPEnabled, perr = parseBool("BLAIR_MCP_ENABLED", false); perr != nil {
		return nil, perr
	}
	if c.MCPEnabled && u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("BLAIR_MCP_ENABLED needs an https BLAIR_BASE_URL, got %q", base)
	}
	for _, v := range strings.Split(os.Getenv("BLAIR_MCP_ALLOWED_CIDRS"), ",") {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		pfx, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, fmt.Errorf("BLAIR_MCP_ALLOWED_CIDRS: %w", err)
		}
		c.MCPCIDRs = append(c.MCPCIDRs, pfx)
	}
	for _, v := range strings.Split(os.Getenv("BLAIR_OAUTH_EXTRA_REDIRECT_URIS"), ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		ru, err := url.Parse(v)
		loopback := err == nil && ru.Scheme == "http" && (ru.Hostname() == "localhost" || ru.Hostname() == "127.0.0.1")
		if err != nil || ru.Host == "" || ru.Fragment != "" || (ru.Scheme != "https" && !loopback) {
			return nil, fmt.Errorf("BLAIR_OAUTH_EXTRA_REDIRECT_URIS: %q must be https or an http loopback URL", v)
		}
		c.OAuthRedirects = append(c.OAuthRedirects, v)
	}
	if c.DebugAddr != "" {
		ap, err := netip.ParseAddrPort(c.DebugAddr)
		if err != nil || !ap.Addr().IsLoopback() {
			return nil, fmt.Errorf("BLAIR_DEBUG_ADDR must be a loopback host:port, got %q", c.DebugAddr)
		}
	}

	for _, p := range strings.Split(env("BLAIR_TRUSTED_PROXIES", "127.0.0.1/32,::1/128"), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("BLAIR_TRUSTED_PROXIES: %w", err)
		}
		c.TrustedProxies = append(c.TrustedProxies, pfx)
	}

	if c.BackupInterval, err = time.ParseDuration(env("BLAIR_BACKUP_INTERVAL", "24h")); err != nil {
		return nil, fmt.Errorf("BLAIR_BACKUP_INTERVAL: %w", err)
	}
	if c.BackupKeep, err = strconv.Atoi(env("BLAIR_BACKUP_KEEP", "14")); err != nil || c.BackupKeep < 1 {
		return nil, fmt.Errorf("BLAIR_BACKUP_KEEP must be a positive integer")
	}
	return c, nil
}

func parseBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
