# CLAUDE.md

Гайд для Claude Code и людей. Объясняет, что делает сервис, где жить кодом, и как им пользуются вызывающие команды для тестов.

## Что это

`chaospay` — локальный мок банковских (Freedom Pay, в планах больше) API для тестирования `payment-gateway` (PG). Сейчас покрывает **Freedom Bank** (Freedom Pay XML, FreedomQR, Loyalty), но архитектура нейтральна — можно добавлять Kaspi, IOKA, JetPay, EPAY, Flitt тем же шаблоном.

Цель — на стороне PG можно дёрнуть оплату в обычном flow, а мок ответит **ровно так, как нужно тесту**: ошибкой банка, таймаутом, битым XML, рассинхроном статусов. Управление — через HTML-панель (`http://localhost:48532/panel`) или programmatic API.

## Архитектура

Hexagonal/clean, всё in-memory (рестарт = чистый стейт):

```
cmd/chaospay/main.go              ← entry point, ручной DI
internal/
├── config/                        ← Config из ENV
├── domain/                        ← zero-deps типы и константы
│   ├── pay/                       ← PaymentRecord, Status, Kind
│   ├── qr/                        ← QR-code, Status
│   ├── scenario/                  ← Scenario, Action — центральная сущность
│   └── requestlog/                ← Entry для request log
├── application/                   ← services (use-cases)
│   ├── pay/                       ← HoldInit/Hold/Capture/Cancel/Revoke/...
│   ├── qr/                        ← Generate/GetStatus/ChangeStatus/Refund
│   └── scenario/                  ← Add/Remove/Match/ApplyPreset
├── infrastructure/                ← реализации
│   ├── freedompay/                ← XML, MD5 signature, replace
│   ├── memstore/                  ← in-memory PayRepo/QRRepo/ScenarioStore/RequestLog
│   ├── pgclient/                  ← webhook-клиенты на PG
│   └── qrgen/                     ← skip2/go-qrcode wrapper
└── ports/                         ← внешние интерфейсы
    ├── api/                       ← HTTP handlers
    │   ├── pay/                   ← Freedom Pay XML endpoints (init/direct/...)
    │   ├── wallet/                ← /pay/{id}/pay (Apple/Google Pay)
    │   ├── qr/                    ← /qr-code/* (QR-PAY)
    │   ├── loyalty/               ← /authservice + /loyaltyservice
    │   ├── health/                ← /health
    │   └── scenarioapply/         ← общий transport-applier для XML и JSON
    └── panel/                     ← HTML-панель (5 вкладок)
```

**Ключевая концепция — Scenario.** Это правило вида «когда придёт запрос на endpoint X с paymentID=Y, ответь действием Z». Сценарий применяется ОДИН раз (если `ConsumeOnce=true`) или каждый раз. Сценарии не модифицируют PaymentRecord — только ответ. Это даёт точечный контроль без правки кода.

## Запуск и dev workflow

```bash
docker compose build && docker compose up -d
curl http://localhost:48532/health        # → OK
open http://localhost:48532/panel
```

Локальная сборка без Docker:
```bash
go build -o /tmp/chaospay ./cmd/chaospay
/tmp/chaospay
```

После правок Go-кода — пересобрать контейнер: `docker compose build && docker compose up -d`.

## Как добавлять scenario actions

Новый action добавляется так:

1. **Константа** в [internal/domain/scenario/scenario.go](internal/domain/scenario/scenario.go): `ActionFooBar Action = "foo_bar"`, добавить в `AllActions`.
2. **Реализация**:
   - **Transport-level** (universal: обрывает соединение/возвращает мусор/код) → добавить в [internal/ports/api/scenarioapply/transport.go](internal/ports/api/scenarioapply/transport.go). Автоматически работает во всех handler-ах через `scenarioapply.Transport(...)`.
   - **Content-level для XML** (модифицирует поля валидного ответа) → добавить в `applyScenarioAfter` в [internal/ports/api/pay/scenario.go](internal/ports/api/pay/scenario.go).
   - **Content-level для wallet (JSON)** → switch в [internal/ports/api/wallet/handler.go](internal/ports/api/wallet/handler.go).
3. **Параметры формы** (если нужны новые) — добавить input в [internal/ports/panel/scenarios.go](internal/ports/panel/scenarios.go) и в список ключей в `handleScenarioAdd` в [internal/ports/panel/controller.go](internal/ports/panel/controller.go).

## Как добавлять preset

Preset — это «1 кнопка = N сценариев», для воспроизведения типичных инцидентов одним нажатием.

В [internal/application/scenario/service.go](internal/application/scenario/service.go):
1. Добавить `PresetInfo` в `AllPresets` (для рендера кнопки).
2. Добавить `case "имя"` в `ApplyPreset` с нужными `add(...)` вызовами.

В UI пресеты раскладываются по 5 группам-карточкам: **Incidents / Retry and timeouts / Business declines / Broken responses / Data integrity**. Группировка и цвет кнопки определяются по имени пресета функциями [`scenarioPresetGroup`](internal/ports/panel/scenarios.go) и [`presetButtonClass`](internal/ports/panel/scenarios.go).

