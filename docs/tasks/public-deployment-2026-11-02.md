# Публичное развёртывание BEINI

Статус: частично выполнено; публичный HTTPS ещё не подключён
Обновлено: 2 ноября 2026
Целевая ветка: main; подготовка: codex/public-deployment
Исходный commit: d3c1a24e65f5d3c98d4bedc6884380d83c226152
Развёрнутый runtime commit: fe7420d949fb6f9bbac64bed7cba229ae1e6ead6

## Цель и критерии приёмки

Один публичный HTTPS origin для frontend и Go API; свежие меню/стопы,
работающая корзина и оформление, состояние после restart, безопасный откат.
Не создавать реальные заказы, не подключать Telegram и QR.
Публичная приёмка ещё не выполнена; наличие backend на VPS не означает её успех.

## Контекст и решения

- Владелец выбрал существующий VPS Timeweb: Ubuntu 24.04.4 LTS, Nginx 1.24,
  x86_64; SSH root@72.56.232.231:22 по уже установленному локальному ключу.
  Перед работой: доступно около 590 МБ RAM и 9,7 ГБ диска; 8080 свободен.
- Поручено: отдельный beini.service на **127.0.0.1:8080**, не менять
  /var/www/avtorstudio и /etc/nginx/sites-available/avtorstudio.com,
  не дублировать map; nginx -t перед каждым reload/restart Nginx.
- Позднее уточнение владельца: **создать чистый /var/lib/beini**, локальный
  журнал теста №12540 не переносить. Старый локальный журнал не удалялся.
  Это заменяет первоначальный пункт поиска журнала для переноса.
- Домен beinirest.ru по WHOIS зарегистрирован и припаркован в REG.RU
  (REGRU-RU), NS ns1.reg.ru/ns2.reg.ru. Начальные A apex/www — 95.163.244.138.
  Требуемые A apex/www — 72.56.232.231; остальные записи и NS сохранить.
- Владелец разрешил действовать самостоятельно и вошёл в REG.RU в обычном
  Chrome. Встроенная браузерная вкладка не получила эту сессию.
  Управление обычным Chrome остановлено инструментом: невозможно достоверно
  определить текущий URL для контроля доступа. После остановки действия в этом
  окне не выполнялись. DNS через него не изменены; альтернативный доступ к DNS
  не получен. Не считать исходный IP парковки IP сервера.
- Документ другой работы docs/tasks/iiko-empty-api-diagnosis-2026-11-02.md
  сохранён без изменений и не включён в commit. Исходный SHA-256:
  67FD1EEEC493796393E05F5E4B87849F6584DB54EE14AD8E2D9FA2D96D994633.

## Сделано

- Добавлен HTTP_LISTEN_ADDR с прежним PORT fallback; явный неверный адрес
  не вызывает fallback на публичный listener. Реальный bind проверен тестом.
- Из лога iiko убрано имя гостя, из ошибок — сырые upstream body.
  Regression-тесты воспроизвели утечки до правок и прошли после них.
- Подготовлены deploy/beini.service, ENV, отдельные HTTP/HTTPS vhost, Certbot
  hook и настройки отдельного namespace journald (32 МБ, 14 дней).
- Добавлена сборка scripts/package-release.py. Архив fe7420d проверен:
  82 entries, 3 PNG, seed присутствует, binary 0755, конфигов/журналов нет;
  SHA-256 каждого файла проверен на сервере по release.json.
- На VPS созданы только отдельные пользователь beini, каталоги BEINI и ACME;
  релиз /opt/beini/releases/fe7420d949fb6f9bbac64bed7cba229ae1e6ead6,
  ссылка /opt/beini/current. Секрет передан по SSH в /etc/beini/iiko_config.json,
  root:beini 0640; каталог /etc/beini 0750. Значения не выводились.
- beini.service enabled/active, HTTP только 127.0.0.1:8080, MemoryMax 192M.
  Наблюдалось около 6,7 МБ MemoryCurrent, NRestarts=0 после запуска.
- /var/lib/beini — beini:beini 0700, orders.jsonl и menu-cache.json — 0600.
  Production orders пустой: реальные заказы не отправлялись. Telegram отключён,
  production notifications.jsonl не создавался.
- Добавлены только /etc/nginx/sites-available/beinirest.ru и ссылка sites-enabled.
  Сейчас это HTTP/ACME bootstrap: **503 для приложения**, challenge webroot
  /var/lib/beini-acme. HTTPS-конфиг ещё не установлен, сертификата BEINI нет.
  После nginx -t выполнен reload, не restart. Сразу после reload первый запрос
  ещё обслужился старой конфигурацией; последующий GET/HEAD подтвердил 503.
- Certbot уже установлен; существующий certbot.timer работает, одна ACME
  регистрация найдена. Добавлен отдельный deploy hook, действующий только для
  lineage /etc/letsencrypt/live/beini и выполняющий nginx -t перед reload.
- Обычный push fe7420d в origin/codex/public-deployment выполнен. Main пока
  на момент первичного push оставался d3c1a24; далее подготовлена fast-forward
  интеграция вместе с этим отчётом, без force-push.

## Проверки

- Исходный GitHub CI d3c1a24: Windows/Linux success, run 41435872963;
  Pages success, run 41435872120.
