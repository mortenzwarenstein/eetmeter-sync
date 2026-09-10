// Command fake-eetmeter is an in-memory stand-in for the Mijn Eetmeter API so
// the whole system can be run and demoed locally without real accounts. It
// implements only what internal/eetmeter calls, in the best-guess shapes from
// specs/001-recipe-sync/contracts/eetmeter-api.md. It is NOT a spec for the real
// API — the spike (task T001) confirms that.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/uid"
)

func main() {
	addr := flag.String("addr", ":8090", "listen address")
	seed := flag.Bool("seed", true, "seed each account with a couple of starter recipes")
	flag.Parse()

	srv := &server{accounts: map[string]*account{}}
	if *seed {
		srv.seed()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/account/credentials", srv.login)
	mux.HandleFunc("GET /api/combinedproduct", srv.list)
	mux.HandleFunc("POST /api/combinedproduct", srv.create)
	mux.HandleFunc("PUT /api/combinedproduct/{id}", srv.update)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	})

	log.Printf("fake-eetmeter listening on %s (accounts: %s)", *addr, strings.Join(srv.emails(), ", "))
	if err := http.ListenAndServe(*addr, logging(mux)); err != nil {
		log.Fatal(err)
	}
}

type account struct {
	email   string
	token   string
	recipes map[string]eetmeter.Recipe // id -> recipe
}

type server struct {
	mu       sync.Mutex
	accounts map[string]*account // token -> account, and email -> account (both keys)
}

func (s *server) seed() {
	oats := eetmeter.Recipe{
		Name: "Havermout", NumberOfPortions: 1,
		Items: []eetmeter.Ingredient{
			{ProductUnitID: "unit-oats", ProductName: "Havermout", UnitName: "gram", Amount: 60},
			{ProductUnitID: "unit-milk", ProductName: "Halfvolle melk", UnitName: "ml", Amount: 250},
		},
	}
	s.addAccount("you@example.com", oats)
	s.addAccount("partner@example.com")
}

func (s *server) addAccount(email string, recipes ...eetmeter.Recipe) {
	a := &account{email: email, recipes: map[string]eetmeter.Recipe{}}
	for _, r := range recipes {
		r.ID = uid.New()
		for i := range r.Items {
			r.Items[i].ID = uid.New()
		}
		a.recipes[r.ID] = r
	}
	s.accounts[email] = a
}

func (s *server) emails() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range s.accounts {
		if !seen[a.email] {
			seen[a.email] = true
			out = append(out, a.email)
		}
	}
	return out
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID     string `json:"deviceId"`
		EmailAddress string `json:"emailAddress"`
		Password     string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.accounts[body.EmailAddress]
	if a == nil || body.Password == "" || body.Password == "wrong" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	a.token = uid.New()
	s.accounts[a.token] = a
	writeJSON(w, map[string]any{"Token": a.token, "EmailAddress": a.email})
}

func (s *server) auth(w http.ResponseWriter, r *http.Request) *account {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := r.Header.Get("Authorization") // "Basic <token>:<deviceId>"
	h = strings.TrimPrefix(h, "Basic ")
	token, _, _ := strings.Cut(h, ":")
	a := s.accounts[token]
	if token == "" || a == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	return a
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	a := s.auth(w, r)
	if a == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]eetmeter.Recipe, 0, len(a.recipes))
	for _, r := range a.recipes {
		items = append(items, r)
	}
	writeJSON(w, map[string]any{"Items": items})
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	a := s.auth(w, r)
	if a == nil {
		return
	}
	var in eetmeter.Recipe
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = uid.New()
	for i := range in.Items {
		in.Items[i].ID = uid.New()
	}
	a.recipes[in.ID] = in
	writeJSON(w, in)
}

func (s *server) update(w http.ResponseWriter, r *http.Request) {
	a := s.auth(w, r)
	if a == nil {
		return
	}
	id := r.PathValue("id")
	var in eetmeter.Recipe
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := a.recipes[id]; !ok {
		http.Error(w, "no such recipe", http.StatusNotFound)
		return
	}
	in.ID = id
	for i := range in.Items {
		if in.Items[i].ID == "" {
			in.Items[i].ID = uid.New()
		}
	}
	a.recipes[id] = in
	writeJSON(w, in)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