Если имя нового preset-а содержит:
- `retry` или `deadline` → Retry and timeouts (btn-warning)
- `funds`/`declined`/`fraud`/`expired`/`3ds`/`limit` → Business declines (btn-danger)
- `malformed`/`empty`/`slow` → Broken responses (btn-warning)
- ничего из вышеперечисленного и не `ex1001`/`desync`/`hold_timeout` → Data integrity (btn-purple)

Если для нового preset-а нужна другая группа/цвет — добавь case в `scenarioPresetGroup`. **Не используй inline-эвристику в `renderScenariosTab`** — всё через эти две функции, иначе классификация разъедется.

## Как мок интегрируется с PG

PG обращается к моку по DNS-имени контейнера `chaospay:8532` в общей docker-сети `dockernet-local`. Для этого в PG `.env` указано:

```
FREEDOM_PAY_HOST=http://chaospay:8532
FREEDOM_PAY_CUSTOMER_HOST=http://chaospay:8532
```

Подпись XML-запросов проверяется по MD5 алгоритму, совместимому с PG SDK (`pkg/freedompay/freedompay.go`). `CHAOSPAY_FREEDOM_SECRET` должен совпадать с `merchantSecret` тестового терминала в БД PG (по умолчанию `mock-secret-key`, merchant_id=100001, terminal_id=1).

Кабинетов может быть несколько: `CHAOSPAY_FREEDOM_MERCHANTS=merchant_id:secret,merchant_id:secret`. Ключ выбирается по `pg_merchant_id` запроса, им же подписывается ответ и постлинк платежа. Подробности — в [docs/setup.md](docs/setup.md).

## Testing для команд, использующих мок

Сценарий типичного теста (например, реакции PG на ошибку банка):

1. Перед прогоном теста — `POST /panel/scenarios/preset` с нужным `preset`.
2. Дёргаешь обычный платёжный flow в PG (через rahmet-app / postman / e2e).
3. Проверяешь поведение PG (статус заказа, логи, события).
4. После теста — `POST /panel/scenarios/reset` для cleanup.

Curl-примеры и каталог пресетов — в [docs/errors-playbook.md](docs/errors-playbook.md).

## Конвенции

- Имя Go-модуля: `chaospay`. Имена файлов — `lowercase.go`, без подчёркиваний.
- Все типы — в `domain/`, без зависимостей на infra.
- Application sevice принимает Store-интерфейс, не конкретный memstore.
- Webhook-клиенты — отдельные адаптеры в [internal/infrastructure/pgclient/](internal/infrastructure/pgclient/).
- При добавлении нового endpoint в `pay/handler.go::Register` — НЕ забывай прописать его в `domain/scenario/scenario.go::AllEndpoints`, иначе matcher не подскажет его в UI.
- Подпись ответа: `responseScriptName` в `xmlEndpoint(...)` должен совпадать со `scriptName` команды PG SDK (см. `commands/<op>/command.go: ScriptName`). PG валидирует ответ через `command.GetScriptName()`, а не через `Response.GetScriptName()` — поэтому пустая строка приведёт к `invalid signature` (актуально для `init`/`direct`).
- Все новые сценарные actions — обязательно в `AllActions` (для UI dropdown).

## Документация

- [docs/setup.md](docs/setup.md) — пошаговый setup мока + PG
- [docs/architecture.md](docs/architecture.md) — слои, DI, state-машины
- [docs/api.md](docs/api.md) — endpoints с примерами
- [docs/signature.md](docs/signature.md) — MD5-подпись (1-в-1 с PG SDK)
- [docs/scenarios.md](docs/scenarios.md) — каталог actions/params
- [docs/errors-playbook.md](docs/errors-playbook.md) — **воспроизведение ошибок (для тестировщиков)**
- [docs/ex1001.md](docs/ex1001.md) — пошаговое воспроизведение инцидента EX-1001

## Что НЕ реализовано (известные ограничения)

- Persistence на диск (всё in-memory, рестарт = чистый стейт)
- TLS / `extra_hosts`-подмена `*.freedompay.kz`
- 3DS Challenge flow (поля принимаем, не симулируем)
- Loyalty integration в Pay (поля принимаем, не процессим)
- Recurring payments, receipt position validation
- Дедупликация `pg_idempotency_key` (намеренно — для EX-1001 нужно её отсутствие)
- Mock других банков (Kaspi/IOKA/JetPay/EPAY/Flitt) — архитектура готова, реализаций нет

## Правила для Claude

- При правках кода — **проверяй сборку**: `docker exec chaospay go build ./...` (или локально `go build ./...`).
- Не плоди .md — обновляй существующие. Новый док только если запросили явно.
- Не лезь в `tmp/` без явной просьбы — там рабочие заметки.
- Магические строки → именованные const (правило из глобального CLAUDE.md).
- Эмоджи в UI-кнопках/документации — ок, в коде — нет.
- Не вставляй relative-ссылки на код Payment Gateway (`../...`) в публичную документацию — у пользователей ChaosPay нет доступа к чужому коду. Если нужна ссылка на PG-код для контекста — упомяни путь в backticks без markdown-link.
