# Уведомления администратору BEINI

Статус: реализация завершена, offline-проверки пройдены; внешняя приёмка не выполнена.
Обновлено: 1 ноября 2026, Europe/Moscow.
Ветка: `codex/notifications-2026-11-01`, отдельный managed worktree.
Исходная ветка: `codex/integration-2026-11-01`, commit `fdcd085`.
Результат сохранён в коммите ветки; merge/push/публикация не выполнялись.
Исходные рабочие папки не редактировались. Машинный путь нового worktree дан в чате.

## Цель и критерии приёмки

Доставлять события существующих NotificationQueue/notificationSource в закрытую
группу администратора: устойчивые ack и дедупликация, retry и восстановление
после рестарта, pending перед конечным событием. Сбой transport/receipts не
мешает оформлению и чтению статуса. До реквизитов отправка проверяется mock.
Не менять VPS, не публиковать и не создавать реальные заказы.

## Контекст и решения

[Контракт и настройка Telegram](../telegram-notifications.md),
[ADR 0005](../decisions/0005-notification-delivery.md),
[локальный план](../superpowers/plans/2026-11-01-notification-delivery.md),
[предыдущая интеграция](integration-2026-11-01.md).

Использован отдельный notifications.jsonl на базе orderstate.Store, чтобы ошибка
ack не отравляла orders.jsonl. Группа привязана хешем ID; автоматической смены
адресата нет. Накопленная очередь не удаляется при отсутствии реквизитов.
Это одна группа/один worker/один процесс, без внешней очереди и новых зависимостей.

## Сделано

- `internal/notifier/worker.go`: последовательная доставка pending/итога;
  группировка независимо от сортировки/часов; дедупликация частей; сохраняемые
  попытки, ack, сроки retry и общая transport-пауза. Backoff 2…512 секунд,
  retry_after для 429, постоянные 4xx через 15 минут без потери события.
  Защита от panic transport, контекст/таймаут, сериализация concurrent Step.
- `internal/notifier/telegram.go`: sendMessage, POST JSON/plain text, timeout,
  no redirects, no link preview, protect_content. Проверка успешного message_id,
  безопасные ошибки без URL/токена. Длинные карточки разбиты без потери позиций,
  части имеют стабильные ключи/номера и собственные ack.
- `internal/orderstate/delivery.go`: сохранение receipts через checksum/fsync/OS lock,
  recovery из того же проверенного формата. Добавлены deliveryAttempts/nextAt;
  контакты в receipts не дублируются, исходный журнал заказов не меняется на send.
- `cmd/server/application.go`: независимые открытие/закрытие receipts и worker.
  Отсутствие/ошибка настройки, повреждение receipts и другая группа отключают
  только доставку. Shutdown отменяет отправку и ожидает worker.
- `internal/config`: только ENV TELEGRAM_BOT_TOKEN и TELEGRAM_CHAT_ID, json:"-".
  Токен не добавлялся ни в конфиги, ни в frontend, ни в документацию.
- Инструкция создания закрытой группы/бота и получения ID на сервере без
  раскрытия токена; описание backlog, recovery, миграции группы и retention.
  Обновлены README, product, architecture, operations, order-processing, testing,
  roadmap и передача предыдущего этапа. Старый Telegram-план сохранён без правок.

## Проверки

1 ноября 2026 в новом worktree, с разрешёнными temp/loopback:

- `python scripts/check.py`: All checks passed, exit 0. Go tests/vet/build,
  JS syntax/checkout, согласованность меню, 14 Python-тестов, аудит 35 блюд.
- `go test ./internal/notifier ./internal/orderstate ./cmd/server ./internal/config`:
  пройдены. Новые тесты worker: retry/ack после рестарта, concurrent Step,
  неуспешный ack после отправки, частичные карточки, panic/4xx, 429 с сохранённой
  паузой всей группы после переоткрытия журнала. Loopback HTTP проверяет
  success/invalid JSON/API error/5xx/403/429, no redirects, plain text и лимит.
- `TestApplicationMenuOrderRestart`: фактический create при отказе mock transport,
  один create на ключ, после рестарта pending → confirmed/rejected; потеря ответа
  iiko остаётся в исходной матрице. Это actual assembly приложения с mock iiko.
- `TestNotificationFailureDoesNotBlockApplication`: блокирующий transport,
  повреждённые receipts и несовпадение группы; статус заказа доступен, shutdown
  завершается, неправильный адресат не получает отправку.
- `TestDeliverySyncFailureDoesNotAck`: ошибка записи реального receipt-файла
  не создаёт ack в памяти/на диске; после рестарта событие остаётся pending.
- Независимое read-only ревью нашло два P2 в 429: отсчёт паузы до ответа и окно
  между записью retry части и transport cooldown. Оба исправлены и повторно
  проверены ревьюером. `TestRetryDeadlineStartsAfterTransportResponse` учитывает
  длительность Send; `TestCrashBetweenRetryWritesPreservesDestinationPause`
  прерывает выполнение после первого post-send fsync и переоткрывает реальный
  журнал. Оба regression-теста падали до соответствующих исправлений и прошли
  после них. Пауза теперь считается после ответа и сохраняется первой.
- `git diff --check`: пройден для изменений этапа.
- `go test -race ./internal/notifier ./internal/orderstate ./cmd/server`: не собрал
  тесты. GCC/ld не находит crt2.o/crtbegin.o из-за кириллического пути установки
  Windows toolchain. Это не проверка отсутствия гонок; исправление toolchain
  и повтор под race остаются отдельно. Системное окружение не менялось.

UI не менялся; новая браузерная приёмка не проводилась. Linux execution, Docker,
сбой питания, реальная группа/Telegram и ресторанные API не проверялись.
Реальные заказы и сообщения не отправлялись; официальная публичная документация
Telegram использована для сверки sendMessage/BotFather/getUpdates.

## Осталось и ограничения

1. Владелец создаёт закрытую группу/бота и передаёт реквизиты только окружению
   сервера. После отдельного поручения — тест группы на mock-источнике и затем
   согласованная кассовая приёмка. Программная реализация не доказывает доставку
   в ещё не созданную группу.
2. Потеря ответа Telegram или crash до fsync ack допускает повтор той же части
   с тем же ID. Exactly-once и прочтение администратором не обещаются.
3. Ретенция контактов/истории, ACL и backups требуют решения до публичного запуска.
   Очистки нет. При первом включении отправится накопленная история с source.
4. Не удалять/откатывать receipts: это переотправит историю. Смена группы требует
   согласованного переноса/отсечения backlog; автоматической миграции нет.
   Формат/границы частей карточки нельзя менять без совместимости сохранённых ack.
5. При ошибке записи receipts worker не отправляет без сохранения; после устранения
   причины нужен рестарт. Health не является подтверждением уведомлений; смотреть
   безопасные логи и проверять доставку в группе.
6. Предыдущие вопросы read-only проверки меню ресторана, размещения, доставки,
   кассовой приёмки и Docker остаются из integration handoff. Этот этап их не меняет.

## Блокеры и вопросы

Для локальной реализации блокеров нет. Для внешнего подключения отсутствуют
реквизиты Telegram и приёмка группы. Для race-проверки требуется рабочий CGO/GCC.

## Следующий шаг

Создать бота и закрытую группу по docs/telegram-notifications.md; получить ID
и подготовить server-only окружение. Включение с backlog и живая проверка —
отдельным поручением, с предварительной сверкой доступа к контактам.
