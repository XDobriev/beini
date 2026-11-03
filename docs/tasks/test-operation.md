# Тестовая эксплуатация: витрина и доступ владельца

Статус: опубликовано в режиме tester; готово к проверке владельцем
Обновлено: 3 ноября 2026 (Europe/Berlin)
Ветка: codex/test-operation, отдельный управляемый worktree
Исходный commit: 38aad78 (актуальный origin/main при начале)
Итоговый commit: commit этой задачи с сообщением `feat: add protected test operation mode`

## Публикация к утренней проверке — 3 ноября

После локальной реализации владелец поручил «доделай задачу до конца, чтоб
утром я мог проверить» и отдельно выбрал генерацию пароля в закрытый локальный
файл. Это последующее поручение разрешило публикацию/перезапуск BEINI;
реальная отправка заказа с сотрудником по-прежнему отдельно.

- Актуальный origin/main7bd5445 с выбором порций объединён с6a04777 обычным
  merge. Конфликты только текста архитектуры и версий HTML-ресурсов разрешены
  с сохранением обоих изменений. Итоговый runtime:
  `a66e4853c2f7275f20499d86ef2d650527ac7636`.
- После merge полный `python scripts/check.py` прошёл. Linux/amd64-пакет:
  74 manifest-файла, SHA256 проверены, конфигурация/журналы отсутствуют.
  Тест порций получил явный owner mock transport, исходные ID/цены сохранены.
- Случайный пароль192bit создан без вывода в чат/логи и сохранён вне checkout
  в локальный файл с ACL владельца и SYSTEM. Персональный машинный путь передан
  владельцу отдельно, в Git его нет. Только PBKDF2-verifier по SSH установлен
  в `/etc/beini/tester.env`, root:beini0640; исходный пароль на VPS не сохранялся.
- Первый прямой SSH-маршрут не получил banner. Для того же VPS использован
  существующий локальный SOCKS с domain-based маршрутом, без изменения VPN,
  DNS и настроек SSH; проверка прежнего host key сохранена.
- Перед переключением: закрытые backup unit/ENV/состояния и fingerprint
  защищённых файлов в `/var/lib/beini-deploy-reports/test-operation-a66e485`.
  Архив извлечён в новый release, явно выставлены0755/0644 и пройдён preflight
  чтения manifest/binary от пользователя beini. `systemd-analyze verify` прошёл.
- В unit добавлен только дополнительный EnvironmentFile; в service.env
  заданы tester и LocalHTTP0. После daemon-reload остановлен только beini,
  сохранено состояние, атомарно переключён current и запущен тот же сервис.
  PID1702187, active/enabled, NRestarts0, listener127.0.0.1:8080.
- Orders сохранил inode304860, размер0, SHA256 пустого файла и права0600.
  Notifications journal отсутствовал и не создавался. menu-cache штатно
  обновлён:251 товар/58 товарных вариантов/29 групп, свежие обе ленты.
- iiko config и оба BEINI/avtorstudio vhost совпали по SHA256; отпечаток3631
  записи дерева avtorstudio до/после совпал. Nginx/DNS/другие сервисы не менялись.
- Оба HTTPS-имени: health/menu/access200, mode=tester, guestCanOrder=false,
  access/menu no-store. Index/tester/3JS/menu-data/CSS совпали с manifest;
 7 приватных путей на каждом имени404. Owner login/logout и безопасные cookie
  проверены через HTTPS, без запроса создания заказа.
- Публичный браузер: desktop1440×1000 и mobile390×844,222 карточки; поиск,
  корзина, guest call, реальный вход, owner delivery/pickup, предупреждение
  реальной отправки, выход, стакан260/графин650, отсутствие overflow/JS errors.
  Контактные данные не вводились, финальная кнопка не нажималась; корзина очищена.

Утренняя проверка: открыть https://beinirest.ru/tester, взять пароль из
переданного закрытого файла, войти и перейти к меню. Для просмотра/корзины
настоящий заказ не нужен. Финальное оформление создаёт реальный заказ:
только с сотрудником, проверкой карточки/суммы/сигнала/печати и отменой.
Следующие разделы сохраняют историю исходного локального этапа.

## Цель и критерии приёмки

Гостям — меню/поиск/корзина и телефон, владельцу — защищённый вход и обычный
checkout с тем же handler/journal. Три серверных режима, безопасный default,
отзыв сессии/CSRF/защита перебора, сохранение key/body и GET статуса после
потери доступа. Ежедневный mock без ключей, тесты, документация и локальный
Conventional Commit. Реальных заказов, push и публикации в поручении нет.

## Контекст и решения

- Основной checkout содержал параллельные незавершённые изменения порций/меню.
  Они не переносились и не менялись. Работа создана от origin/main в отдельной
  ветке; seed275, public251 и hidden24 в diff этой задачи не изменялись.
- Более позднее состояние, чем исходные вводные: read-only `/opt/beini/current`
  подтвердил runtime865162230fff2c79597b6cc7ad0a18fd9e97be39, публичный API251,
  свежие меню/стопы. Это сверка2ноября, не развёртывание этой задачи.
- Один процесс и один действующий журнал. Session в памяти, restart отзывает
  вход, но worker/journal сохраняют заказы. Нового staging-поддомена нет.
