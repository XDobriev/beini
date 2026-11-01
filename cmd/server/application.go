package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"beini-backend/internal/catalog"
	"beini-backend/internal/config"
	"beini-backend/internal/handler"
	"beini-backend/internal/iiko"
	"beini-backend/internal/menusync"
	"beini-backend/internal/middleware"
	"beini-backend/internal/orderstate"
)

// stateDirectory разрешает только существующий каталог вне публичной статики,
// включая переходы через symlink/junction. Секретные данные не раздаются HTTP.
func stateDirectory(staticDir, stateDir string) (string, error) {
	if !filepath.IsAbs(stateDir) {
		return "", errors.New("BEINI_STATE_DIR должен быть абсолютным путём вне web root")
	}
	web, err := filepath.Abs(staticDir)
	if err != nil {
		return "", err
	}
	web, err = filepath.EvalSymlinks(web)
	if err != nil {
		return "", err
	}
	state, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(web, state)
	if err != nil {
		return "", err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return "", errors.New("BEINI_STATE_DIR находится внутри web root")
	}
	info, err := os.Stat(state)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("BEINI_STATE_DIR должен быть каталогом")
	}
	return state, nil
}

func newApplication(cfg *config.Config, logger *slog.Logger, staticDir, stateDir string) (http.Handler, func(), error) {
	dir, err := stateDirectory(staticDir, stateDir)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(filepath.Join(staticDir, "data", "menu_catalog.json"))
	if err != nil {
		return nil, nil, err
	}
	seed, err := catalog.ReadSeed(f)
	f.Close()
	if err != nil {
		return nil, nil, err
	}
	store, err := catalog.NewStore(2*time.Hour, 5*time.Minute)
	if err != nil {
		return nil, nil, err
	}
	client := iiko.NewClient(cfg, logger)
	syncer, err := menusync.New(menusync.Config{
		OrganizationID: cfg.OrganizationID, TerminalGroupID: cfg.TerminalGroupID,
		CachePath:    filepath.Join(dir, "menu-cache.json"),
		MenuInterval: 15 * time.Minute, StopListInterval: time.Minute, RequestTimeout: 20 * time.Second,
	}, &menusync.API{BaseURL: cfg.APIBaseURL, HTTP: &http.Client{Timeout: 20 * time.Second}, Token: client.GetToken}, store, seed)
	if err != nil {
		return nil, nil, err
	}
	if err := syncer.Restore(); err != nil {
		logger.Warn("кэш меню не восстановлен; заказ доступен только после успешной синхронизации")
	}
	states, err := orderstate.Open(filepath.Join(dir, "orders.jsonl"))
	if err != nil {
		return nil, nil, err
	}
	orders := handler.NewOrderHandlerWithState(client, logger, cfg.TestToken, store, states)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); syncer.Run(ctx) }()
	var once sync.Once
	closeApp := func() {
		once.Do(func() {
			cancel()
			<-done
			orders.Close()
			if err := states.Close(); err != nil {
				logger.Error("ошибка закрытия журнала", "error", err)
			}
		})
	}
	mux := http.NewServeMux()
	orderLimit := middleware.NewRateLimiter(10, time.Minute)
	statusLimit := middleware.NewRateLimiter(60, time.Minute)
	mux.HandleFunc("/api/health", orders.HealthCheck)
	mux.Handle("/api/menu", menusync.MenuHandler(store))
	mux.Handle("/api/order", middleware.RateLimit(orderLimit)(http.HandlerFunc(orders.CreateOrder)))
	mux.Handle("/api/order/status", middleware.RateLimit(statusLimit)(http.HandlerFunc(orders.OrderStatus)))
	mux.Handle("/api/test-order", middleware.RateLimit(orderLimit)(http.HandlerFunc(orders.SendTestOrder)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "эндпоинт API не найден"})
	})
	mux.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "demo.html"))
	})
	mux.Handle("/", safeFileServer(staticDir))
	var root http.Handler = mux
	root = middleware.NewCORS(cfg.AllowedOrigins)(root)
	root = middleware.Logger(logger)(root)
	root = middleware.Recoverer(logger)(root)
	return root, closeApp, nil
}
