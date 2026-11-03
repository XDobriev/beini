# Menu variants Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Объединить порции одного блюда в карточке без изменения заказа iiko.
**Architecture:** Явные variantGroup/variantName/variantLabel/variantOrder у исходных
товаров проходят через Go API и статический экспорт. Frontend строит карточки
поверх плоского каталога; корзина и запрос заказа используют прежние товарные ID.
**Tech Stack:** Go 1.24, Python 3.10+, Node.js 22+, HTML/CSS/Vanilla JS.
**Spec:** docs/superpowers/specs/2026-11-02-menu-variants-design.md

## Global Constraints

- Разные вкусы остаются отдельными карточками; группы задаются явно.
- Цена и доступность каждого товара проверяются сервером.
- Не добавлять миллилитры без подтверждения, runtime-библиотеки и новые токены.
- По последующему поручению владельца коммит и push разрешены после тестов;
  публикация и реальные заказы в задачу не входят.

## Review Focus

- Сохранённая корзина с двумя порциями сохраняет оба ID и количества.
- Стоп/исчезновение одной порции не скрывает доступную соседнюю.
- Смена цены требует прежнего подтверждения; отправляются ID и цены порций.
- Поиск по порции сохраняет всю карточку; hidden NEW не раскрывается.
- 3–4 варианта, длинные подписи и клавиатурный фокус не ломают мобильную сетку.

## Task 1: Данные, API и выбор порции

**Files:** data/menu_catalog.json; internal/catalog/catalog.go, snapshot.go,
snapshot_test.go; tools/import_menu.py, test_import_menu.py; js/menu-data.js,
app.js; internal/catalog/catalog_data.go; css/style.css; index.html;
scripts/check-menu-variants.cjs, check.py.

**Interfaces:** четыре редакционных поля у исходного Item; menuCards(items)
строит карточки, selectVariant(group, id) выбирает исходный товар. cartLines()
и placeOrder() сохраняют прежний контракт.

- [x] Добавить RED-проверки сохранения metadata в Go/JS и запрета некорректных групп.
- [x] Добавить RED-проверки реального app.js: группировка, 3–4 порции, поиск,
  независимые количества, refresh/стопы/цены, восстановление и checkout payload.
- [x] Реализовать metadata, 29 явных групп, экспорт и интерфейс; получить GREEN.
- [x] Включить проверку вариантов в scripts/check.py и выполнить полную команду.
- [x] Проверить desktop/mobile на offline-стенде, выполнить ревью изменения.
- [x] Обновить README, product, architecture, design-system, testing и передачу
  с фактическими результатами. Последующее поручение: после тестов создать
  и отправить коммит; публикация отдельно.

## Журнал

- Исходная полная проверка прошла в предыдущем ходе с разрешённым loopback.
- Работа выполняется последовательно в текущем checkout; чужой untracked
  docs/tasks/iiko-empty-api-diagnosis-2026-11-02.md не изменяется.
- Основная suite прошла, 29 групп/222 карточки подтверждены. 17 Python-тестов
  импортёра и 8 v3-аудита; desktop/mobile и ширины 320/390/1024/1440 проверены.
- Final: fixed P2 default selection after stop — `add(default) → stop → refresh`
  RED→GREEN, первый добавленный вариант сохраняется; независимое ревью других
  существенных замечаний не выявило. Ревью выполнялось в отдельном worktree,
  после завершения он архивирован. Полная suite после исправления повторно
  завершилась All checks passed.