- PBKDF2 стандартной библиотеки Go1.24 (HMAC-SHA256/600000, соль16, ключ32),
  random opaque session ID и HMAC CSRF. Источники параметров:
  [Go crypto/pbkdf2](https://go.dev/pkg/crypto/pbkdf2/),
  [OWASP Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
- Краткие [спецификация](../superpowers/specs/2026-11-02-test-operation.md)
  и [план](../superpowers/plans/2026-11-02-test-operation.md) записаны до реализации.

## Сделано

- `internal/access`: closed/tester/public, defaultclosed, server-only ENV,
  login/logout, срок8ч, HttpOnly/Secure/Strict cookie, process revocation,
  CSRF/Origin/JSON, глобальный limiter5/мин и2KDF. Неверная конфигурация fail-closed.
- Application gate стоит перед прежним CreateOrder. Цена, стопы, свежесть,
  состав, идемпотентность и state worker остаются прежними. Status по приватному
  ключу не требует входа и доступен при закрытии режима.
- `/tester`, плашка над шапкой, выход, гостевой звонок вместо создания заказа,
  предупреждение владельца о реальной отправке. Корзина не требует входа.
- Перепроверка доступа перед POST/по таймеру/focus/visibility, синхронизация
  вкладок, неизменная попытка при expiry/logout/unknown. Web Lock не допускает
  создания разных ключей одновременно; без поддержки браузер предлагает обновить
  его или позвонить. Ошибка browser storage не теряет существующий ключ.
- Python mock fixture с настоящим HTTP auth-контрактом, no-store/CSRF и учебной
  кассой; pending/confirmed/rejected/drop/dropBefore/replay, expiry/modes/stops.
  MOCK сохраняется и в browser attempt, поэтому refresh не выдаёт учебный успех
  за реальный. `mock-control.py` работает только с127.0.0.1 и проверяет mock=true.
- Безопасное создание хеша через stdin и `tester-password.py`: скрытый ввод,
  новый закрытый файл вне checkout/STATIC_DIR, без вывода хеша/пароля. Опциональный
  tester.env в шаблоне systemd. README/product/architecture/design-system/
  roadmap/testing/operations/deployment обновлены вместе с кодом.

## Проверки

- Baseline и повторные `python scripts/check.py` с разрешённым loopback —
  All checks passed. Go tests/vet/build, JS syntax и regression, Python mock4,
  importer16/audit8, согласованный JSON/JS251 и аудит seed275. Последний итоговый
  прогон фиксируется перед коммитом; build/release отдельно от runtime на VPS.
- RED→GREEN: отсутствие модуля доступа; гостевой create до gate;
  frontend отсутствие access gate; две вкладки создавали2POST вместо1;
  потеря существующего key при storage error. Исправления прошли проверки.
  Отдельный regression запрещает старому GET access стирать cookie нового
  входа из другой вкладки; cookie сессии меняют только login/logout.
  Fixture regression подтвердил непрочитанное тело при CSRF403; bounded drain
  предотвращает Windows reset до получения клиентом HTTP-отказа.
- Go mock integration: owner pickup/delivery, цена/состав, stop/stale-menu/
  stale-stop с upstream0, pending/confirmed/rejected/lost-response/replay/restart,
  сохранность попытки при переключении режимов. Guest create вызовов нет.
- Сессии/пароль/подделка/expiry/logout/restart, CSRF, no-store, ENV serialization,
  proxy TLS/local HTTP и KDF capacity закреплены Go-тестами. Ответы и логи не
  содержат пароля, verifier или session ID в JSON body.
- Node выполняет настоящий app.js и access.js: guest не отправляет POST,
  повтор сохраняет key/body, expiry/межвкладочный lock/slow access/storage error,
  свежий CSRF и отказ от POST после потери допуска.
- Браузер IAB: mobile390×844 и desktop1440×1000, неверный пароль/Enter,
  меню/поиск/корзина, доставка/самовывоз, pending refresh, expiry/new login,
  mock confirmed/rejected, lost response до записи и явный retry, logout/login
  из соседней вкладки, Escape/возврат фокуса, прокрутка/overflow. Создано только
 2 синтетических заказа, повтор/refresh/вход не увеличивали creates.
  Физическая экранная клавиатура телефона не проверялась; проверен viewport.
- Независимое read-only ревью нашло две ошибки (межвкладочные ключи и ввод при
  slow access); исправлены и закреплены тестами. После повторного просмотра
  новых actionable findings не осталось; дополнительные proxy/KDF/client
  проверки добавлены по замеченным пробелам покрытия.
- Чужой untracked `iiko-empty-api-diagnosis-2026-11-02.md` сохранён вне этой
  ветки, SHA25667FD1EEEC493796393E05F5E4B87849F6584DB54EE14AD8E2D9FA2D96D994633.
  В commit его нет. Production/local journals №12540 не открывались и не менялись.

## Осталось и ограничения

Локальный функционал завершён. Публикация и реальная приёмка не выполнялись.
Пароль владельца в задаче не задавался; его создаёт владелец перед настройкой
server-only tester.env. Без него вход закрыт. После restart нужен вход заново.
Лимитер входа глобальный: атака может на минуту заблокировать и владельца.
Сессии apex/www раздельны. Состояние Python fixture в памяти; надёжность файлов
и restart проверяет Go, не fixture. Веб-приложению нужен браузер с Web Locks.

## Следующий шаг

Отдельно поручить публикацию по [точному регламенту](../deployment.md#следующее-развёртывание-тестовая-эксплуатация):
объединить с актуальным main → checks/пакет → закрытые ENV/hash/unit → тот же
BEINI_STATE_DIR и только beini restart → HTTPS guest/owner проверки без заказа.
Затем отдельное поручение на приёмку с сотрудником и обязательной отменой.
Не откатывать журналы. Старый runtime8651622 не содержит gate: его возврат
не гарантирует закрытие заказов, аварийный безопасный вариант — BEINI503.
