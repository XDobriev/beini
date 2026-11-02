# Публичное развёртывание BEINI

Статус: инфраструктурная публикация завершена; кассовая приёмка отдельно
Обновлено: 2 ноября 2026
Целевая ветка: main; подготовка: codex/public-deployment; завершение: codex/public-https
Исходный commit продолжения: e9e1521
Исходный commit: d3c1a24e65f5d3c98d4bedc6884380d83c226152
Развёрнутый runtime commit: fe7420d949fb6f9bbac64bed7cba229ae1e6ead6

## Цель и критерии приёмки

Один публичный HTTPS origin для frontend и Go API; свежие меню/стопы,
работающая корзина и оформление, состояние после restart, безопасный откат.
Не создавать реальные заказы, не подключать Telegram и QR.
Публичная приёмка интерфейса без отправки заказа выполнена 2 ноября, вечером (MSK).
Публичные адреса: https://beinirest.ru и https://www.beinirest.ru.

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
  На этапе подготовки это был HTTP/ACME bootstrap: **503 для приложения**, challenge webroot
  /var/lib/beini-acme. До продолжения HTTPS-конфиг не был установлен и сертификата не было.
  После nginx -t выполнен reload, не restart. Сразу после reload первый запрос
  ещё обслужился старой конфигурацией; последующий GET/HEAD подтвердил 503.
- Certbot уже установлен; существующий certbot.timer работает, одна ACME
  регистрация найдена. Добавлен отдельный deploy hook, действующий только для
  lineage /etc/letsencrypt/live/beini и выполняющий nginx -t перед reload.
- Обычный push fe7420d в origin/codex/public-deployment выполнен. Код fe7420d
  и отчёт e9e1521 затем отправлены в main; CI Windows/Linux обоих прошёл.

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

## Завершение DNS/HTTPS — 2 ноября, вечером (MSK)

- Владелец изменил две A-записи в панели; при первой проверке оба NS ещё
  возвращали парковку. Изменения публиковались по очереди: сначала apex,
  затем www. В 21:06 MSK оба authoritative NS REG.RU и resolver 1.1.1.1
  возвращали 72.56.232.231 для обоих имён. NS сохранены; AAAA и CAA не найдены.
- Certbot выпустил lineage beini через существующий production ACME account
  и /var/lib/beini-acme. Сертификат покрывает оба имени, действует до
  31 января 2027, 20:08 MSK. Новое принятие условий не потребовалось.
- Установлен только vhost beinirest.ru из deploy/nginx-https.conf;
  bootstrap сохранён в /etc/beini/nginx-http-bootstrap.conf.
  nginx -t прошёл **до reload**. Binary, ENV и конфигурация iiko не менялись.
- certbot renew --cert-name beini --dry-run: все simulated renewals succeeded.
  Certbot создал staging account для проверки; production account не заменён.
  certbot.timer active, отдельный deploy hook BEINI сохранён.
- Оба публичных HTTPS-имени: health 200; HTTP: 301 на соответствующий HTTPS.
  /api/menu: 200, Cache-Control: no-store, 35 позиций,
  orderingAvailable=true, menuStale=false, stopListStale=false.
- Все 67 публичных файлов release manifest, включая фото/логотипы/JS/CSS/HTML,
  вернули 200 через HTTPS; полные тела (6 406 711 байт) совпали по SHA-256
  с release manifest. Все 43 img элемента в Chrome полностью загрузились
  и декодировались; desktop/mobile screenshots просмотрены. Принудительная
  загрузка всех фото в этой сети не уложилась в 60 секунд, повтор с лимитом
  180 секунд прошёл; скорость других сетей этим не подтверждена.
  11 private/missing путей из предыдущего списка
  вернули 404 через публичный origin. Соединение к 72.56.232.231:8080 с VPS
  отклонено; ss подтверждает только loopback listener. Прямой Windows probe
  порта 8080 завершился timeout без HTTP-ответа, приложение с него не получено.
