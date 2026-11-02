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
	"beini-backend/internal/notifier"
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
	if err := checkStateLocation(web, state); err != nil {
		return "", err
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

func checkStateLocation(web, state string) error {
	// EvalSymlinks can preserve Windows extended path prefixes. Compare the
	// equivalent ordinary paths so a prefix alone cannot evade containment.
	ordinary := func(path string) (string, error) {
		if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
			path = path[4:]
			if len(path) >= 4 && strings.EqualFold(path[:4], `UNC\`) {
				return `\\` + path[4:], nil
			}
			if len(path) < 3 || path[1] != ':' || path[2] != '\\' || !((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) {
				return "", errors.New("неподдерживаемый Windows device path для каталога состояния")
			}
		}
		return path, nil
	}
	var err error
	web, err = ordinary(web)
	if err != nil {
		return err
	}
	state, err = ordinary(state)
	if err != nil {
		return err
	}
	// Rel cannot represent a path on another Windows drive/share. Both paths
	// already exist and have been resolved before this check.
	if !strings.EqualFold(filepath.VolumeName(web), filepath.VolumeName(state)) {
		return nil
	}
	rel, err := filepath.Rel(web, state)
	if err != nil {
		return err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("BEINI_STATE_DIR находится внутри web root")
	}
	return nil
}

func newApplication(cfg *config.Config, logger *slog.Logger, staticDir, stateDir string) (http.Handler, func(), error) {
	var transport notifier.Transport
	if cfg.TelegramBotToken != "" || cfg.TelegramChatID != "" {
		telegram, err := notifier.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
		if err != nil {
			logger.Error("Telegram не настроен; события остаются в очереди")
		} else {
			transport = telegram
		}
	} else {
		logger.Info("Telegram отключён; события остаются в очереди")
	}
	return newApplicationWithTransport(cfg, logger, staticDir, stateDir, transport)
}

func newApplicationWithTransport(cfg *config.Config, logger *slog.Logger, staticDir, stateDir string, transport notifier.Transport) (http.Handler, func(), error) {
	if strings.TrimSpace(cfg.ExternalMenuID) == "" {
		return nil, nil, errors.New("задайте externalMenuId или IIKO_EXTERNAL_MENU_ID для меню iiko v3")
	}
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
		SourceID:       "v3:" + cfg.ExternalMenuID + ":" + cfg.PriceCategoryID,
		OrganizationID: cfg.OrganizationID, TerminalGroupID: cfg.TerminalGroupID,
		CachePath:    filepath.Join(dir, "menu-cache.json"),
		MenuInterval: 15 * time.Minute, StopListInterval: time.Minute, RequestTimeout: 20 * time.Second,
	}, &menusync.API{BaseURL: cfg.APIBaseURL, HTTP: &http.Client{Timeout: 20 * time.Second}, Token: client.GetToken, ExternalMenuID: cfg.ExternalMenuID, PriceCategoryID: cfg.PriceCategoryID}, store, seed)
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
	// Ошибка отдельного delivery-журнала не блокирует заказы и menu worker.
	notifyDone := make(chan struct{})
	var receipts *orderstate.Store
	if transport != nil {
		receipts, err = orderstate.Open(filepath.Join(dir, "notifications.jsonl"))
		if err == nil {
			// Случайная смена группы не должна отправить накопленные контакты в другой чат.
			var binding orderstate.Record
			binding, _, err = receipts.Reserve("telegram-destination", orderstate.Hash([]byte(cfg.TelegramChatID)), 0)
			receipts.Release(binding)
		}
		if err != nil {
			logger.Error("доставка уведомлений отключена: проверьте журнал и привязку группы")
			if receipts != nil {
				receipts.Close()
				receipts = nil
			}
		}
	}
	if receipts != nil {
		worker := notifier.New(states, receipts, transport, logger)
		go func() { defer close(notifyDone); worker.Run(ctx) }()
	} else {
		close(notifyDone)
	}
	var once sync.Once
	closeApp := func() {
		once.Do(func() {
			cancel()
			<-notifyDone
			if receipts != nil {
				if err := receipts.Close(); err != nil {
					logger.Error("ошибка закрытия журнала уведомлений")
				}
			}
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
