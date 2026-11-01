package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"beini-backend/internal/config"
)

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	indexPath := filepath.Join(dir, "index.html")
	f, err := os.Open(indexPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), f)
}

// safeFileServer раздаёт статические файлы фронтенда, блокируя доступ к конфигам, коду и системным файлам,
// и обеспечивает корректный 404 fallback для навигации.
func safeFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(filepath.FromSlash(r.URL.Path))

		// Разрешаем прямое обращение к корню сайта и index.html без 301 редиректа
		base := filepath.Base(clean)
		if clean == `\` || clean == "/" || clean == "." || strings.EqualFold(base, "index.html") {
			serveIndex(w, r, dir)
			return
		}

		// Разбиваем путь на сегменты и проверяем каждый сегмент
		segments := strings.FieldsFunc(clean, func(c rune) bool {
			return c == '/' || c == '\\'
		})

		for _, seg := range segments {
			lowerSeg := strings.ToLower(seg)
			// Блокируем любые скрытые папки и файлы (.git, .env, .github и т.д.)
			if strings.HasPrefix(seg, ".") {
				http.NotFound(w, r)
				return
			}
			// Блокируем доступ к служебным директориям бэкенда
			if lowerSeg == "internal" || lowerSeg == "cmd" || lowerSeg == "tools" || lowerSeg == "scratch" || lowerSeg == "data" {
				http.NotFound(w, r)
				return
			}
		}

		// Защита от служебных форматов (конфиги, исходники, бинарники, скрипты)
		lowerBase := strings.ToLower(base)
		if strings.HasSuffix(lowerBase, ".json") ||
			strings.HasSuffix(lowerBase, ".go") ||
			strings.HasSuffix(lowerBase, ".mod") ||
			strings.HasSuffix(lowerBase, ".sum") ||
			strings.HasSuffix(lowerBase, ".exe") ||
			strings.HasSuffix(lowerBase, ".md") ||
			strings.HasSuffix(lowerBase, ".py") ||
			strings.HasSuffix(lowerBase, ".yml") ||
			strings.HasSuffix(lowerBase, ".yaml") ||
			strings.HasSuffix(lowerBase, ".toml") ||
			strings.HasSuffix(lowerBase, ".sh") ||
			strings.HasSuffix(lowerBase, ".bat") ||
			strings.HasSuffix(lowerBase, ".ps1") ||
			lowerBase == "dockerfile" ||
			strings.HasPrefix(lowerBase, "docker-compose") {
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
		serveIndex(w, r, dir)
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

	rootHandler, closeApp, err := newApplication(cfg, logger, staticDir, os.Getenv("BEINI_STATE_DIR"))
	if err != nil {
		logger.Error("ошибка инициализации приложения", "error", err)
		os.Exit(1)
	}
	defer closeApp()

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
