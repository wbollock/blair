// Command blair runs the Blair server and its admin CLI.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // distroless images have no zoneinfo

	"github.com/wbollock/blair/api"
	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/config"
	"github.com/wbollock/blair/internal/httpapi"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
	"github.com/wbollock/blair/internal/telemetry"
	"github.com/wbollock/blair/internal/web"
)

// version is set with -ldflags "-X main.version=..."; falls back to VCS info.
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if rev != "" {
			if dirty {
				rev += "-dirty"
			}
			return rev
		}
	}
	return "dev"
}

const usage = `blair — self-hosted tasks and notes

Usage:
  blair [serve]                         run the server (default)
  blair migrate                         apply database migrations and exit
  blair user create --email E [--admin] [--name N]   (password read from stdin)
  blair user list
  blair user passwd --email E           set a password (stdin); forces a change at next login
  blair user reset-2fa --email E        turn off 2FA for a user
  blair backup [--to PATH]              write a consistent database copy
  blair healthcheck                     exit 0 if the local server is ready (container probes)
  blair version

Configuration is read from BLAIR_* and OTEL_* environment variables (see docs/SPEC.md §14).
`

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = withDB(func(ctx context.Context, _ *config.Config, _ *store.DB, _ *service.Service) error {
			fmt.Println("migrations applied")
			return nil
		})
	case "user":
		err = userCmd(args)
	case "backup":
		err = backupCmd(args)
	case "healthcheck":
		err = healthcheck()
	case "version":
		fmt.Println(buildVersion())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// secretKey returns BLAIR_SECRET_KEY or a generated key persisted in the data dir.
func secretKey(cfg *config.Config) ([]byte, error) {
	if cfg.SecretKey != "" {
		if len(cfg.SecretKey) < 32 {
			return nil, errors.New("BLAIR_SECRET_KEY must be at least 32 characters")
		}
		return []byte(cfg.SecretKey), nil
	}
	path := filepath.Join(cfg.DataDir, "secret.key")
	if b, err := os.ReadFile(path); err == nil {
		k := strings.TrimSpace(string(b))
		if len(k) < 32 {
			return nil, fmt.Errorf("%s is too short", path)
		}
		return []byte(k), nil
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	k := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(k+"\n"), 0o600); err != nil {
		return nil, err
	}
	slog.Info("generated secret key", "path", path)
	return []byte(k), nil
}

func withDB(fn func(context.Context, *config.Config, *store.DB, *service.Service) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	key, err := secretKey(cfg)
	if err != nil {
		return err
	}
	box, err := auth.NewSecretBox(key, "totp")
	if err != nil {
		return err
	}
	svc := service.New(db, box, service.Options{Registration: cfg.Registration, Require2FA: cfg.Require2FA})
	return fn(ctx, cfg, db, svc)
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, telemetry.Options{Version: buildVersion(), LogLevel: cfg.LogLevel, Prometheus: cfg.Prometheus})
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tel.Shutdown(sctx)
	}()

	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	key, err := secretKey(cfg)
	if err != nil {
		return err
	}
	box, err := auth.NewSecretBox(key, "totp")
	if err != nil {
		return err
	}
	svc := service.New(db, box, service.Options{Registration: cfg.Registration, Require2FA: cfg.Require2FA})

	if svc.SetupRequired(ctx) {
		tok, err := svc.SetupToken(ctx)
		if err != nil {
			return err
		}
		u := *cfg.BaseURL
		u.Path = "/setup"
		u.RawQuery = "token=" + tok
		slog.Warn("no accounts yet: open the one-time setup link within 1 hour (restart for a new one)", "url", u.String())
	}

	openapiJSON, err := api.JSON()
	if err != nil {
		return fmt.Errorf("openapi contract: %w", err)
	}
	handler := httpapi.New(svc, db, httpapi.Config{
		Secure:         cfg.SecureCookies,
		HSTS:           cfg.BaseURL.Scheme == "https",
		TrustedProxies: cfg.TrustedProxies,
		MetricsHandler: tel.MetricsHandler,
		MetricsToken:   cfg.MetricsToken,
		Version:        buildVersion(),
		Web:            web.FS(),
		OpenAPIYAML:    api.YAML,
		OpenAPIJSON:    openapiJSON,
		BaseURL:        cfg.BaseURL,
		MCPEnabled:     cfg.MCPEnabled,
		OAuthRedirects: cfg.OAuthRedirects,
		MCPCIDRs:       cfg.MCPCIDRs,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	if cfg.DebugAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		go func() {
			slog.Info("pprof listening", "addr", cfg.DebugAddr)
			_ = http.ListenAndServe(cfg.DebugAddr, mux) //nolint:gosec // loopback-only, validated in config
		}()
	}

	go background(ctx, cfg, db, svc)

	errc := make(chan error, 1)
	go func() {
		slog.Info("blair listening", "addr", cfg.Addr, "version", buildVersion(), "base_url", cfg.BaseURL.String(), "telemetry_export", tel.ExportersActive)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
	return nil
}

// background fires reminders every 30 s, runs housekeeping hourly and takes scheduled backups.
func background(ctx context.Context, cfg *config.Config, db *store.DB, svc *service.Service) {
	remind := time.NewTicker(30 * time.Second)
	house := time.NewTicker(time.Hour)
	backup := time.NewTicker(cfg.BackupInterval)
	defer remind.Stop()
	defer house.Stop()
	defer backup.Stop()
	_ = svc.Housekeeping(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-remind.C:
			if n, err := svc.FireReminders(ctx); err != nil {
				slog.Error("firing reminders failed", "err", err)
			} else if n > 0 {
				slog.Info("reminders fired", "count", n)
			}
		case <-house.C:
			if err := svc.Housekeeping(ctx); err != nil {
				slog.Error("housekeeping failed", "err", err)
			}
		case <-backup.C:
			if path, err := db.ScheduledBackup(ctx, cfg.BackupKeep); err != nil {
				slog.Error("backup failed", "err", err)
			} else {
				slog.Info("backup written", "path", path)
			}
		}
	}
}

