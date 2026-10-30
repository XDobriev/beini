# Дизайн-спецификация: Система уведомлений о заказах BEINI в Telegram

- **Дата:** 30.10.2026
- **Статус:** Утверждён
- **Автор:** BEINI Engineering
- **Область:** Бэкенд (Go), интеграция с Telegram Bot API, интеграция с iikoCloud / iikoFront

---

## 1. Введение и цели

### 1.1. Проблема
В ресторане BEINI заказы с сайта поступают напрямую в кассовую систему iikoFront через облачный шлюз Go-бэкенда. Если управляющий или администратор смены отходит от кассового терминала, не слышит звуковой сигнал терминала или в часы пик занят в зале, заказ рискует остаться незамеченным и зависнуть до истечения таймаута приёма.

### 1.2. Цели
1. **Мгновенное информирование:** отправлять полную карточку поступившего заказа в Telegram управляющему / в рабочий чат смены в течение 1–2 секунд после оформления.
2. **Гарантированное реагирование на сбои кассы:** если iikoCloud или терминал iikoFront недоступны (оффлайн, ошибка валидации, таймаут связи), немедленно отправлять экстренный алерт в Telegram с контактами клиента и составом заказа, чтобы администратор не потерял заказ и связался с гостем вручную.
3. **Zero-Bloat и надёжность:** никакой зависимости от внешних очередей (Redis/RabbitMQ) или сторонних платных шлюзов. Неблокирующая асинхронная отправка, не замедляющая оформление заказа для гостя.

---

## 2. Архитектура и структура компонентов

### 2.1. Пакетная структура
```
internal/
├── notifier/
│   ├── notifier.go       # Интерфейс Notifier и доменные структуры сообщений
│   ├── telegram.go       # Реализация TelegramNotifier с поддержкой retry и таймаутов
│   ├── noop.go           # Заглушка NoopNotifier (при отключенном Telegram)
│   └── telegram_test.go  # Unit-тесты форматирования, экранирования HTML и HTTP-клиента
├── config/
│   └── config.go         # Добавление TelegramBotToken и TelegramChatIDs ([]string)
├── handler/
│   └── order_handler.go  # Внедрение Notifier и вызовы при создании/ошибке заказа
```

### 2.2. Контракт интерфейса `Notifier`
```go
package notifier

import (
	"context"
	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// Notifier определяет контракт для систем отправки служебных оповещений
type Notifier interface {
	// NotifyNewOrder асинхронно отправляет карточку успешно принятого заказа
	NotifyNewOrder(ctx context.Context, order *domain.Order, result *iiko.OrderResult)

	// NotifyOrderFailure асинхронно отправляет экстренное предупреждение о сбое в iiko
	NotifyOrderFailure(ctx context.Context, order *domain.Order, reason error)
}
```

### 2.3. Внедрение зависимостей в `OrderHandler`
В конструктор `NewOrderHandlerWithDeps` добавляется параметр `notifier notifier.Notifier`:
- При успешном завершении `CreateOrder` вызывается `h.notifier.NotifyNewOrder(...)` в безопасной горутине.
- При ошибке `h.handleOrderError` вызывается `h.notifier.NotifyOrderFailure(...)` в безопасной горутине.
- При отсутствии токена в конфиге используется `noop.NewNoopNotifier()`.

---

## 3. Форматирование сообщений и шаблоны (HTML)

Отправка выполняется в режиме Telegram `parse_mode: "HTML"`. Все пользовательские поля (`Customer.Name`, `Address.Street`, `Comment`) строго экранируются через `html.EscapeString`.

