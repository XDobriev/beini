package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"beini-backend/internal/catalog"
	"beini-backend/internal/config"
	"beini-backend/internal/handler"
	"beini-backend/internal/idempotency"
	"beini-backend/internal/iiko"
	"beini-backend/internal/middleware"
)

// safeFileServer раздаёт статические файлы фронтенда, блокируя доступ к конфигам, коду и системным файлам,
// и обеспечивает корректный 404 fallback для навигации.
func safeFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(filepath.FromSlash(r.URL.Path))
		base := filepath.Base(clean)

		// Разрешаем обращение к корню сайта (отдаёт index.html)
		if clean == `\` || clean == "/" || clean == "." || base == "." {
			fs.ServeHTTP(w, r)
			return
		}

		// Защита от скрытых файлов (.git, .env) и служебных форматов (конфиги, исходники, бинарники, скрипты)
		lowerBase := strings.ToLower(base)
		if strings.HasPrefix(base, ".") ||
			strings.HasSuffix(lowerBase, ".json") ||
			strings.HasSuffix(lowerBase, ".go") ||
			strings.HasSuffix(lowerBase, ".mod") ||
			strings.HasSuffix(lowerBase, ".sum") ||
			strings.HasSuffix(lowerBase, ".exe") ||
			strings.HasSuffix(lowerBase, ".md") ||
			strings.HasSuffix(lowerBase, ".py") ||
			lowerBase == "dockerfile" ||
			lowerBase == "docker-compose.yml" {
			http.NotFound(w, r)
			return
		}

		// Проверяем наличие запрашиваемого файла на диске
		fullPath := filepath.Join(dir, clean)
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		// Для статических ассетов с расширением (.css, .js, .png, .jpg и т.д.) отдаём честный 404
		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}

		// Fallback для навигации без расширения (SPA landing fallback): отдаём index.html
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

func main() {
	// 1. Инициализация структурированного логгера log/slog
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("запуск бэкенд-сервиса ресторана BEINI (iikoCloud Gateway)")

	// 2. Загрузка конфигурации (файл iiko_config.json с автоматическим fallback на переменные окружения)
	cfg, err := config.Load("iiko_config.json")
	if err != nil {
		logger.Error("фатальная ошибка конфигурации", "error", err)
		os.Exit(1)
	}

	// 3. Директория со статическими файлами фронтенда
	staticDir := "."
	if envStatic := os.Getenv("STATIC_DIR"); envStatic != "" {
		staticDir = envStatic
	}

	// 4. Инициализация слоя интеграции с iiko
	iikoClient := iiko.NewClient(cfg, logger)

	// 5. Инициализация серверного каталога блюд и защиты от дублей
	beiniCatalog := catalog.DefaultBeiniCatalog()
	idempotencyStore := idempotency.NewStore(30 * time.Minute)

	// 6. Инициализация HTTP хендлеров
	orderHandler := handler.NewOrderHandlerWithDeps(iikoClient, logger, cfg.TestToken, beiniCatalog, idempotencyStore)

	// 7. Ограничитель частоты запросов для оформления заказов (10 заказов в минуту на IP)
	orderRateLimiter := middleware.NewRateLimiter(10, time.Minute)

	// 8. Настройка роутинга (Go 1.22+ ServeMux)
	mux := http.NewServeMux()

	// API эндпоинты
	mux.HandleFunc("/api/health", orderHandler.HealthCheck)
	mux.Handle("/api/order", middleware.RateLimit(orderRateLimiter)(http.HandlerFunc(orderHandler.CreateOrder)))
	mux.HandleFunc("/api/test-order", orderHandler.SendTestOrder)

	// Корректный JSON 404 fallback для любых несуществующих /api/* маршрутов
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "эндпоинт API не найден",
		})
	})

	// Служебная страница проверки кассы (изолирована от основного сайта)
	mux.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "demo.html"))
	})

	// Статический фронтенд (index.html, css, js, assets) с защитой служебных файлов и 404 fallback
	mux.Handle("/", safeFileServer(staticDir))

	// 9. Применение цепочки middleware: Recoverer -> Logger -> CORS -> Mux
	var rootHandler http.Handler = mux
	rootHandler = middleware.NewCORS(cfg.AllowedOrigins)(rootHandler)
	rootHandler = middleware.Logger(logger)(rootHandler)
	rootHandler = middleware.Recoverer(logger)(rootHandler)

	// 8. Конфигурация HTTP сервера с production-таймаутами
	serverAddr := ":" + cfg.Port
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 45 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 9. Запуск сервера в отдельной горутине
	go func() {
		logger.Info("сервер успешно запущен",
			"addr", "http://localhost"+serverAddr,
			"port", cfg.Port,
			"staticDir", staticDir,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("ошибка работы HTTP сервера", "error", err)
			os.Exit(1)
		}
	}()

	// 10. Graceful Shutdown по системным сигналам (SIGINT, SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	logger.Info("получен сигнал завершения работы, остановка сервера...")

	ctxShutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctxShutdown); err != nil {
		logger.Error("принудительная остановка сервера", "error", err)
	} else {
		logger.Info("сервер остановлен корректно (graceful shutdown)")
	}
}
