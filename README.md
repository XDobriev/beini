# Ресторан BEINI — Официальный сайт & Доставка

Веб-сервис и интерактивное онлайн-меню для ресторана интернациональной и авторской кухни **BEINI** (г. Назрань, ул. С. С. Осканова, 21).

## Основной функционал

- **Интерактивное меню:** рубрикатор по разделам (мясо на углях, гриль, супы, салаты, суши, десерты, авторские чаи и кофе с урбечом).
- **Корзина доставки и самовывоза:** быстрый расчет суммы, выбор способа получения и адреса.
- **Оформление заказа:** прямая передача заказа в кассовую систему iikoFront через API-шлюз либо дублирование в WhatsApp администратора.
- **Бронирование столов и кабинок:** интерактивная форма резерва с выбором приватных закрытых кабинок (до 10 гостей) или общего зала.
- **Связка с iikoCloud:** интеграция через Public API v2 с передачей заказов на терминал iikoFront (`internal/iiko`, `iiko_config.json`).
- **Тестирование кассы:** служебный изолированный интерфейс (`/demo`, см. `TESTING_IIKO_FRIDAY.md`) и консольный инструмент (`cmd/test-order`).

## Стек и архитектура

- **Бэкенд:** Golang 1.24+ (совместим с Go 1.22+; `net/http`, `log/slog`, `sync.RWMutex`, Graceful Shutdown).
  - Безопасная раздача статических файлов (без утечки конфигов и исходного кода).
  - Корректный 404 fallback: JSON 404 для `/api/*`, честный 404 для отсутствующих статических ассетов (`.css`, `.js`, `.png` и др.), SPA fallback на `index.html` для навигации.
  - Middleware: CORS (с поддержкой `X-Test-Token`), Structured Logging (`slog`), Panic Recoverer.
  - Таймауты: ReadTimeout 15s, WriteTimeout 45s, IdleTimeout 60s, Shutdown 10s.
- **Фронтенд (Zero-Bloat & Subtractive Design):**
  - Чистый HTML5, нативный CSS Grid / Flexbox на CSS Variables. Полное отсутствие внешних runtime CDN и библиотек.
  - **Типографика:** `Cormorant` (с `lining-nums` для цифр цен) + `Golos Text`.
  - **Палитра:** теплый холст (`#FAF8F3`), чернила (`#1F3A2B`), рубиновый гранат (`#A3161A`).
  - **Адаптивность:** Mobile First с единой точкой перелома `@media (min-width: 1024px)`.
- **Интеграция:** iikoCloud Public API v2 (JWT Auth, Deliveries API v2).
- **Контейнеризация:** Multi-stage Dockerfile (~13.2 МБ на базе `alpine:3.19`), Docker Compose.

## HTTP API

| Метод | Путь | Описание |
| :--- | :--- | :--- |
| `GET` | `/` | Главная страница сайта ресторана BEINI |
| `GET` | `/api/health` | Проверка жизнеспособности сервера (health check) |
| `POST` | `/api/order` | Прием заказа от гостя и создание доставки в iikoCloud |
| `POST` | `/api/test-order` | Отправка тестового заказа на терминал кассы (защищен `X-Test-Token` / localhost) |
| `GET` | `/demo` | Служебная страница ручной проверки связи с терминалом |

## Проверенные команды запуска и тестирования

### 1. Модульное тестирование (без реальных ключей iiko и без внешнего API)

```bash
go test ./...
```
Все тесты пакетов `internal/config`, `internal/domain`, `internal/handler`, `internal/iiko` и `internal/middleware` изолированы и выполняются на моках (`httptest.Server`).

### 2. Сборка проекта

```bash
# Компиляция всех пакетов проекта
go build ./...

# Сборка бинарного исполняемого файла
go build -o server.exe ./cmd/server
```

### 3. Локальный запуск сервера

```bash
go run ./cmd/server
```
Сервер стартует на `http://localhost:8080`.

### 4. Консольный тест связи с кассой

```bash
go run ./cmd/test-order
```

### 5. Сборка и запуск в Docker

```bash
# Сборка легковесного Docker-образа
docker build -t beini-backend .

# Запуск контейнера с пробросом порта и монтированием конфигурации
docker run -p 8080:8080 -v "${PWD}/iiko_config.json:/app/iiko_config.json:ro" beini-backend

# Либо запуск через Docker Compose
docker compose up -d --build
```

Конфигурационный шаблон находится в `iiko_config.example.json`. Боевые ключи задаются в `iiko_config.json` либо через переменные окружения (`IIKO_API_KEY`, `IIKO_APP_ID`, `IIKO_CLIENT_SECRET`, `IIKO_ORGANIZATION_ID`, `IIKO_TERMINAL_GROUP_ID`).
