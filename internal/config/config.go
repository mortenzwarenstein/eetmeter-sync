// Package config loads all runtime settings from environment variables. See
// specs/001-recipe-sync/contracts/http-api.md for the full list.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/dotenv"
)

// Account is one Mijn Eetmeter account's identity and credentials.
//
// Two auth modes:
//   - Token + DeviceID: a device-bound pair captured from the app. Preferred —
//     the API's device-registration login is not reproducible from a server.
//   - Email + Password: only works if the API still accepts a bare credential
//     login (it currently does not without a pre-registered device token).
type Account struct {
	Label    string
	Email    string
	Password string
	Token    string
	DeviceID string
}

// HasToken reports whether a usable Token+DeviceID pair is configured.
func (a Account) HasToken() bool { return a.Token != "" && a.DeviceID != "" }

// Eetmeter holds API-endpoint settings (overridable mainly for local runs and
// the spike).
type Eetmeter struct {
	BaseURL    string
	AppVersion string
	Platform   string
}

// Config is the fully resolved configuration.
type Config struct {
	DatabaseURL  string
	AccountA     Account
	AccountB     Account
	DailyTime    string // "HH:MM"
	Timezone     string // IANA name
	RunOnStartup bool
	DryRun       bool // preview every run: log what it would do, write nothing
	HTTPAddr     string
	APIToken     string
	Eetmeter     Eetmeter
	LogLevel     slog.Level
}

// Load reads .env (if present, parsed literally — no shell expansion), then
// reads and validates the environment. It returns an error listing every
// problem found rather than failing on the first.
func Load() (Config, error) {
	if err := dotenv.Load(".env"); err != nil {
		return Config{}, fmt.Errorf("config: reading .env: %w", err)
	}

	var missing []string
	req := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	opt := func(key, def string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return def
	}
	// note: passwords/tokens are taken raw (no TrimSpace) so a value that
	// legitimately ends in whitespace is preserved; .env strips the newline.
	raw := func(key string) string { return os.Getenv(key) }

	cfg := Config{
		DatabaseURL: req("DATABASE_URL"),
		AccountA: Account{
			Label:    opt("EETMETER_ACCOUNT_A_LABEL", "a"),
			Email:    opt("EETMETER_ACCOUNT_A_EMAIL", ""),
			Password: raw("EETMETER_ACCOUNT_A_PASSWORD"),
			Token:    strings.TrimSpace(raw("EETMETER_ACCOUNT_A_TOKEN")),
			DeviceID: strings.TrimSpace(raw("EETMETER_ACCOUNT_A_DEVICE_ID")),
		},
		AccountB: Account{
			Label:    opt("EETMETER_ACCOUNT_B_LABEL", "b"),
			Email:    opt("EETMETER_ACCOUNT_B_EMAIL", ""),
			Password: raw("EETMETER_ACCOUNT_B_PASSWORD"),
			Token:    strings.TrimSpace(raw("EETMETER_ACCOUNT_B_TOKEN")),
			DeviceID: strings.TrimSpace(raw("EETMETER_ACCOUNT_B_DEVICE_ID")),
		},
		DailyTime:    opt("SYNC_DAILY_TIME", "06:00"),
		Timezone:     opt("SYNC_TIMEZONE", "Europe/Amsterdam"),
		RunOnStartup: truthy(opt("SYNC_ON_STARTUP", "false")),
		DryRun:       truthy(opt("SYNC_DRY_RUN", "false")),
		HTTPAddr:     opt("HTTP_ADDR", ":8080"),
		APIToken:     strings.TrimSpace(os.Getenv("SYNC_API_TOKEN")),
		Eetmeter: Eetmeter{
			BaseURL:    opt("EETMETER_API_BASE_URL", "https://api3-mijn.voedingscentrum.nl/api/"),
			AppVersion: opt("EETMETER_APP_VERSION", "4.6.0"),
			Platform:   opt("EETMETER_PLATFORM", "iOS"),
		},
		LogLevel: parseLevel(opt("LOG_LEVEL", "info")),
	}

	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "missing required env: "+strings.Join(missing, ", "))
	}
	for _, a := range []struct {
		id  string
		acc Account
	}{{"A", cfg.AccountA}, {"B", cfg.AccountB}} {
		switch {
		case a.acc.HasToken():
			// token + deviceId is enough; email/password are not consulted.
		case a.acc.Password != "" && a.acc.Email != "":
			// password login also needs the email.
		default:
			problems = append(problems, fmt.Sprintf(
				"account %s needs EETMETER_ACCOUNT_%s_TOKEN + EETMETER_ACCOUNT_%s_DEVICE_ID "+
					"(or EETMETER_ACCOUNT_%s_EMAIL + EETMETER_ACCOUNT_%s_PASSWORD)",
				a.id, a.id, a.id, a.id, a.id))
		}
	}
	if _, err := parseDailyTime(cfg.DailyTime); err != nil {
		problems = append(problems, err.Error())
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		problems = append(problems, fmt.Sprintf("SYNC_TIMEZONE %q is not a valid IANA timezone", cfg.Timezone))
	}
	if cfg.AccountA.Email != "" && cfg.AccountA.Email == cfg.AccountB.Email {
		problems = append(problems, "EETMETER_ACCOUNT_A_EMAIL and EETMETER_ACCOUNT_B_EMAIL must differ")
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// DailyRun returns hour and minute parsed from DailyTime.
func (c Config) DailyRun() (hour, min int) {
	hm, _ := parseDailyTime(c.DailyTime)
	return hm[0], hm[1]
}

// Location returns the configured timezone (already validated by Load).
func (c Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// LogValue implements slog.LogValuer so a Config can be logged without leaking
// passwords or the API token.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("databaseURL", redactDSN(c.DatabaseURL)),
		slog.String("accountA", c.AccountA.Email),
		slog.String("accountAAuth", authMode(c.AccountA)),
		slog.String("accountB", c.AccountB.Email),
		slog.String("accountBAuth", authMode(c.AccountB)),
		slog.String("dailyTime", c.DailyTime),
		slog.String("timezone", c.Timezone),
		slog.Bool("runOnStartup", c.RunOnStartup),
		slog.Bool("dryRun", c.DryRun),
		slog.String("httpAddr", c.HTTPAddr),
		slog.Bool("apiTokenSet", c.APIToken != ""),
		slog.String("eetmeterBaseURL", c.Eetmeter.BaseURL),
	)
}

// String is deliberately the redacted form too, so accidental %v/%s is safe.
func (c Config) String() string { return c.LogValue().String() }

func parseDailyTime(s string) ([2]int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return [2]int{}, fmt.Errorf("SYNC_DAILY_TIME %q must be HH:MM", s)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return [2]int{}, fmt.Errorf("SYNC_DAILY_TIME %q must be HH:MM in 00:00..23:59", s)
	}
	return [2]int{h, m}, nil
}

func authMode(a Account) string {
	switch {
	case a.HasToken() && a.Password != "":
		return "token+password"
	case a.HasToken():
		return "token"
	case a.Password != "":
		return "password"
	default:
		return "none"
	}
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// redactDSN hides the password in a postgres URL for logging.
func redactDSN(dsn string) string {
	at := strings.LastIndex(dsn, "@")
	scheme := strings.Index(dsn, "://")
	if at == -1 || scheme == -1 || at < scheme {
		return dsn
	}
	creds := dsn[scheme+3 : at]
	if i := strings.IndexByte(creds, ':'); i >= 0 {
		creds = creds[:i] + ":***"
	}
	return dsn[:scheme+3] + creds + dsn[at:]
}
