package config

import (
	"strings"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:secretpw@localhost:5432/db")
	t.Setenv("EETMETER_ACCOUNT_A_EMAIL", "you@example.com")
	t.Setenv("EETMETER_ACCOUNT_A_PASSWORD", "hunter2")
	t.Setenv("EETMETER_ACCOUNT_B_EMAIL", "partner@example.com")
	t.Setenv("EETMETER_ACCOUNT_B_PASSWORD", "correcthorse")
}

func TestLoad_Defaults(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DailyTime != "06:00" || cfg.Timezone != "Europe/Amsterdam" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if h, m := cfg.DailyRun(); h != 6 || m != 0 {
		t.Fatalf("DailyRun = %d:%d", h, m)
	}
}

func TestLoad_ReportsAllMissing(t *testing.T) {
	// no env set
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing env")
	}
	for _, want := range []string{"DATABASE_URL", "EETMETER_ACCOUNT_A_EMAIL", "EETMETER_ACCOUNT_B_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestLoad_RejectsBadDailyTimeAndTZ(t *testing.T) {
	setValidEnv(t)
	t.Setenv("SYNC_DAILY_TIME", "25:00")
	t.Setenv("SYNC_TIMEZONE", "Mars/Olympus")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SYNC_DAILY_TIME") || !strings.Contains(err.Error(), "SYNC_TIMEZONE") {
		t.Fatalf("want both time and tz errors, got %v", err)
	}
}

func TestLoad_TokenModeNeedsNoEmailOrPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("EETMETER_ACCOUNT_A_TOKEN", "tok-a")
	t.Setenv("EETMETER_ACCOUNT_A_DEVICE_ID", "dev-a")
	t.Setenv("EETMETER_ACCOUNT_B_TOKEN", "tok-b")
	t.Setenv("EETMETER_ACCOUNT_B_DEVICE_ID", "dev-b")
	// no *_EMAIL, no *_PASSWORD

	cfg, err := Load()
	if err != nil {
		t.Fatalf("token-only config should be valid: %v", err)
	}
	if cfg.AccountA.Token != "tok-a" || cfg.AccountA.DeviceID != "dev-a" {
		t.Fatalf("account A = %+v", cfg.AccountA)
	}
	if cfg.AccountA.Email != "" || cfg.AccountA.Password != "" {
		t.Fatal("email/password should be empty and unused in token mode")
	}
}

func TestLoad_PasswordModeStillNeedsEmail(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("EETMETER_ACCOUNT_A_PASSWORD", "pw-a") // no email
	t.Setenv("EETMETER_ACCOUNT_B_TOKEN", "tok-b")
	t.Setenv("EETMETER_ACCOUNT_B_DEVICE_ID", "dev-b")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "account A") {
		t.Fatalf("password without email should be rejected, got %v", err)
	}
}

func TestLoad_RejectsSameEmail(t *testing.T) {
	setValidEnv(t)
	t.Setenv("EETMETER_ACCOUNT_B_EMAIL", "you@example.com")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("want same-email rejection, got %v", err)
	}
}

func TestConfig_NeverLeaksSecrets(t *testing.T) {
	setValidEnv(t)
	t.Setenv("SYNC_API_TOKEN", "topsecrettoken")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{cfg.String(), cfg.LogValue().String()} {
		for _, secret := range []string{"hunter2", "correcthorse", "secretpw", "topsecrettoken"} {
			if strings.Contains(rendered, secret) {
				t.Fatalf("rendered config leaked %q:\n%s", secret, rendered)
			}
		}
	}
}
