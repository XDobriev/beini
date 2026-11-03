# Тестовая эксплуатация: план реализации

> Исполнитель: superpowers:executing-plans, работа в этой сессии.

**Цель:** витрина для гостей и защищённый обычный checkout одного владельца.
**Архитектура:** middleware доступа перед существующим OrderHandler, server-side
сессии в памяти; журнал заказов не меняется. Статус доступен по прежнему ключу.
**Стек:** Go 1.24 standard library, HTML/CSS/Vanilla JS, Python mock fixture.
**Спецификация:** ../specs/2026-11-02-test-operation.md.

## Ограничения и фокус ревью

- Сохранить origin/main seed275/public251/hidden24 и чужие изменения checkout.
- Все автоматические заказы — mock; без production ключей и инфраструктуры.
- Закрытый/некорректный доступ никогда не вызывает upstream create.
- Сессии нескольких вкладок, поздние ответы и restart не создают новый ключ.
- CSRF, слишком длинные пароли/тела, forged cookies, лимит памяти/CPU.
- Public без сессии всё равно требует CSRF; меню freshness проверяется отдельно.
- Mock подтверждение нельзя спутать с результатом реальной кассы.

## Этапы

1. [x] Baseline: актуальный origin/main, отдельный worktree/ветка,
   полная проверка с loopback; read-only сверка публичного runtime.
2. [x] `internal/access`: тесты перед кодом для конфигурации, PBKDF2,
   access/login/logout, сроков, CSRF, перебора и всех gate-режимов.
   `cmd/password-hash` читает stdin; ENV подключается в `internal/config`.
   Команда: `go test ./internal/access ./internal/config`.
3. [x] `cmd/server/application.go`: gate /api/order, access endpoints,
   /tester; integration-тест owner через mock iiko с price/replay/restart;
   гость не вызывает create, status остаётся после смены режима.
   Команда: `go test ./cmd/server` с loopback.
4. [x] `index.html`, `js/app.js`, `css/style.css`, `tester.html`, `js/tester.js`:
   плашка, вход/выход, гостевая шторка, CSRF gate, refresh/tabs/expiry.
   Node regression в `scripts/check-access.cjs`, общая проверка JS.
5. [x] `scripts/mock-ui.py`: auth/modes, no-store/CSRF, безопасные сценарии,
   скрытие NEW, mock identity. Python unit tests и браузер desktop/mobile.
6. [x] README/product/architecture/design-system/roadmap/testing/operations/
   deployment/service.env.example: настройка, смена пароля, restart, приёмка,
   развёртывание отдельным поручением. Итоговый docs/tasks/test-operation.
7. [x] Полная `python scripts/check.py`, итоговый diff/review, проверка
   сохранности исходного checkout. Conventional Commit только этой ветки.

Один итоговый commit соответствует прямому поручению владельца; push отсутствует.
