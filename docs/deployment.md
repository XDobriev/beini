# Развёртывание на Linux VPS

Go раздаёт frontend/API с одного origin. Nginx завершает HTTPS и проксирует
запросы на `127.0.0.1:8080`. Сервис — `beini.service`. Docker не используется.
Результат фактической приёмки: [задача публикации](tasks/public-deployment-2026-11-02.md).

## Состав релиза

После полной `python scripts/check.py` и commit:

```sh
python scripts/package-release.py --output scratch/deploy/beini.tar.gz
```

Скрипт требует чистый tracked checkout, собирает Linux/amd64 без CGO и включает
только tracked frontend-файлы, все изображения/логотипы и data/menu_catalog.json.
`release.json` содержит commit и SHA-256 файлов. Конфигурация и состояние в архив
не входят. Архив извлекается в новый `/opt/beini/releases/<commit>`, binary
исполнимый, файлы принадлежат root. Проверить manifest до запуска.
`/opt/beini/current` указывает на активный релиз. Повторно каталог релиза не
перезаписывать. Сервис не имеет прав менять код.

При распаковке Python `tarfile` с `filter='data'` права каталогов из tar
не восстанавливаются автоматически. После извлечения явно установить для всех
каталогов релиза 0755, для binary 0755, для остальных файлов 0644; владелец root.
До остановки сервиса проверить чтение всех manifest-файлов и запуск binary
через `runuser -u beini -- test -r <файл>` и `test -x <binary>`. Это проверка
прав, не запуск второй копии сервера. При неудачном запуске и откате может
потребоваться `systemctl reset-failed beini` перед стартом прежней версии.

HTML раздаётся с `Cache-Control: no-cache`, чтобы браузер перепроверял версию;
после изменения JS обновлять параметры версии обеих script-ссылок в index.html.
Проверять обновление в браузере с прежним кэшем, включая число разделов/карточек.

## Каталоги и запуск

Системный пользователь `beini` без shell:

- `/etc/beini`: root:beini, 0750; iiko_config.json и service.env — 0640.
- `/var/lib/beini`: beini:beini, 0700; journals/cache — 0600.
- `/opt/beini/releases`: root:root, исполнимые каталоги; публичные файлы — 0644.
- `/var/lib/beini-acme`: webroot ACME без конфигурации и журналов заказов.

Конфигурацию передавать защищённо из локального секрета, не выводить её содержимое.
Типы заказов/оплаты остаются в JSON. Владелец 2 ноября поручил **чистое** состояние
на VPS; старый локальный журнал теста №12540 сохраняется отдельно и не переносится.
Для последующих переносов остановить прежний writer и сохранить действующие
orders.jsonl/notifications.jsonl; это разрешение на чистый старт к ним не относится.

Шаблоны: [unit](../deploy/beini.service), [ENV](../deploy/service.env.example),
[журнал](../deploy/journal-retention.conf). Установить unit в
`/etc/systemd/system/beini.service`, ENV — `/etc/beini/service.env`, настройки
изолированного journald — `/etc/systemd/journald@beini.conf.d/retention.conf`.
HTTP_LISTEN_ADDR должен быть `127.0.0.1:8080`; при ошибочном адресе запуск
завершается ошибкой, публичный PORT не используется как fallback.
ALLOWED_ORIGINS явно содержит оба HTTPS-имени beinirest.ru.
Telegram не включать в рамках этой публикации.

После проверки unit/прав и свободного порта:

```sh
systemd-analyze verify /etc/systemd/system/beini.service
systemctl daemon-reload
systemctl enable --now beini
curl --fail http://127.0.0.1:8080/api/health
ss -lntp
```

`/api/health` — живость процесса. Связь с iiko и допуск заказа проверять по
`/api/menu`: свежие меню и стопы, доступные позиции и orderingAvailable.
До появления свежего снимка оформление блокируется.

## HTTPS и продление

На сервере уже есть Nginx и Certbot. Не менять avtorstudio, глобальные `map`
и listeners других проектов. Отдельный vhost:
`/etc/nginx/sites-available/beinirest.ru` и ссылка в sites-enabled.
Сначала установить [HTTP/ACME-конфиг](../deploy/nginx-http.conf).
Он отвечает 503 на запросы приложения, пока не готов сертификат.
Всегда `nginx -t` перед reload/restart Nginx.

