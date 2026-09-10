package eetmeter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New("you@example.com", "pw", Options{BaseURL: srv.URL + "/api"})
}

func TestLogin_SendsCredentialsAndStoresToken(t *testing.T) {
	var gotBody loginRequest
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/account/credentials" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("version") == "" || r.Header.Get("platform") == "" {
			t.Errorf("missing version/platform headers")
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"token":"tok-123"}`))
	})

	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if gotBody.EmailAddress != "you@example.com" || gotBody.Password != "pw" || gotBody.DeviceID == "" {
		t.Fatalf("login body = %+v", gotBody)
	}
	if c.token != "tok-123" {
		t.Fatalf("token not stored, got %q", c.token)
	}
}

func TestAuthHeader_IsTokenColonDevice(t *testing.T) {
	var gotAuth string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/account/credentials":
			w.Write([]byte(`{"token":"T"}`))
		default:
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte(`{"Items":[]}`))
		}
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListRecipes(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "Basic T:" + c.deviceID
	if gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestDo_MapsUnauthorizedToErrAuth(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	err := c.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("want ErrAuth, got %v", err)
	}
}

func TestDo_RetriesOnceOn500(t *testing.T) {
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/account/credentials" {
			w.Write([]byte(`{"token":"T"}`))
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"Items":[]}`))
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListRecipes(context.Background()); err != nil {
		t.Fatalf("ListRecipes after one 502: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

func TestListRecipes_ParsesEnvelope(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/account/credentials" {
			w.Write([]byte(`{"token":"T"}`))
			return
		}
		io.WriteString(w, `{"Items":[
			{"Id":"r1","Name":"Bami","NumberOfPortions":4,"Items":[
				{"Id":"l1","ProductUnitId":"u1","ProductName":"Mix","UnitName":"gram","Amount":300}
			]}
		]}`)
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := c.ListRecipes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Bami" || got[0].NumberOfPortions != 4 || len(got[0].Items) != 1 {
		t.Fatalf("parsed recipe = %+v", got)
	}
	if got[0].Items[0].ProductUnitID != "u1" || got[0].Items[0].Amount != 300 {
		t.Fatalf("parsed ingredient = %+v", got[0].Items[0])
	}
}
