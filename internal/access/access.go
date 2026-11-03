// Package access controls online submission independently of catalog freshness.
package access

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionTTL = 8 * time.Hour
const csrfTTL = 2 * time.Hour

type Config struct {
	Mode         string
	PasswordHash string
	LocalHTTP    string
	Origins      []string
}

type Gate struct {
	config   Config
	now      func() time.Time
	secret   []byte
	mu       sync.Mutex
	sessions map[[32]byte]time.Time
	logins   []time.Time
	kdf      chan struct{}
}

func New(cfg Config) (*Gate, error) {
	if cfg.Mode == "" {
		cfg.Mode = "closed"
	}
	if cfg.Mode != "closed" && cfg.Mode != "tester" && cfg.Mode != "public" {
		return nil, errors.New("BEINI_ORDER_MODE: допустимы closed, tester, public")
	}
	if cfg.LocalHTTP != "" && cfg.LocalHTTP != "0" && cfg.LocalHTTP != "1" {
		return nil, errors.New("BEINI_TESTER_LOCAL_HTTP: допустимы 0 или 1")
	}
	if cfg.PasswordHash != "" {
		if _, _, err := parseHash(cfg.PasswordHash); err != nil {
			return nil, err
		}
	}
	if len(cfg.Origins) == 0 {
		cfg.Origins = []string{"https://beinirest.ru", "https://www.beinirest.ru"}
	}
	for _, origin := range cfg.Origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(cfg.LocalHTTP == "1" && u.Scheme == "http" && loopbackHost(u.Hostname()))) {
			return nil, errors.New("access: ALLOWED_ORIGINS должен содержать точные HTTPS origins")
		}
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, errors.New("не удалось инициализировать защиту доступа")
	}
	return &Gate{config: cfg, now: time.Now, secret: secret, sessions: make(map[[32]byte]time.Time), kdf: make(chan struct{}, 2)}, nil
}

func loopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func remoteLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && loopbackHost(host)
}
func (g *Gate) local(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return g.config.LocalHTTP == "1" && r.TLS == nil && loopbackHost(host) && remoteLoopback(r) && r.Header.Get("Forwarded") == "" && r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Real-IP") == "" && r.Header.Get("X-Forwarded-Proto") == ""
}
func (g *Gate) transportOK(r *http.Request) bool {
	if g.local(r) {
		return true
	}
	if r.TLS == nil && !(remoteLoopback(r) && r.Header.Get("X-Forwarded-Proto") == "https") {
		return false
	}
	for _, origin := range g.config.Origins {
		u, _ := url.Parse(origin)
		if u.Scheme == "https" && u.Host == r.Host {
			return true
		}
	}
	return false
}
func (g *Gate) cookieName(r *http.Request, kind string) string {
	if g.local(r) {
		return "beini_" + kind + "_dev"
	}
	return "__Host-beini_" + kind
}
func (g *Gate) cookie(w http.ResponseWriter, r *http.Request, kind, value string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if ttl < 0 {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: g.cookieName(r, kind), Value: value, Path: "/", HttpOnly: true, Secure: !g.local(r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: g.now().Add(ttl)})
}
func (g *Gate) sessionID(r *http.Request) string {
	c, err := r.Cookie(g.cookieName(r, "tester"))
	if err != nil || len(c.Value) != 43 {
		return ""
	}
	return c.Value
}
func (g *Gate) session(r *http.Request) (string, time.Time) {
	id := g.sessionID(r)
	if id == "" {
		return "", time.Time{}
	}
	key := sha256.Sum256([]byte(id))
	g.mu.Lock()
	defer g.mu.Unlock()
	expiry, exists := g.sessions[key]
	if !exists || !g.now().Before(expiry) {
		delete(g.sessions, key)
		return "", time.Time{}
	}
	return id, expiry
}
func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (g *Gate) sign(body, id string) string {
	h := hmac.New(sha256.New, g.secret)
	h.Write([]byte(body + "|" + id))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (g *Gate) validCSRF(token, id string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[0]) != 43 {
		return false
	}
	deadline, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || g.now().Unix() >= deadline || deadline > g.now().Add(csrfTTL).Unix()+1 {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(g.sign(parts[0]+"."+parts[1], id)))
}
func (g *Gate) csrf(w http.ResponseWriter, r *http.Request, id string) string {
	if c, err := r.Cookie(g.cookieName(r, "csrf")); err == nil && g.validCSRF(c.Value, id) {
		return c.Value
	}
	nonce := randomID()
	if nonce == "" {
		return ""
	}
	body := nonce + "." + strconv.FormatInt(g.now().Add(csrfTTL).Unix(), 10)
	token := body + "." + g.sign(body, id)
	g.cookie(w, r, "csrf", token, csrfTTL)
	return token
}
func jsonReply(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
func failure(w http.ResponseWriter, code int, key, message string) {
	jsonReply(w, code, map[string]any{"success": false, "code": key, "error": message})
}

func (g *Gate) Access(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		failure(w, 405, "method", "Разрешён только GET")
		return
	}
	if !g.transportOK(r) {
		failure(w, 503, "access_unavailable", "Защищённый доступ требует HTTPS")
		return
	}
	id, expiry := g.session(r)
	// Do not mutate the identity cookie on GET: a stale response from another
	// tab could otherwise erase the cookie set by a newer successful login.
	token := g.csrf(w, r, id)
	if token == "" {
		failure(w, 503, "access_unavailable", "Не удалось проверить доступ")
		return
	}
	var expires any
	if id != "" {
		expires = expiry.UTC().Format(time.RFC3339)
	}
	jsonReply(w, 200, map[string]any{"mode": g.config.Mode, "authenticated": id != "", "canOrder": g.config.Mode == "public" || (g.config.Mode == "tester" && id != ""), "loginAvailable": g.config.PasswordHash != "", "sessionExpiresAt": expires, "csrfToken": token, "mock": false})
}

