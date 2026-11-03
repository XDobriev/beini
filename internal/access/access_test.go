package access

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testOrigin = "https://beinirest.ru"

func testGate(t *testing.T, mode string) *Gate {
	t.Helper()
	hash, err := HashPassword("offline-password-only")
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(Config{Mode: mode, PasswordHash: hash, Origins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

type browser struct {
	cookies map[string]*http.Cookie
	csrf    string
}

func (b *browser) call(g *Gate, path, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", b.csrf)
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	switch path {
	case "/api/access":
		g.Access(w, r)
	case "/api/tester/login":
		g.Login(w, r)
	case "/api/tester/logout":
		g.Logout(w, r)
	default:
		g.ProtectOrder(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })).ServeHTTP(w, r)
	}
	if b.cookies == nil {
		b.cookies = map[string]*http.Cookie{}
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	var data struct {
		CSRFToken string `json:"csrfToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	if data.CSRFToken != "" {
		b.csrf = data.CSRFToken
	}
	return w
}

func TestPasswordAndFailClosedConfig(t *testing.T) {
	h, err := HashPassword("offline-password-only")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "offline-password-only") || CheckPassword(h, "wrong") {
		t.Fatal("password verification")
	}
	if strings.Contains(h, "offline-password-only") {
		t.Fatal("plaintext hash")
	}
	for _, cfg := range []Config{{Mode: "typo"}, {Mode: "tester", PasswordHash: "bad"}, {LocalHTTP: "true"}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	g, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	b := &browser{}
	if w := b.call(g, "/api/order", "POST", "{}"); w.Code != 403 {
		t.Fatalf("default gate %d", w.Code)
	}
	if w := b.call(g, "/api/tester/login", "POST", `{"password":"x"}`); w.Code == 200 {
		t.Fatal("missing hash opened login")
	}
}

func TestModesSessionExpiryForgeryLogoutAndRestart(t *testing.T) {
	for _, mode := range []string{"closed", "tester", "public"} {
		t.Run(mode, func(t *testing.T) {
			g := testGate(t, mode)
			b := &browser{}
			now := time.Now()
			g.now = func() time.Time { return now }
			w := b.call(g, "/api/access", "GET", "")
			if w.Header().Get("Cache-Control") != "no-store" || b.csrf == "" {
				t.Fatal("shared/no CSRF access")
			}
			code := 403
			if mode == "public" {
				code = 201
			}
			if w = b.call(g, "/api/order", "POST", "{}"); w.Code != code {
				t.Fatalf("guest %d", w.Code)
			}
			if w = b.call(g, "/api/tester/login", "POST", `{"password":"wrong"}`); w.Code != 401 {
				t.Fatalf("wrong password %d", w.Code)
			}
			if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
				t.Fatalf("login %d %s", w.Code, w.Body)
			}
			session := b.cookies["__Host-beini_tester"]
			if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.Domain != "" {
				t.Fatal("unsafe cookie")
			}
			code = 201
			if mode == "closed" {
				code = 403
			}
			if w = b.call(g, "/api/order", "POST", "{}"); w.Code != code {
				t.Fatalf("owner %d %s", w.Code, w.Body)
			}
			stolen := *session
			if w = b.call(g, "/api/tester/logout", "POST", "{}"); w.Code != 200 {
				t.Fatalf("logout %d", w.Code)
			}
			b.cookies[stolen.Name] = &stolen
			if w = b.call(g, "/api/access", "GET", ""); strings.Contains(w.Body.String(), `"authenticated":true`) {
				t.Fatal("logout failed to revoke")
			}
			b.cookies[stolen.Name] = &http.Cookie{Name: stolen.Name, Value: strings.Repeat("a", 64)}
			if w = b.call(g, "/api/access", "GET", ""); strings.Contains(w.Body.String(), `"authenticated":true`) {
				t.Fatal("forged session")
			}
			if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
				t.Fatalf("login before expiry %d", w.Code)
			}
			now = now.Add(9 * time.Hour)
			if w = b.call(g, "/api/access", "GET", ""); strings.Contains(w.Body.String(), `"authenticated":true`) {
				t.Fatal("expired session")
			}
			now = now.Add(time.Minute)
			if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
				t.Fatalf("login before restart %d", w.Code)
			}
			next, err := New(g.config)
			if err != nil {
				t.Fatal(err)
			}
			if w = b.call(next, "/api/access", "GET", ""); strings.Contains(w.Body.String(), `"authenticated":true`) {
				t.Fatal("session survived restart")
			}
		})
	}
}

func TestProxyTLSAndKDFCapacity(t *testing.T) {
	g := testGate(t, "tester")
	for _, test := range []struct {
		remote, proto string
		code          int
	}{
		{"127.0.0.1:1234", "https", 200},
		{"[::1]:1234", "https", 200},
		{"127.0.0.1:1234", "http", 503},
		{"127.0.0.1:1234", "", 503},
		{"203.0.113.1:1234", "https", 503},
	} {
		r := httptest.NewRequest("GET", "http://beinirest.ru/api/access", nil)
		r.RemoteAddr = test.remote
		r.Header.Set("X-Forwarded-Proto", test.proto)
		w := httptest.NewRecorder()
		g.Access(w, r)
		if w.Code != test.code {
			t.Fatalf("proxy remote=%s proto=%s: %d", test.remote, test.proto, w.Code)
		}
		for _, cookie := range w.Result().Cookies() {
			if !cookie.Secure || !cookie.HttpOnly || !strings.HasPrefix(cookie.Name, "__Host-") {
				t.Fatal("proxy weakened cookie")
			}
		}
	}
	b := &browser{}
	b.call(g, "/api/access", "GET", "")
	for i := 0; i < cap(g.kdf); i++ {
		g.kdf <- struct{}{}
	}
	w := b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`)
	if w.Code != 429 || len(g.sessions) != 0 {
		t.Fatal("parallel KDF capacity not enforced")
	}
	for i := 0; i < cap(g.kdf); i++ {
		<-g.kdf
	}
	if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
		t.Fatal("KDF slot not reusable")
	}
	if strings.Contains(w.Body.String(), g.config.PasswordHash) || strings.Contains(w.Body.String(), "offline-password-only") || strings.Contains(w.Body.String(), b.cookies["__Host-beini_tester"].Value) {
		t.Fatal("credential leaked in response body")
	}
}

func TestStaleAccessReadDoesNotClearNewSessionCookie(t *testing.T) {
	g := testGate(t, "tester")
	r := httptest.NewRequest("GET", testOrigin+"/api/access", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-beini_tester", Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	g.Access(w, r)
	// This old anonymous response may arrive after another tab's successful login.
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "__Host-beini_tester" {
			t.Fatal("stale GET can overwrite a newer login cookie")
		}
	}
	b := &browser{cookies: map[string]*http.Cookie{"__Host-beini_tester": {Name: "__Host-beini_tester", Value: strings.Repeat("a", 43)}}}
	b.call(g, "/api/access", "GET", "")
	if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
		t.Fatalf("restart cookie prevented login: %d %s", w.Code, w.Body)
	}
}

