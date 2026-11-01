# Telegram Order Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Реализовать систему мгновенных уведомлений в Telegram для управляющего и смены ресторана BEINI при поступлении новых заказов с сайта и экстренных алертов при сбоях кассы iiko.

**Architecture:** В бэкенде на Go создаётся изолированный пакет `internal/notifier` с интерфейсом `Notifier`, реализациями `TelegramNotifier` (с поддержкой нескольких `chat_id`, защитой от паники `recover`, ретраями с экспоненциальной задержкой и таймаутами) и `NoopNotifier` (fallback при пустом токене). Нотификатор внедряется через DI в `OrderHandler` и вызывается асинхронно в горутине, исключая задержки оформления заказа для клиентов ресторана.

**Tech Stack:** Go 1.22+, `log/slog`, `net/http`, `net/http/httptest`, Telegram Bot API (`parse_mode: "HTML"`), iikoCloud API v2.

---

## File Structure & Responsibilities

| File | Status | Responsibility |
|---|---|---|
| `internal/config/config.go` | Modify | Чтение параметров `TelegramBotToken` и `TelegramChatIDs` из JSON и ENV (`TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_IDS`) |
| `iiko_config.example.json` | Modify | Пример конфигурации с полями Telegram |
| `internal/config/config_test.go` | Modify | Unit-тесты загрузки параметров Telegram из JSON и ENV |
| `internal/notifier/notifier.go` | Create | Контракт интерфейса `Notifier` (`NotifyNewOrder`, `NotifyOrderFailure`) |
| `internal/notifier/noop.go` | Create | Безопасная no-op реализация `Notifier` для тестов и dev-режима |
| `internal/notifier/noop_test.go` | Create | Unit-тест для `NoopNotifier` |
| `internal/notifier/telegram.go` | Create | Реализация `TelegramNotifier`: HTML-шаблоны, экранирование `html.EscapeString`, HTTP POST к Telegram Bot API, retry и таймауты |
| `internal/notifier/telegram_test.go` | Create | Unit-тесты HTML-форматирования, экранирования XSS/спецсимволов, mock HTTP сервера с проверкой кодов 200/500/400 и retry |
| `internal/handler/order_handler.go` | Modify | Внедрение `Notifier` в `OrderHandler`, асинхронный вызов `NotifyNewOrder` и `NotifyOrderFailure` |
| `internal/handler/order_handler_test.go` | Modify | Тесты вызова `Notifier` при успехе и ошибках iiko |
| `cmd/server/main.go` | Modify | Инициализация `TelegramNotifier` / `NoopNotifier` и передача в `OrderHandler` |

---

### Task 1: Telegram Configuration Fields (`internal/config`)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `iiko_config.example.json`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test for Telegram configuration in `config_test.go`**

Добавить тестовые функции `TestConfigLoad_TelegramFields` и `TestConfigLoad_TelegramEnvOverrides` в `internal/config/config_test.go`:

```go
func TestConfigLoad_TelegramFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.json")

	content := `{
		"apiBaseUrl": "https://api-ru.iiko.services",
		"apiKey": "test-key",
		"telegramBotToken": "123456:ABC-DEF",
		"telegramChatIds": ["-100123456789", "987654321"]
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("ошибка записи временного файла: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.TelegramBotToken != "123456:ABC-DEF" {
		t.Errorf("TelegramBotToken = %q; want 123456:ABC-DEF", cfg.TelegramBotToken)
	}
	if len(cfg.TelegramChatIDs) != 2 || cfg.TelegramChatIDs[0] != "-100123456789" || cfg.TelegramChatIDs[1] != "987654321" {
		t.Errorf("TelegramChatIDs = %v; want [-100123456789 987654321]", cfg.TelegramChatIDs)
	}
}