После A-записей apex/www на выбранный сервер и проверки CAA/AAAA:

```sh
certbot certonly --webroot -w /var/lib/beini-acme --cert-name beini \
  -d beinirest.ru -d www.beinirest.ru
```

Использовать уже существующую регистрацию ACME, не принимать новые условия
или задавать контакт вместо владельца без необходимости. Установить
[HTTPS-конфиг](../deploy/nginx-https.conf), затем `nginx -t` и reload.
Файлы сертификата — `/etc/letsencrypt/live/beini/`.
Проверить `certbot.timer`; [deploy hook](../deploy/renew-beini-certificate)
установить исполнимым в `/etc/letsencrypt/renewal-hooks/deploy/beini`.
Он действует только для сертификата BEINI и проверяет Nginx перед reload.
Проверить `certbot renew --cert-name beini --dry-run`.

## Логи, проверки и рестарт

Логи приложения доступны администраторам через
`journalctl --namespace=beini -u beini`; отдельный namespace ограничен 32 МБ
и 14 днями, без пересылки в общий syslog. Логи не содержат тел заказов,
имени гостя, ключей и сырых ответов iiko. Nginx access_log BEINI отключён.
Состояние заказов не имеет автоматического retention: доступ/резервирование
журналов следует учитывать отдельно от логов.

Проверить desktop/mobile: меню, поиск, фото, корзина и открытие оформления,
не отправляя заказ. Проверить no-store/freshness `/api/menu`, HTTPS/redirect,
честные 404 неизвестных API/ассетов и отсутствие HTTP-доступа к конфигам,
журналам, seed и backup. Внешний 8080 недоступен. После `systemctl restart beini`
проверить новый PID, health, свежесть меню, права и сохранность journals/cache.
При чистом старте проверка пустого журнала сама по себе не доказывает
восстановление реального заказа: соответствующий сценарий проверяется
offline `TestApplicationMenuOrderRestart`. Внешний кассовый тест — отдельное
согласование с владельцем и обязательной последующей отменой.

## Обновление и откат

Перед обновлением сохранить закрытый manifest текущего релиза и параметры
запуска. Не удалять предыдущий совместимый release. Затем:

```sh
systemctl stop beini
ln -s /opt/beini/releases/NEW_COMMIT /opt/beini/current.next
mv -Tf /opt/beini/current.next /opt/beini/current
systemctl start beini
```

Для отката те же команды с PREVIOUS_COMMIT. Проверить health/menu/bind и статус
существующей попытки по её приватному Idempotency-Key, если она есть.
**Не заменять и не откатывать `/var/lib/beini/orders.jsonl` или notifications.jsonl.**
Не запускать два writer с разными копиями состояния. Исходный d3c1a24 без
HTTP_LISTEN_ADDR нельзя считать безопасным откатом с прежним ENV.
При первом релизе, если нет проверенного предыдущего: остановить только beini
и вернуть его vhost на 503, сохранив всё состояние. Для изменения vhost —
`nginx -t`, затем reload; конфиги других проектов не восстанавливать из общей копии.

## Наблюдаемый результат 2 ноября 2026

DNS apex/www указывает на 72.56.232.231 на обоих NS REG.RU; AAAA и CAA
не обнаружены. Отдельный HTTPS-vhost установлен из deploy/nginx-https.conf
после выпуска сертификата beini через существующую ACME-регистрацию.
nginx -t выполнен до reload; другие vhost не изменялись.
Оба HTTP-имени возвращают 301 на соответствующий HTTPS-origin.
Сертификат покрывает оба имени и действует до 31 января 2027.
Certbot timer active; certbot renew --cert-name beini --dry-run прошёл.

Runtime первоначального запуска: fe7420d949fb6f9bbac64bed7cba229ae1e6ead6;
/opt/beini/current указывает на одноимённый release. Изменение DNS/HTTPS
не меняет binary и журнал состояния. На момент проверки orders.jsonl пустой.
Для возврата к 503 сохранён /etc/beini/nginx-http-bootstrap.conf:
восстановить **только** vhost beinirest.ru, затем nginx -t и reload.
При остановке приложения сохранить /var/lib/beini целиком.