func TestCSRFAndLoginRateLimit(t *testing.T) {
	g := testGate(t, "tester")
	b := &browser{}
	b.call(g, "/api/access", "GET", "")
	good := b.csrf
	for _, token := range []string{"", "forged"} {
		b.csrf = token
		if w := b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 403 {
			t.Fatal("CSRF login")
		}
	}
	b.csrf = good
	for i := 0; i < 5; i++ {
		if w := b.call(g, "/api/tester/login", "POST", `{"password":"wrong"}`); w.Code != 401 {
			t.Fatalf("attempt %d %d", i, w.Code)
		}
	}
	w := b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("no brute force protection")
	}
	g.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if w = b.call(g, "/api/tester/login", "POST", `{"password":"offline-password-only"}`); w.Code != 200 {
		t.Fatalf("limiter never releases %d", w.Code)
	}
	for _, endpoint := range []string{"/api/order", "/api/tester/logout"} {
		b.csrf = ""
		if w = b.call(g, endpoint, "POST", "{}"); w.Code != 403 {
			t.Fatal("missing state CSRF")
		}
	}
	b.call(g, "/api/access", "GET", "")
	for _, origin := range []string{"https://www.beinirest.ru", "https://evil.invalid", "null", ""} {
		r := httptest.NewRequest("POST", testOrigin+"/api/tester/logout", strings.NewReader("{}"))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", b.csrf)
		for _, c := range b.cookies {
			r.AddCookie(c)
		}
		w = httptest.NewRecorder()
		g.Logout(w, r)
		if w.Code != 403 {
			t.Fatalf("origin accepted %q", origin)
		}
	}
}

func TestExplicitLocalHTTPDoesNotWeakenProduction(t *testing.T) {
	for _, enabled := range []string{"", "1"} {
		g, err := New(Config{Mode: "public", LocalHTTP: enabled})
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"http://127.0.0.1:8080/api/access", "http://beinirest.ru/api/access"} {
			r := httptest.NewRequest("GET", target, nil)
			r.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			g.Access(w, r)
			want := 503
			if enabled == "1" && strings.Contains(target, "127.0.0.1") {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("local=%s target=%s code=%d", enabled, target, w.Code)
			}
			if want == 200 {
				r.Header.Set("X-Forwarded-For", "1.2.3.4")
				w = httptest.NewRecorder()
				g.Access(w, r)
				if w.Code != 503 {
					t.Fatal("proxy headers enabled HTTP exception")
				}
			}
		}
	}
}