- Playwright, отдельный Chrome: desktop 1440x1000 и mobile 390x844.
  Свежий API/no-store, поиск «плов», добавление, корзина после reload,
  открытие доставки и самовывоза, отсутствие горизонтального overflow — PASS.
  JS page errors: 0; POST заказа: 0. POST дополнительно блокировался в тестовом
  context; форма не отправлялась, контактные данные не вводились.
- Прямой браузерный тест использовал --no-proxy-server и явное сопоставление
  beinirest.ru -> 72.56.232.231 только в отдельном тестовом Chrome;
  сертификат проверялся штатно. Обычная Windows-сеть и встроенный браузер ещё
  видели парковку/502 openresty. Это ограничение распространения DNS,
  прежний TTL 86400 секунд; обновление всех внешних кэшей не подтверждено.
  Прямой curl --resolve подтвердил remote_ip=72.56.232.231 и health 200.
- python scripts/check.py в изолированном worktree с разрешённым loopback:
  All checks passed (Go tests/vet/build, JS/checkout, 14 Python-тестов, 35 блюд).
  Первый sandbox-запуск без разрешённого loopback не прошёл Go mock-сценарии;
  бизнес-логика для обхода ограничений не менялась.
- Сервис сохранил PID 1685232, NRestarts=0. orders.jsonl остаётся пустым,
  0600 beini:beini. Новых реальных заказов нет; сохранённые выше реальные
  restart и mock-восстановление относятся к тому же runtime fe7420d.
- К началу продолжения avtorstudio был независимо изменён после предыдущего
  отчёта. Новый baseline config SHA-256:
  68e89f59ee33e1994ff42035907306e1e576322428928f4034c36ef272b33f7a.
  Дерево файлов (find %P %s %T@, LC_ALL=C sort, sha256sum):
  7dd214bc1abf003a09e6f947d638a032a04899a9d172f86d2c2295b937650f6d.
  Оба отпечатка до/после установки HTTPS BEINI совпали. Файлы и конфиги другого
  проекта не менялись. nginx -t до правок уже показывал предупреждения
  protocol options redefined в supabase-proxy и conflicting server name
  api.avtorstudio.com на 80/443; они сохранены, исправление вне этой задачи.

## Осталось и ограничения

- Публичный инфраструктурный запуск выполнен. Документы продолжения находятся
  в commit с сообщением docs(ops): record public HTTPS acceptance;
  runtime остаётся fe7420d. Результат CI этого commit проверяется после push.
- Обычные сети с прежним DNS-кэшем могут временно видеть парковку.
- Полное меню разбирается отдельно; каталог не расширялся.
- Старый тест №12540: отмена не подтверждена. Публичная кассовая приёмка,
  сигнал/печать/отмена требуют отдельного согласования присутствия владельца.
- Telegram, оплата/фискализация и условия доставки не включены в работу.
- Существующие canonical/og:url/JSON-LD в index.html ещё используют beini.rest;
  метаданные требуют отдельной правки с публикацией нового статического релиза.

## Откат и следующий шаг

Runtime/source: fe7420d949fb6f9bbac64bed7cba229ae1e6ead6.
Запуск: beini.service, /opt/beini/current, конфигурация /etc/beini,
состояние /var/lib/beini. Полный регламент: docs/deployment.md.

Это первый runtime-релиз без предыдущего проверенного боевого. При аварии
остановить только beini, восстановить только его vhost из
/etc/beini/nginx-http-bootstrap.conf, выполнить nginx -t, затем reload Nginx.
Сохранить /var/lib/beini целиком, не откатывать и не удалять
orders.jsonl/notifications.jsonl. Другой проект не перезапускать.

Следующий продуктовый шаг — отдельно согласовать кассовую приёмку с владельцем.

## Отдельная следующая задача

Владелец 2 ноября попросил разобрать неполноту меню в новом чате. Сейчас seed
содержит 35 позиций; iiko v3 отдавал 313 продуктов/34 группы, в allowlist35
находились 33 ID. Полный импорт/категории/поддерживаемые варианты требуют
отдельной диагностики; это не часть инфраструктурной публикации.