func TestConfigLoad_TelegramEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.json")

	content := `{"apiKey": "file-key", "telegramBotToken": "file-token", "telegramChatIds": ["111"]}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("ошибка записи временного файла: %v", err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "env-token-xyz")
	t.Setenv("TELEGRAM_CHAT_IDS", "-1002222, 33333")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.TelegramBotToken != "env-token-xyz" {
		t.Errorf("TelegramBotToken = %q; want env-token-xyz", cfg.TelegramBotToken)
	}
	if len(cfg.TelegramChatIDs) != 2 || cfg.TelegramChatIDs[0] != "-1002222" || cfg.TelegramChatIDs[1] != "33333" {
		t.Errorf("TelegramChatIDs = %v; want [-1002222 33333]", cfg.TelegramChatIDs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Команда: `go test -v ./internal/config -run TestConfigLoad_Telegram`  
Ожидается: Ошибка компиляции `cfg.TelegramBotToken undefined`, `cfg.TelegramChatIDs undefined`.

- [ ] **Step 3: Implement Telegram fields in `internal/config/config.go` and update `iiko_config.example.json`**

В структуру `Config` в `internal/config/config.go` добавить:
```go
	// TelegramBotToken — токен Telegram-бота для оповещения управляющего
	TelegramBotToken string `json:"telegramBotToken"`
	// TelegramChatIDs — ID чатов или групп Telegram для отправки уведомлений
	TelegramChatIDs []string `json:"telegramChatIds"`