- Новый GitHub CI fe7420d: оба jobs Windows/Linux success,
  https://github.com/XDobriev/beini/actions/runs/41440891260.
- Полная python scripts/check.py после изменений: All checks passed.
  Go tests/vet/build, JS/checkout, 14 Python-тестов, 35 согласованных блюд.
  Sandbox требует разрешённого loopback; боевые ключи проверка не использует.
- Read-only code/config review immutable d3c1a24..fe7420d: Important/Critical
  замечаний нет. Minor: packager собирает текущую working directory, поэтому
  untracked Go-файлы теоретически могут повлиять на binary. В этой сборке
  untracked только чужой Markdown; исходники и manifest соответствуют commit.
- VPS: systemd-analyze verify прошёл; health 200; menu 200/no-store, 35 items,
  orderingAvailable=true, menuStale=false, stopListStale=false. Origin preflight
  https://beinirest.ru разрешён; POST заказа не выполнялся.
- 11 private/missing путей localhost возвращают 404: config, .env/.git,
  orders/notifications/cache, seed, backup, исходник, missing asset/API.
  67 публичных файлов релиза (включая все фото/PNG/JS/CSS/HTML) — 200;
  assets/images/instagram/manifest.json ожидаемо 404 как служебный JSON.
- avtorstudio.com после reload — HTTP 200. Его Nginx config SHA-256 до/после:
  80da1488dcfa7ff5b2389d2e11d1b3d382dc297dc5175c0238dbd5f39e8b82a9.
  Отпечаток имён/размеров/mtime всего /var/www/avtorstudio также совпал:
  90b08b0fbdf6dbe53d4e92f5cc63f6e4085ceb9be63973a6fd657beb94e5b013.
- nginx -t проходит с прежним предупреждением protocol options redefined
  в supabase-proxy. Его конфиг и настройки firewall не менялись.
- Внешние probes из текущей Windows-сети с подменённым Host маршрутизируются
  к парковке REG.RU (Server: openresty), а не к указанному IP. TCP connect 8080
  создаётся, но HTTP-ответа нет. Это не доказательство доступности/закрытости
  порта с обычного интернета; loopback bind подтверждён ss на VPS.

Дополнительная проверка VPS:

- Реальный `systemctl restart beini`: новый PID, существующий пустой
  orders.jsonl сохранил inode и SHA-256, menu-cache.json остался на диске.
  После рестарта health 200, orderingAvailable=true, menuStale/stopListStale=false;
  beini enabled/active, systemd-journald@beini active.
- Linux-бинарник тестов из fe7420d выполнен на VPS: TestApplicationMenuOrderRestart
  confirmed/rejected/lost-response — PASS; оба теста bind/fallback — PASS.
  Только mock iiko и /var/lib/beini-verification; production конфигурация не читалась.
  Непустое восстановление подтверждено синтетическими заказами, без кассы.

## Осталось

1. Получить рабочее подключение к авторизованной панели REG.RU с проверяемым URL
   либо безопасный API-доступ. Владелец просит менять DNS самостоятельно; не
   требовать повторного согласования двух A-записей, если доступ восстановлен.
2. Изменить только A apex/www на VPS, проверить authoritative DNS/CAA/AAAA.
   Выпустить beini через существующий ACME account и webroot, установить
   deploy/nginx-https.conf, nginx -t, reload; проверить renewal dry-run.
3. Публичный desktop/mobile: меню, поиск, изображения, корзина и оформление
   без отправки. Проверить HTTPS, freshness/no-store, приватные пути, внешний
   8080, логи. Пока эти проверки НЕ выполнены.
4. По завершении публикации обновить итоговые документы, выполнить проверки,
   Conventional Commit и обычный push; проверить CI. Чужой документ сохранить.

## Блокеры и ограничения

- Публичное приложение на beinirest.ru ещё не запущено: DNS указывает на парковку,
  сертификат BEINI не выдан. Нельзя выдавать парковку HTTPS за HTTPS приложения.
- Browser UI был остановлен из-за невозможности определить URL, не из-за отказа
  владельца. Не обходить остановку продолжением действий в той же UI-сессии.
- Новых реальных заказов нет. Отмена прежнего №12540 по-прежнему не подтверждена;
  публичную кассовую приёмку проводить отдельно с владельцем и отменой теста.
- Непустое восстановление заказа проверяется mock-тестами; пустой production
  journal/cache сами по себе не доказывают восстановление реального заказа.
- Telegram, оплата/фискализация и условия доставки не включены в работу.

## Откат и следующий шаг

Инструкция: docs/deployment.md. Сейчас это первый релиз без предыдущего боевого:
при аварии остановить только beini; его HTTP-vhost уже отдаёт 503. Сохранить
/var/lib/beini, не откатывать orders.jsonl/notifications.jsonl. Другой проект
не перезапускать и его конфиги не заменять.

Следующий инфраструктурный шаг — восстановить разрешённый доступ к REG.RU и
внести две уже согласованные A-записи; затем закончить HTTPS и публичную приёмку.

## Отдельная следующая задача

Владелец 2 ноября попросил разобрать неполноту меню в новом чате. Сейчас seed
содержит 35 позиций; iiko v3 отдавал 313 продуктов/34 группы, в allowlist35
находились 33 ID. Полный импорт/категории/поддерживаемые варианты требуют
отдельной диагностики; это не часть инфраструктурной публикации.