func (g *Gate) stateRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		failure(w, 405, "method", "Разрешён только POST")
		return false
	}
	if !g.transportOK(r) {
		failure(w, 503, "access_unavailable", "Защищённый доступ требует HTTPS")
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		failure(w, 415, "content_type", "Требуется application/json")
		return false
	}
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	allowed := false
	for _, v := range g.config.Origins {
		if origin == v {
			allowed = true
		}
	}
	if g.local(r) && err == nil && u.Scheme == "http" && loopbackHost(u.Hostname()) {
		allowed = true
	}
	if err != nil || !allowed || u.Host != r.Host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		failure(w, 403, "csrf", "Проверка безопасности не пройдена. Обновите страницу.")
		return false
	}
	c, err := r.Cookie(g.cookieName(r, "csrf"))
	token := r.Header.Get("X-CSRF-Token")
	id, _ := g.session(r)
	if err != nil || token == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) != 1 || !g.validCSRF(token, id) {
		failure(w, 403, "csrf", "Проверка безопасности не пройдена. Обновите страницу.")
		return false
	}
	return true
}

func (g *Gate) Login(w http.ResponseWriter, r *http.Request) {
	if !g.stateRequest(w, r) {
		return
	}
	if g.config.PasswordHash == "" {
		failure(w, 503, "login_unavailable", "Вход владельца не настроен")
		return
	}
	g.mu.Lock()
	now := g.now()
	recent := g.logins[:0]
	for _, at := range g.logins {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	g.logins = recent
	if len(g.logins) >= 5 {
		g.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "login_limited", "Слишком много попыток входа. Подождите минуту.")
		return
	}
	g.logins = append(g.logins, now)
	g.mu.Unlock()
	select {
	case g.kdf <- struct{}{}:
		defer func() { <-g.kdf }()
	default:
		w.Header().Set("Retry-After", "5")
		failure(w, 429, "login_limited", "Подождите перед повторным входом.")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		failure(w, 400, "invalid_login", "Некорректный запрос входа")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || !CheckPassword(g.config.PasswordHash, body.Password) {
		failure(w, 401, "invalid_login", "Неверный пароль")
		return
	}
	id := randomID()
	if id == "" {
		failure(w, 503, "login_unavailable", "Не удалось создать сессию")
		return
	}
	g.mu.Lock()
	delete(g.sessions, sha256.Sum256([]byte(g.sessionID(r))))
	for k, expiry := range g.sessions {
		if !now.Before(expiry) {
			delete(g.sessions, k)
		}
	}
	// Bound memory even when the valid owner repeatedly starts new browsers.
	if len(g.sessions) >= 32 {
		var oldest [32]byte
		var at time.Time
		for k, v := range g.sessions {
			if at.IsZero() || v.Before(at) {
				oldest = k
				at = v
			}
		}
		delete(g.sessions, oldest)
	}
	g.sessions[sha256.Sum256([]byte(id))] = now.Add(sessionTTL)
	g.mu.Unlock()
	g.cookie(w, r, "tester", id, sessionTTL)
	// New CSRF is bound to the new session; the old pre-login token cannot mutate it.
	token := g.csrf(w, r, id)
	jsonReply(w, 200, map[string]any{"success": true, "csrfToken": token})
}

func (g *Gate) Logout(w http.ResponseWriter, r *http.Request) {
	if !g.stateRequest(w, r) {
		return
	}
	g.mu.Lock()
	delete(g.sessions, sha256.Sum256([]byte(g.sessionID(r))))
	g.mu.Unlock()
	g.cookie(w, r, "tester", "", -time.Hour)
	g.cookie(w, r, "csrf", "", -time.Hour)
	jsonReply(w, 200, map[string]any{"success": true})
}

func (g *Gate) ProtectOrder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Cookie")
		id, _ := g.session(r)
		if g.config.Mode == "closed" || (g.config.Mode == "tester" && id == "") {
			failure(w, 403, "ordering_closed", "Онлайн-отправка временно закрыта. Позвоните в ресторан или войдите как тестировщик.")
			return
		}
		if !g.stateRequest(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