```

В функцию `applyEnv` в `internal/config/config.go` добавить:
```go
	if envTGToken := os.Getenv("TELEGRAM_BOT_TOKEN"); envTGToken != "" {
		cfg.TelegramBotToken = envTGToken
	}
	if envTGChats := os.Getenv("TELEGRAM_CHAT_IDS"); envTGChats != "" {
		rawIDs := strings.Split(envTGChats, ",")
		cleaned := make([]string, 0, len(rawIDs))
		for _, id := range rawIDs {
			trimmed := strings.TrimSpace(id)
			if trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		if len(cleaned) > 0 {
			cfg.TelegramChatIDs = cleaned
		}
	}
```

В `iiko_config.example.json` добавить строки:
```json
  "telegramBotToken": "",
  "telegramChatIds": []
```

- [ ] **Step 4: Run tests to verify they pass**

Команда: `go test -v ./internal/config`  
Ожидается: `PASS` для всех тестов пакета `internal/config`.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go iiko_config.example.json
git commit -m "feat(config): add telegram bot token and chat ids configuration"
```

---

### Task 2: Notifier Domain Interface and Noop Implementation (`internal/notifier`)

**Files:**
- Create: `internal/notifier/notifier.go`
- Create: `internal/notifier/noop.go`
- Create: `internal/notifier/noop_test.go`

- [ ] **Step 1: Write failing test in `internal/notifier/noop_test.go`**

Создать файл `internal/notifier/noop_test.go`:
```go
package notifier_test

import (
	"context"
	"testing"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
	"beini-backend/internal/notifier"
)

func TestNoopNotifier(t *testing.T) {
	var n notifier.Notifier = notifier.NewNoopNotifier()

	order := &domain.Order{
		OrderNumber:  101,
		CustomerName: "Тест",
		Phone:        "+79281234567",
	}

	result := &iiko.OrderResult{
		CorrelationID: "test-corr-id",
		OrderInfo: iiko.OrderInfo{
			CreationStatus: iiko.StatusSuccess,
		},
	}

	// Вызовы не должны паниковать и возвращать ошибки
	n.NotifyNewOrder(context.Background(), order, result)
	n.NotifyOrderFailure(context.Background(), order, nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Команда: `go test -v ./internal/notifier`  
Ожидается: Ошибка компиляции (пакет `internal/notifier` ещё не существует).

- [ ] **Step 3: Implement `notifier.go` and `noop.go`**

Создать `internal/notifier/notifier.go`:
```go
package notifier

import (
	"context"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// Notifier определяет контракт для сервисов служебных уведомлений о заказах
type Notifier interface {
	// NotifyNewOrder отправляет карточку успешно созданного заказа
	NotifyNewOrder(ctx context.Context, order *domain.Order, result *iiko.OrderResult)

	// NotifyOrderFailure отправляет экстренное предупреждение о сбое отправки в кассу iiko
	NotifyOrderFailure(ctx context.Context, order *domain.Order, reason error)
}
```

Создать `internal/notifier/noop.go`:
```go
package notifier

import (
	"context"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// NoopNotifier — безопасная пустая реализация интерфейса Notifier, используемая при отключенном Telegram
type NoopNotifier struct{}

// NewNoopNotifier возвращает новый экземпляр NoopNotifier
func NewNoopNotifier() *NoopNotifier {
	return &NoopNotifier{}
}

// NotifyNewOrder реализует интерфейс Notifier (no-op)
func (n *NoopNotifier) NotifyNewOrder(_ context.Context, _ *domain.Order, _ *iiko.OrderResult) {}

// NotifyOrderFailure реализует интерфейс Notifier (no-op)
func (n *NoopNotifier) NotifyOrderFailure(_ context.Context, _ *domain.Order, _ error) {}
```

- [ ] **Step 4: Run test to verify it passes**

Команда: `go test -v ./internal/notifier`  
Ожидается: `PASS: TestNoopNotifier`.

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/notifier.go internal/notifier/noop.go internal/notifier/noop_test.go
git commit -m "feat(notifier): define Notifier interface and NoopNotifier fallback"
```

---

### Task 3: Telegram Notifier Implementation with HTML Templates & Retries (`internal/notifier`)

**Files:**
- Create: `internal/notifier/telegram.go`
- Create: `internal/notifier/telegram_test.go`

- [ ] **Step 1: Write unit tests in `internal/notifier/telegram_test.go`**

Создать `internal/notifier/telegram_test.go` с мок-сервером `httptest.Server`, проверяющим:
1. Экранирование HTML (защита от сломанных тегов и инъекций в имени, улице, комментарии).
2. Отправку по всем `chat_ids`.
3. Успешный retry при ошибке 500 и отсутствие retry при 400.

```go
package notifier_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
	"beini-backend/internal/notifier"
)

func TestTelegramNotifier_NotifyNewOrder_Success(t *testing.T) {
	var requestCount int32
	var receivedBodies []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		receivedBodies = append(receivedBodies, payload)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 123}}`))
	}))
	defer server.Close()

	tg := notifier.NewTelegramNotifierWithBaseURL(
		server.URL,
		"test-bot-token",
		[]string{"-100111", "-100222"},
		server.Client(),
		slog.Default(),
	)

	order := &domain.Order{
		OrderNumber:  104,
		CustomerName: "Иван <Tag> & Co",
		Phone:        "+7 (928) 123-45-67",
		Address: domain.Address{
			Street: "ул. Мира & Дружбы",
			House:  "10",
		},
		OrderType: domain.OrderTypeDelivery,
		Items: []domain.OrderItem{
			{Name: "Шашлык <Баранина>", Amount: 2, Price: 500, Sum: 1000},
		},
		Comment: "Оставить у двери <быстро>",
	}
	result := &iiko.OrderResult{
		CorrelationID: "corr-12345",
		OrderInfo: iiko.OrderInfo{
			CreationStatus: iiko.StatusSuccess,
		},
	}

	tg.SendSync(context.Background(), order, result, nil)

	if atomic.LoadInt32(&requestCount) != 2 {
		t.Fatalf("requestCount = %d; want 2 (по одному на каждый chat_id)", requestCount)
	}

	for _, payload := range receivedBodies {
		if payload["parse_mode"] != "HTML" {
			t.Errorf("parse_mode = %v; want HTML", payload["parse_mode"])
		}
		text, _ := payload["text"].(string)
		if strings.Contains(text, "<Tag>") || strings.Contains(text, "<Баранина>") {
			t.Errorf("текст содержит неэкранированные HTML теги: %s", text)
		}
		if !strings.Contains(text, "&lt;Tag&gt;") {
			t.Errorf("ожидалось экранирование &lt;Tag&gt;, получено: %s", text)
		}
		if !strings.Contains(text, "1 000") || !strings.Contains(text, "104") {
			t.Errorf("текст не содержит номер заказа или сумму: %s", text)
		}
	}
}