### 3.1. Шаблон успешного заказа
```html
🔔 <b>НОВЫЙ ЗАКАЗ #{{.OrderNumber}}</b>
━━━━━━━━━━━━━━━━━━━━
💰 <b>Сумма:</b> {{.TotalSum}} ₽ (Оплата: {{.PaymentType}})
📦 <b>Тип:</b> {{.OrderType}}

👤 <b>Клиент:</b> {{.CustomerName}}
📞 <b>Телефон:</b> <a href="tel:{{.CleanPhone}}">{{.FormattedPhone}}</a>
📍 <b>Адрес:</b> {{.FullAddress}}
{{if .Comment}}💬 <b>Комментарий:</b> {{.Comment}}{{end}}

📋 <b>Состав заказа:</b>
{{range .Items}}• {{.Name}} × {{.Amount}} — {{.Sum}} ₽
{{end}}
━━━━━━━━━━━━━━━━━━━━
✅ <b>iikoFront:</b> {{.CreationStatus}}
⏰ <b>Время:</b> {{.Timestamp}}
🆔 <code>{{.CorrelationID}}</code>
```

### 3.2. Шаблон экстренного алерта (сбой передачи в iiko)
```html
🚨 <b>ТРЕВОГА: ЗАКАЗ НЕ ПОПАЛ В КАССУ IIKO!</b>
━━━━━━━━━━━━━━━━━━━━
Гость нажал «Оформить заказ» на сайте, но касса ресторана не смогла его принять!
Срочно свяжитесь с гостем и забейте заказ в кассу вручную.

👤 <b>Клиент:</b> {{.CustomerName}}
📞 <b>Телефон:</b> <a href="tel:{{.CleanPhone}}">{{.FormattedPhone}}</a>
📍 <b>Адрес:</b> {{.FullAddress}}
💰 <b>Сумма:</b> {{.TotalSum}} ₽

📋 <b>Состав:</b>
{{range .Items}}• {{.Name}} × {{.Amount}} — {{.Sum}} ₽
{{end}}
⚠️ <b>Причина ошибки iiko:</b> <code>{{.ErrorMessage}}</code>
⏰ <b>Время сбоя:</b> {{.Timestamp}}
```

---

## 4. Конфигурация и окружение

В `internal/config/config.go` и `iiko_config.json`:
```json
{
  "telegramBotToken": "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ",
  "telegramChatIds": ["-1002345678901", "548192031"]
}
```

### Переменные окружения (12-Factor App):
- `TELEGRAM_BOT_TOKEN` — токен бота от `@BotFather`.
- `TELEGRAM_CHAT_IDS` — список ID чатов/групп через запятую (`-100123456789,548192031`).

Если `telegramBotToken` пустой, сервис логирует информационную строку и использует `NoopNotifier`, сохраняя 100% работоспособность сайта и API.

---

## 5. Надёжность и отказоустойчивость

1. **Неблокирующая отправка:** вызов отправки обёрнут в `go func()`. Задержки Telegram API (300–800 мс) никак не влияют на время ответа гостю на сайте.
2. **Panic Protection:** горутина защищена блоком `defer func() { if r := recover(); r != nil { logger.Error(...) } }()`. Никакой сбой внутри нотификатора не может привести к падению сервера.
3. **Retry Policy:**
   - Таймаут HTTP-клиента: 5 секунд на запрос.
   - До 3 повторных попыток с экспоненциальной задержкой (500ms, 1000ms, 2000ms) при кодах ответа HTTP 5xx или временных сетевых ошибках соединения.
   - Ошибки 4xx (например, 400 Bad Request или 403 Forbidden) логируются с уровнем `ERROR` без бессмысленных повторов.

---

## 6. Тестирование и верификация

1. **Unit-тестирование (`internal/notifier/telegram_test.go`):**
   - Тест на корректное экранирование HTML-тегов (`<script>`, `&`, `"`).
   - Тест HTTP-клиента с использованием `httptest.Server` (эмуляция 200 OK, 500 Internal Error с последующим успехом на retry, 400 Bad Request).
   - Проверка поведения `NoopNotifier`.
2. **Интеграционное тестирование (`internal/handler/order_handler_test.go`):**
   - Мок-тест: успешное создание заказа гарантированно вызывает `NotifyNewOrder`.
   - Мок-тест: сбой iiko гарантированно вызывает `NotifyOrderFailure`.
3. **Служебный тест с кассы (`/demo` и `/api/test-order`):**
   - Добавление флага отправки тестового сообщения в Telegram при нажатии тестовой кнопки на служебной странице `/demo`.