Браузер в Windows-сети после смены DNS ещё видел парковку REG.RU.
Не считать такой ответ проверкой VPS. Финальная Playwright-приёмка
использовала --no-proxy-server и явный DNS
только в отдельном тестовом Chrome: публичный домен, действительный сертификат
и обычные запросы frontend; TLS-проверка не отключалась. Это проверка VPS
через публичный HTTPS, а не подтверждение обновления всех внешних кэшей.

Позднее 2 ноября меню обновлено до runtime865162230fff2c79597b6cc7ad0a18fd9e97be39:
251 видимая карточка и 24 hidden NEW, все фото-заглушки. Обычный браузер с
прежним кэшем проверен после reload. Nginx/DNS/config и журналы не менялись;
предыдущие релизы сохранены. [Отчёт обновления](tasks/menu-release-2026-11-02.md).

## Следующее развёртывание: тестовая эксплуатация

Этот раздел — инструкция для отдельного поручения, не запись выполненных действий.
Ветка `codex/test-operation` основана на origin/main38aad78 и сохраняет seed275,
public251/hidden24. Её текущий локальный результат: [test-operation](tasks/test-operation.md).

1. После ревью и разрешения на публикацию объединить изменения с актуальным main,
   сохранив последующие изменения меню. Повторить `python scripts/check.py`.
   Из чистого commit собрать `python scripts/package-release.py --output RELEASE.tar.gz`.
   Manifest обязан включать `public/tester.html`, `public/js/access.js`,
   `public/js/tester.js`, обновлённые app.js/style.css и прежний seed.
2. Сохранить закрытый manifest действующего `/opt/beini/current` и параметры
   конфигурации. Сохранить действующий BEINI_STATE_DIR целиком; orders/notifications
   не заменять, не очищать и не откатывать. Не запускать второй writer.
3. Локально создать уникальный пароль владельца через
   `python scripts/tester-password.py --env --output PRIVATE_ENV_FRAGMENT`.
   Защищённо установить fragment в `/etc/beini/tester.env` root:beini0640;
   пароль и хеш не печатать. Существующие iiko config и Telegram не менять.
   В `/etc/beini/service.env` установить `BEINI_ORDER_MODE=tester`,
   `BEINI_TESTER_LOCAL_HTTP=0`, сохранить точные HTTPS origins apex/www.
   Для полного запрета до входа владельца можно сначала выбрать closed.
4. Установить обновлённый unit `deploy/beini.service`: он дополнительно читает
   опциональный `/etc/beini/tester.env`. Выполнить `systemd-analyze verify`
   и `systemctl daemon-reload`. Распаковать релиз в новый каталог, сверить
   manifest/SHA256 и права; выполнить обязательный preflight от пользователя beini,
   описанный выше. Публичные статика/каталоги0644/0755, binary0755.
5. Остановить **только beini**, атомарно переключить `/opt/beini/current` и
   запустить beini с прежним состоянием по регламенту выше. Nginx/DNS/avtorstudio
   не менять. Проверить health, loopback bind, fresh menu/stop, права/сохранность
   журналов и отсутствие секретов в логах. При restart сессии отзываются, заказы
   остаются в журнале и worker сверяет pending.
6. Через оба публичных HTTPS имени проверить меню, плашку, гостевую корзину и
   `/api/access`: mode=tester, authenticated=false, canOrder=false, no-store.
   Проверить приватные файлы404, затем войти на выбранном одном имени через
   `/tester`: authenticated/canOrder=true, предупреждение реальной отправки,
   выход возвращает canOrder=false. Эти проверки не требуют создания заказа.
7. Обычный checkout с iiko — только отдельная согласованная приёмка с сотрудником:
   заказ → карточка/состав/сумма/сигнал/печать → отмена сотрудником. После неё
   `public` включать только по решению владельца и с тем же журналом.

Важно для отката: runtime8651622 ещё **не содержит** нового ограничения доступа.
Его возврат может снова открыть гостевой POST, даже если в ENV написано closed.
Для аварийного закрытия остановить beini и использовать существующий отдельный
BEINI bootstrap503 по регламенту с nginx -t; конфиги других проектов не менять.
Не считать прежний runtime безопасным откатом режима без проверки его поведения.