// healthcheck probes /readyz on the configured listen port (distroless has no curl).
func healthcheck() error {
	addr := os.Getenv("BLAIR_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz returned %d", res.StatusCode)
	}
	return nil
}

func readPassword() (string, error) {
	if pw := os.Getenv("BLAIR_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Fprint(os.Stderr, "password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func userCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("user needs a subcommand: create, list, passwd, reset-2fa")
	}
	fs := flag.NewFlagSet("user "+args[0], flag.ExitOnError)
	email := fs.String("email", "", "account email")
	admin := fs.Bool("admin", false, "make the user an admin")
	name := fs.String("name", "", "display name")
	tz := fs.String("timezone", "UTC", "IANA timezone")
	_ = fs.Parse(args[1:])

	return withDB(func(ctx context.Context, _ *config.Config, _ *store.DB, svc *service.Service) error {
		findUser := func() (*service.User, error) {
			users, err := svc.ListUsers(ctx)
			if err != nil {
				return nil, err
			}
			for i := range users {
				if strings.EqualFold(users[i].Email, *email) {
					return &users[i], nil
				}
			}
			return nil, fmt.Errorf("no user with email %q", *email)
		}
		switch args[0] {
		case "create":
			pw, err := readPassword()
			if err != nil {
				return err
			}
			u, err := svc.CreateUser(ctx, service.NewUser{Email: *email, Password: pw, DisplayName: *name, Timezone: *tz}, *admin)
			if err != nil {
				return err
			}
			fmt.Printf("created %s (%s) admin=%v\n", u.Email, u.ID, u.IsAdmin)
		case "list":
			users, err := svc.ListUsers(ctx)
			if err != nil {
				return err
			}
			for _, u := range users {
				fmt.Printf("%s\t%s\tadmin=%v\t2fa=%v\tdisabled=%v\n", u.ID, u.Email, u.IsAdmin, u.TOTPEnabled, u.Disabled)
			}
		case "passwd":
			u, err := findUser()
			if err != nil {
				return err
			}
			pw, err := readPassword()
			if err != nil {
				return err
			}
			if _, err := svc.AdminUpdateUser(ctx, &service.User{ID: "cli"}, u.ID, service.AdminUserPatch{Password: service.Some(pw)}); err != nil {
				return err
			}
			fmt.Println("password set; the user must change it at next sign-in")
		case "reset-2fa":
			u, err := findUser()
			if err != nil {
				return err
			}
			if _, err := svc.AdminUpdateUser(ctx, &service.User{ID: "cli"}, u.ID, service.AdminUserPatch{Reset2FA: service.Some(true)}); err != nil {
				return err
			}
			fmt.Println("2FA turned off for", u.Email)
		default:
			return fmt.Errorf("unknown user subcommand %q", args[0])
		}
		return nil
	})
}

func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	to := fs.String("to", "", "destination file (default: data/backups/blair-<time>.db)")
	_ = fs.Parse(args)
	return withDB(func(ctx context.Context, cfg *config.Config, db *store.DB, _ *service.Service) error {
		if *to == "" {
			p, err := db.ScheduledBackup(ctx, cfg.BackupKeep)
			if err == nil {
				fmt.Println(p)
			}
			return err
		}
		if err := db.BackupTo(ctx, *to); err != nil {
			return err
		}
		fmt.Println(*to)
		return nil
	})
}
