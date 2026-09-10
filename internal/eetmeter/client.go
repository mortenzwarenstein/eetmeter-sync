package eetmeter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/uid"
)

// ErrAuth is returned when the API rejects the credentials or token (HTTP 401 or
// 403). A run that sees this aborts without writing anything.
var ErrAuth = errors.New("eetmeter: authentication failed")

// DefaultBaseURL is the production Mijn Eetmeter API root.
const DefaultBaseURL = "https://api3-mijn.voedingscentrum.nl/api/"

// Options configures a Client. Zero values fall back to sensible defaults.
type Options struct {
	BaseURL    string
	AppVersion string
	Platform   string
	HTTPClient *http.Client
	// PerCall bounds a single HTTP request (default 30s).
	PerCall time.Duration

	// Token + DeviceID: a device-bound pair captured from the app. When both are
	// set the client uses them directly and Login is a no-op — this is the
	// supported auth path, because the API's device-registration login cannot be
	// reproduced from a server (a bare email/password login returns 404).
	Token    string
	DeviceID string
}

// Client talks to one Mijn Eetmeter account. It is not safe for concurrent use;
// a sync creates one Client per account and uses each sequentially.
type Client struct {
	base     string
	appVer   string
	platform string
	http     *http.Client
	perCall  time.Duration

	email    string
	password string
	deviceID string
	token    string
}

// New returns a Client for the given account. If opt.Token+opt.DeviceID are set
// the client is ready immediately; otherwise call Login first.
func New(email, password string, opt Options) *Client {
	c := &Client{
		base:     strings.TrimRight(orDefault(opt.BaseURL, DefaultBaseURL), "/") + "/",
		appVer:   orDefault(opt.AppVersion, "4.6.0"),
		platform: orDefault(opt.Platform, "iOS"),
		http:     opt.HTTPClient,
		perCall:  opt.PerCall,
		email:    email,
		password: password,
		deviceID: orDefault(opt.DeviceID, uid.New()),
		token:    opt.Token,
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.perCall == 0 {
		c.perCall = 30 * time.Second
	}
	return c
}

// HasToken reports whether the client already holds a usable token.
func (c *Client) HasToken() bool { return c.token != "" }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Login exchanges the account credentials for a token and stores it for
// subsequent calls. A wrong email/password yields ErrAuth. If the client was
// created with a Token+DeviceID, Login is a no-op.
func (c *Client) Login(ctx context.Context) error {
	if c.token != "" {
		return nil
	}
	body := loginRequest{DeviceID: c.deviceID, EmailAddress: c.email, Password: c.password}
	var resp loginResponse
	if err := c.do(ctx, http.MethodPost, "account/credentials", body, &resp); err != nil {
		return err
	}
	if resp.Token == "" {
		return errors.New("eetmeter: login succeeded but no token was returned")
	}
	c.token = resp.Token
	return nil
}

// do performs one API call with the shared headers, a per-call timeout, and a
// single retry on a transport error or 5xx. It never retries a 4xx.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("eetmeter: encode %s %s: %w", method, path, err)
		}
		body = b
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, c.perCall)
		req, err := http.NewRequestWithContext(callCtx, method, c.base+path, bytes.NewReader(body))
		if err != nil {
			cancel()
			return fmt.Errorf("eetmeter: build %s %s: %w", method, path, err)
		}
		req.Header.Set("version", c.appVer)
		req.Header.Set("platform", c.platform)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			// NOTE: not RFC-2617 Basic auth — the literal string "token:deviceId".
			req.Header.Set("Authorization", "Basic "+c.token+":"+c.deviceID)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("eetmeter: %s %s: %w", method, path, err)
			continue // transport error — retry once
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		switch {
		case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
			return ErrAuth
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("eetmeter: %s %s: server status %d: %s", method, path, resp.StatusCode, snippet(data))
			continue // 5xx — retry once
		case resp.StatusCode >= 400:
			return fmt.Errorf("eetmeter: %s %s: status %d: %s", method, path, resp.StatusCode, snippet(data))
		}

		if readErr != nil {
			return fmt.Errorf("eetmeter: %s %s: read body: %w", method, path, readErr)
		}
		if out == nil || len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("eetmeter: %s %s: decode body: %w", method, path, err)
		}
		return nil
	}
	return lastErr
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