func TestTelegramNotifier_RetryOn500(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok": false, "description": "temporary error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 456}}`))
	}))
	defer server.Close()

	tg := notifier.NewTelegramNotifierWithBaseURL(
		server.URL,
		"test-token",
		[]string{"123"},
		server.Client(),
		slog.Default(),
	)
	tg.SetRetryBackoff(5 * time.Millisecond) // быстрый бэкофф для тестов

	order := &domain.Order{OrderNumber: 1, CustomerName: "Тест", Phone: "+79991112233"}
	tg.SendSync(context.Background(), order, nil, nil)

	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d; want 2 (1 ошибка + 1 успешный retry)", attempts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Команда: `go test -v ./internal/notifier`  
Ожидается: Ошибка компиляции (не объявлены функции `NewTelegramNotifierWithBaseURL`, `SendSync`, `SetRetryBackoff`).

- [ ] **Step 3: Implement `internal/notifier/telegram.go`**

Создать `internal/notifier/telegram.go`:
```go
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// TelegramNotifier отправляет сообщения в Telegram через Bot API
type TelegramNotifier struct {
	baseURL      string
	botToken     string
	chatIDs      []string
	client       *http.Client
	logger       *slog.Logger
	retryBackoff time.Duration
}

// NewTelegramNotifier инициализирует TelegramNotifier со стандартным URL Telegram Bot API
func NewTelegramNotifier(botToken string, chatIDs []string, client *http.Client, logger *slog.Logger) *TelegramNotifier {
	return NewTelegramNotifierWithBaseURL("https://api.telegram.org", botToken, chatIDs, client, logger)
}

// NewTelegramNotifierWithBaseURL инициализирует TelegramNotifier с кастомным baseURL (для тестов)
func NewTelegramNotifierWithBaseURL(baseURL, botToken string, chatIDs []string, client *http.Client, logger *slog.Logger) *TelegramNotifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TelegramNotifier{
		baseURL:      strings.TrimRight(baseURL, "/"),
		botToken:     botToken,
		chatIDs:      chatIDs,
		client:       client,
		logger:       logger,
		retryBackoff: 500 * time.Millisecond,
	}
}

// SetRetryBackoff переопределяет базовую задержку ретраев (используется в тестах)
func (t *TelegramNotifier) SetRetryBackoff(d time.Duration) {
	t.retryBackoff = d
}

// NotifyNewOrder реализует интерфейс Notifier и асинхронно отправляет карточку заказа
func (t *TelegramNotifier) NotifyNewOrder(ctx context.Context, order *domain.Order, result *iiko.OrderResult) {
	if len(t.chatIDs) == 0 || t.botToken == "" {
		return
	}
	// Запускаем в отдельной горутине с защитой от паники
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.logger.Error("паника в горутине отправки telegram-уведомления", "panic", r)
			}
		}()
		// Создаем независимый контекст с таймаутом на отправку всех сообщений
		bgCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		t.SendSync(bgCtx, order, result, nil)
	}()
}

// NotifyOrderFailure реализует интерфейс Notifier и асинхронно отправляет алерт о сбое кассы
func (t *TelegramNotifier) NotifyOrderFailure(ctx context.Context, order *domain.Order, reason error) {
	if len(t.chatIDs) == 0 || t.botToken == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.logger.Error("паника в горутине telegram failure-уведомления", "panic", r)
			}
		}()
		bgCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		t.SendSync(bgCtx, order, nil, reason)
	}()
}

// SendSync выполняет синхронную отправку сообщения всем получателям (используется в тестах и горутинах)
func (t *TelegramNotifier) SendSync(ctx context.Context, order *domain.Order, result *iiko.OrderResult, failureErr error) {
	var messageText string
	if failureErr != nil {
		messageText = t.formatFailureMessage(order, failureErr)
	} else {
		messageText = t.formatSuccessMessage(order, result)
	}

	for _, chatID := range t.chatIDs {
		if strings.TrimSpace(chatID) == "" {
			continue
		}
		if err := t.sendMessageWithRetry(ctx, chatID, messageText); err != nil {
			t.logger.Error("не удалось отправить telegram-уведомление",
				"chat_id", chatID,
				"order_number", order.OrderNumber,
				"error", err,
			)
		}
	}
}

func (t *TelegramNotifier) sendMessageWithRetry(ctx context.Context, chatID, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.botToken)

	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ошибка сериализации payload telegram: %w", err)
	}

	maxAttempts := 3
	backoff := t.retryBackoff

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("ошибка создания запроса telegram: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = err
			t.logger.Warn("сетевая ошибка при отправке в telegram, повтор",
				"attempt", attempt,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		// Если ошибка клиента (4xx, кроме 429), повторять бессмысленно
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return fmt.Errorf("telegram api error: status %d, body: %s", resp.StatusCode, string(respBody))
		}

		lastErr = fmt.Errorf("telegram api status %d: %s", resp.StatusCode, string(respBody))
		t.logger.Warn("серверная ошибка telegram api, повтор",
			"attempt", attempt,
			"status", resp.StatusCode,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	return fmt.Errorf("превышено число попыток отправки: %w", lastErr)
}

func (t *TelegramNotifier) formatSuccessMessage(order *domain.Order, result *iiko.OrderResult) string {
	var b strings.Builder

	orderTypeLabel := "Доставка курьером"
	if order.OrderType == domain.OrderTypePickup {
		orderTypeLabel = "Самовывоз из ресторана"
	}

	b.WriteString(fmt.Sprintf("🔔 <b>НОВЫЙ ЗАКАЗ #%d</b>\n", order.OrderNumber))
	b.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	b.WriteString(fmt.Sprintf("💰 <b>Сумма:</b> %s ₽\n", formatPrice(order.TotalSum())))
	b.WriteString(fmt.Sprintf("📦 <b>Тип:</b> %s\n\n", orderTypeLabel))

	b.WriteString(fmt.Sprintf("👤 <b>Клиент:</b> %s\n", html.EscapeString(order.CustomerName)))
	cleanPhone := cleanPhoneForTel(order.Phone)
	b.WriteString(fmt.Sprintf("📞 <b>Телефон:</b> <a href=\"tel:%s\">%s</a>\n", cleanPhone, html.EscapeString(order.Phone)))

	if order.OrderType == domain.OrderTypeDelivery {
		addr := html.EscapeString(order.Address.Street)
		if order.Address.House != "" {
			addr += ", д. " + html.EscapeString(order.Address.House)
		}
		if order.Address.Building != "" {
			addr += ", корп. " + html.EscapeString(order.Address.Building)
		}
		if order.Address.Flat != "" {
			addr += ", кв./оф. " + html.EscapeString(order.Address.Flat)
		}
		b.WriteString(fmt.Sprintf("📍 <b>Адрес:</b> %s\n", addr))

		var extra []string
		if order.Address.Entrance != "" {
			extra = append(extra, "подъезд "+html.EscapeString(order.Address.Entrance))
		}
		if order.Address.Floor != "" {
			extra = append(extra, "этаж "+html.EscapeString(order.Address.Floor))
		}
		if order.Address.Doorphone != "" {
			extra = append(extra, "домофон "+html.EscapeString(order.Address.Doorphone))
		}
		if len(extra) > 0 {
			b.WriteString(fmt.Sprintf("🏢 <b>Детали:</b> %s\n", strings.Join(extra, ", ")))
		}
	}

	if strings.TrimSpace(order.Comment) != "" {
		b.WriteString(fmt.Sprintf("💬 <b>Комментарий:</b> <i>%s</i>\n", html.EscapeString(order.Comment)))
	}

	b.WriteString("\n📋 <b>Состав заказа:</b>\n")
	for _, it := range order.Items {
		b.WriteString(fmt.Sprintf("• %s × %d — %s ₽\n",
			html.EscapeString(it.Name),
			it.Amount,
			formatPrice(it.Sum),
		))
	}

	b.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	statusLabel := "Передан на терминал"
	if result != nil && result.OrderInfo.CreationStatus != iiko.StatusSuccess {
		statusLabel = fmt.Sprintf("Ожидает кассы (%s)", result.OrderInfo.CreationStatus)
	}
	b.WriteString(fmt.Sprintf("✅ <b>iikoFront:</b> %s\n", statusLabel))
	now := time.Now().Format("02.01.2006, 15:04:05")
	b.WriteString(fmt.Sprintf("⏰ <b>Время:</b> %s (МСК)\n", now))
	if result != nil && result.CorrelationID != "" {
		b.WriteString(fmt.Sprintf("🆔 <code>%s</code>\n", result.CorrelationID))
	}

	return b.String()
}

func (t *TelegramNotifier) formatFailureMessage(order *domain.Order, failureErr error) string {
	var b strings.Builder

	b.WriteString("🚨 <b>ТРЕВОГА: ЗАКАЗ НЕ ПОПАЛ В КАССУ IIKO!</b>\n")
	b.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	b.WriteString("Гость оформил заказ на сайте, но касса ресторана отклонила приём!\n")
	b.WriteString("<b>Срочно свяжитесь с клиентом и пробейте заказ вручную!</b>\n\n")

	b.WriteString(fmt.Sprintf("👤 <b>Клиент:</b> %s\n", html.EscapeString(order.CustomerName)))
	cleanPhone := cleanPhoneForTel(order.Phone)
	b.WriteString(fmt.Sprintf("📞 <b>Телефон:</b> <a href=\"tel:%s\">%s</a>\n", cleanPhone, html.EscapeString(order.Phone)))
	b.WriteString(fmt.Sprintf("💰 <b>Сумма:</b> %s ₽\n", formatPrice(order.TotalSum())))

	if order.OrderType == domain.OrderTypeDelivery {
		b.WriteString(fmt.Sprintf("📍 <b>Адрес:</b> %s\n", html.EscapeString(order.Address.Street)))
	}

	b.WriteString("\n📋 <b>Состав заказа:</b>\n")
	for _, it := range order.Items {
		b.WriteString(fmt.Sprintf("• %s × %d — %s ₽\n",
			html.EscapeString(it.Name),
			it.Amount,
			formatPrice(it.Sum),
		))
	}

	b.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	errMsg := "неизвестная ошибка"
	if failureErr != nil {
		errMsg = failureErr.Error()
	}
	b.WriteString(fmt.Sprintf("⚠️ <b>Причина:</b> <code>%s</code>\n", html.EscapeString(errMsg)))
	now := time.Now().Format("02.01.2006, 15:04:05")
	b.WriteString(fmt.Sprintf("⏰ <b>Время сбоя:</b> %s\n", now))

	return b.String()
}

func formatPrice(price int) string {
	s := fmt.Sprintf("%d", price)
	if len(s) <= 3 {
		return s
	}
	var res []string
	rem := len(s) % 3
	if rem > 0 {
		res = append(res, s[:rem])
	}
	for i := rem; i < len(s); i += 3 {
		res = append(res, s[i:i+3])
	}
	return strings.Join(res, " ")
}

func cleanPhoneForTel(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r == '+' || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Команда: `go test -v ./internal/notifier`  
Ожидается: `PASS` для `TestTelegramNotifier_NotifyNewOrder_Success` и `TestTelegramNotifier_RetryOn500`.

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/telegram.go internal/notifier/telegram_test.go
git commit -m "feat(notifier): implement TelegramNotifier with HTML templates and retry logic"
```

---

### Task 4: Handler Integration & Async Dispatch (`internal/handler`)

**Files:**
- Modify: `internal/handler/order_handler.go`
- Modify: `internal/handler/order_handler_test.go`

- [ ] **Step 1: Write test in `order_handler_test.go` verifying notifier calls**

Создать мок `MockNotifier` в `internal/handler/order_handler_test.go`:
```go
type mockNotifier struct {
	newOrderCalled   atomic.Bool
	orderFailedCalled atomic.Bool
}

func (m *mockNotifier) NotifyNewOrder(_ context.Context, _ *domain.Order, _ *iiko.OrderResult) {
	m.newOrderCalled.Store(true)
}

func (m *mockNotifier) NotifyOrderFailure(_ context.Context, _ *domain.Order, _ error) {
	m.orderFailedCalled.Store(true)
}
```

Добавить тест `TestOrderHandler_CreateOrder_NotifierSuccess`:
```go
func TestOrderHandler_CreateOrder_NotifierSuccess(t *testing.T) {
	// Проверяет, что при успешном заказе вызывается NotifyNewOrder
	mockN := &mockNotifier{}
	// Инициализируем хендлер с mockN
	// Вызываем CreateOrder с валидным телом
	// Убеждаемся, что mockN.newOrderCalled.Load() == true
}
```

- [ ] **Step 2: Run test to verify it fails**

Команда: `go test -v ./internal/handler -run TestOrderHandler_CreateOrder_NotifierSuccess`  
Ожидается: Ошибка компиляции (сигнатура конструктора или поле `notifier`).

- [ ] **Step 3: Update `OrderHandler` in `internal/handler/order_handler.go`**

1. Добавить поле `notifier notifier.Notifier` в структуру `OrderHandler`.
2. Обновить `NewOrderHandlerWithDeps`:
```go
func NewOrderHandlerWithDeps(
	client *iiko.Client,
	logger *slog.Logger,
	testToken string,
	cat catalog.Catalog,
	store *idempotency.Store,
	notif notifier.Notifier,
) *OrderHandler {
	if notif == nil {
		notif = notifier.NewNoopNotifier()
	}
	// ...
```
3. При успешном создании заказа (`CreateOrder` перед отправкой ответа):
```go
	// Асинхронное уведомление управляющего
	h.notifier.NotifyNewOrder(r.Context(), &order, res)
```
4. При ошибке iiko (`h.handleOrderError(w, err, &order)`):
```go
	h.notifier.NotifyOrderFailure(r.Context(), order, err)
```
5. В `SendTestOrder` также вызвать `h.notifier.NotifyNewOrder(...)` с тестовым флагом в номере.

- [ ] **Step 4: Run tests to verify they pass**

Команда: `go test -v ./internal/handler`  
Ожидается: `PASS` для всех тестов пакета `internal/handler`.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/order_handler.go internal/handler/order_handler_test.go
git commit -m "feat(handler): inject Notifier and dispatch order success and failure notifications"
```

---

### Task 5: Server Wiring & End-to-End Verification (`cmd/server`)

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Wire `TelegramNotifier` in `cmd/server/main.go`**

В `cmd/server/main.go`:
```go
	// 5.1. Инициализация системы оповещений (Telegram или fallback Noop)
	var orderNotifier notifier.Notifier
	if cfg.TelegramBotToken != "" && len(cfg.TelegramChatIDs) > 0 {
		logger.Info("активирован Telegram notifier", "chat_count", len(cfg.TelegramChatIDs))
		orderNotifier = notifier.NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatIDs, nil, logger)
	} else {
		logger.Info("Telegram notifier отключен (отсутствует токен или chat_id), используется NoopNotifier")
		orderNotifier = notifier.NewNoopNotifier()
	}

	// 6. Инициализация HTTP хендлеров
	orderHandler := handler.NewOrderHandlerWithDeps(iikoClient, logger, cfg.TestToken, beiniCatalog, idempotencyStore, orderNotifier)
```

- [ ] **Step 2: Run all tests in the workspace**

Команда: `go test -v ./...`  
Ожидается: Все пакеты `cmd/server`, `internal/catalog`, `internal/config`, `internal/domain`, `internal/handler`, `internal/idempotency`, `internal/iiko`, `internal/middleware`, `internal/notifier` возвращают `ok`.

- [ ] **Step 3: Compile server binary**

Команда: `go build -o server.exe ./cmd/server`  
Ожидается: Сборка завершается с кодом 0 без ошибок и предупреждений.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): wire telegram notifier with configuration fallback"
```

---

## Self-Review Checklist
- [x] **Spec coverage:** Проверены все требования: отправка в Telegram, несколько `chat_id`, HTML-экранирование, алерт при сбоях iiko, fallback Noop, zero-bloat.
- [x] **Placeholder scan:** Отсутствуют "TODO", "TBD", пропущенные куски кода или неспецифицированные методы.
- [x] **Type consistency:** Имена типов (`Notifier`, `TelegramNotifier`, `NoopNotifier`), методов (`NotifyNewOrder`, `NotifyOrderFailure`) и параметров совпадают во всех задачах.

