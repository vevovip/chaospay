# ChaosPay

> Chaos engineering для платёжных интеграций. Локальный мок банковских API, который умеет воспроизводить реальные production-сбои банка на ваш выбор — таймауты, отклонённые карты, ambiguous-ответы, retry-exhausted, битые payload-ы, рассинхрон списаний.

[![CI](https://github.com/vevovip/chaospay/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vevovip/chaospay/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/tests-150%2B-brightgreen)](#тестирование)
[![Scenarios](https://img.shields.io/badge/scenarios-30%2B-blue)](#категории-пресетов)

## Зачем

Когда вы интегрируете payment gateway с банком, проблемы возникают **не в happy-path**, а в граничных случаях:
- Банк не ответил вовремя — Capture-таймаут пришёл, а деньги списались на банке
- Hold вернул `pg_status=pending` — нужно правильно полить Status и не уронить заказ
- Карта отклонена с кодом 10009 — должны ли мы дать клиенту повторить, или сразу `failed`
- Freedom Pay вернул `200 OK` с пустым body — как PG это обработает?
- DNS-таймаут / retry-exhausted после 3 попыток — что увидит пользователь?

Воспроизвести всё это в проде вручную невозможно — а пропуск багов стоит денег и репутации.

**ChaosPay** — это локальный mock-сервер, который притворяется банком (сейчас Freedom Pay, в планах больше) и по одному клику в HTML-панели **симулирует любой нужный тебе сбой** на любом endpoint-е банка.

## Что внутри

- **30+ готовых пресетов** банковских сбоев из реальных production-инцидентов
- **17 типов action-ов** для построения собственных сценариев (timeout, ambiguous_error, malformed_body, slow_body, wrong_payment_id, ...)
- **HTML-панель** с автообновлением — добавь сценарий одним кликом, посмотри request log
- **Полная поддержка Freedom Pay SDK**: HoldInit / Hold / Status / Capture / Cancel / Revoke / InitPayment / AddCard / RemoveCard / ApplePay / GooglePay + FreedomQR + Loyalty
- **Recovery-flows**: воспроизведи "деньги списались, а Capture упал" в один клик
- **MD5-подпись** один-в-один совместима с PG SDK
- **150+ тестов**, unit + интеграционные, прогоняются одной командой

## Быстрый старт

```bash
git clone https://github.com/<your-user>/chaospay
cd chaospay
make up                        # docker compose build && up -d
make health                    # → OK
open http://localhost:48532/panel
```

Направь свой Payment Gateway на ChaosPay:

```env
FREEDOM_PAY_HOST=http://chaospay:8532
FREEDOM_PAY_CUSTOMER_HOST=http://chaospay:8532
```

И тестовый Freedom-терминал в БД PG (`merchant_id` / `merchant_secret` должны совпадать с дефолтами):

```bash
./payment-gateway terminal freedom \
  --terminalID=1 \
  --merchantID=100001 \
  --merchantSecret=mock-secret-key
```

Триггерь оплату — смотри сценарий в [`/panel?tab=scenarios`](http://localhost:48532/panel?tab=scenarios), reproduce'ишь любой инцидент кнопкой.

## Категории пресетов

| Группа | Что внутри |
|---|---|
| 🚨 **Incidents** | EX-1001 ambiguous Hold → recovery, hold_pending_recovery, capture/cancel/revoke failed → status recovered, desync, hold_timeout |
| 🌐 **Retry and timeouts** | init_retry_exhausted, hold_init_retry_exhausted, wallet_retry_exhausted, context_deadline |
| 💸 **Business declines** | insufficient_funds (10009), card_declined (10007), expired_card (10017), 3ds_failed (10004), limit_exceeded (10006), card_data_input (10005), code_limit_exceeded (10003), emitter_error (10001), country_not_supported (10013), transaction_amount_zero (11016), unknown_bank_error (9992), default_bank_error (99999) |
| 💥 **Broken responses** | wallet_empty_response, wallet_malformed, init_malformed_xml, slow_body_capture |
| 🔀 **Data integrity** | wrong_payment_id, missing_signature, wrong_amount |

У каждого пресета в UI есть `❔` с примером ответа банка — раскрой и сразу увидишь, что увидит PG.

## Команды Makefile

```bash
make help              # список команд
make up                # docker compose build + up
make down              # остановить
make restart           # rebuild + restart
make logs              # docker logs -f
make health            # проверить /health
make test-unit         # юнит-тесты (go test ./...)
make test-integration  # интеграционные тесты против запущенного мока
make test-all          # юнит + интеграция (поднимет мок если нужно)
```

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
│   └── scenario/                  ← Add/Remove/Match/ApplyPreset + AllPresets
├── infrastructure/                ← реализации
│   ├── freedompay/                ← XML, MD5 signature
│   ├── memstore/                  ← in-memory PayRepo/QRRepo/ScenarioStore/RequestLog
│   ├── pgclient/                  ← webhook-клиенты на PG
│   └── qrgen/                     ← skip2/go-qrcode wrapper
└── ports/                         ← внешние интерфейсы
    ├── api/                       ← HTTP handlers
    │   ├── pay/                   ← Freedom Pay XML endpoints
    │   ├── wallet/                ← /pay/{id}/pay (Apple/Google Pay)
    │   ├── qr/                    ← /qr-code/* (FreedomQR)
    │   ├── loyalty/               ← /authservice + /loyaltyservice
    │   ├── scenarioapply/         ← общий transport-applier
    │   └── health/                ← /health
    └── panel/                     ← HTML-панель (Cards / QR / Scenarios / Log / Settings)

tests/integration/                 ← system-tests, отдельный Go-модуль
```

**Ключевая концепция — Scenario.** Правило вида «когда придёт запрос на endpoint X с paymentID=Y, ответь действием Z». Сценарии не модифицируют PaymentRecord — только ответ. Это даёт точечный контроль без правки кода.

## ENV-переменные

| Переменная | Default | Описание |
|---|---|---|
| `CHAOSPAY_LISTEN_ADDR` | `:8532` | Адрес HTTP-сервера |
| `CHAOSPAY_DELAY_SECONDS` | `0` | Глобальная задержка ответов |
| `CHAOSPAY_FREEDOM_MERCHANT_ID` | `100001` | merchant_id Freedom-терминала |
| `CHAOSPAY_FREEDOM_TERMINAL_ID` | `1` | terminal_id |
| `CHAOSPAY_FREEDOM_SECRET` | `mock-secret-key` | secret для MD5-подписи (кабинет по умолчанию) |
| `CHAOSPAY_FREEDOM_MERCHANTS` | пусто | Дополнительные кабинеты: `merchant_id:secret,merchant_id:secret` |
| `CHAOSPAY_FREEDOM_AUTO_WEBHOOK` | `false` | Авто-webhook на смену статуса |
| `CHAOSPAY_FREEDOM_HOSTED_URL` | `http://localhost:48532/panel?tab=cards` | redirect_url для PayPage |
| `PG_WEBHOOK_URL` | `http://...freedom-qr` | URL PG для QR webhook'а |
| `PG_FREEDOM_PAY_WEBHOOK_URL` | `http://...freedompay` | URL PG для card webhook'а |
| `PG_FREEDOM_PAY_CARD_WEBHOOK_URL` | `http://...freedompay/card` | URL PG для bind-webhook'а |

## Документация

- 🚀 [docs/setup.md](docs/setup.md) — пошаговый setup ChaosPay + PG
- 🧪 [docs/errors-playbook.md](docs/errors-playbook.md) — **воспроизведение ошибок банка для тестов**
- 📐 [docs/architecture.md](docs/architecture.md) — слои, DI, state-машины
- 🔌 [docs/api.md](docs/api.md) — все endpoints с примерами
- ✍️ [docs/signature.md](docs/signature.md) — алгоритм MD5 (1-в-1 с PG SDK)
- 🎬 [docs/scenarios.md](docs/scenarios.md) — каталог actions/params
- ⚡ [docs/ex1001.md](docs/ex1001.md) — пошаговое воспроизведение инцидента EX-1001
- 🤖 [CLAUDE.md](CLAUDE.md) — гайд для Claude Code / контрибьюторов

## Тестирование

```bash
make test-all
```

- **Юнит-тесты** (4 пакета): `freedompay` (signature, OrdMap, XML), `scenario` (Matches, params), `memstore` (race-safe store), `application/scenario` (все пресеты)
- **Интеграционные тесты** (79): транспорт-уровень wallet+XML, все бизнес-пресеты (wallet+XML), регистрация пресетов, recovery flows end-to-end, content-level XML modifications, GooglePay, AddCard/RemoveCard, Cancel/Revoke happy-path, QR-PAY full cycle, panel-actions, webhook

## Roadmap

- [x] Freedom Pay (XML + ApplePay + GooglePay)
- [x] FreedomQR
- [x] Freedom Loyalty (заглушка)
- [x] 30+ пресетов сбоев
- [x] HTML-панель управления
- [x] 150+ тестов
- [ ] Kaspi Pay (QR + status polling + recovery)
- [ ] IOKA / JetPay / EPAY / Flitt
- [ ] Persistence на диск (опционально, для тестовых стендов)
- [ ] Webhook URL override через query/header (для e2e fake-PG в одном процессе)
- [ ] CI workflow (GitHub Actions)

## Что НЕ реализовано (намеренно)

- 3DS Challenge flow (поля принимаем, не симулируем — PG SDK не реализует Challenge)
- Loyalty integration в Pay (поля принимаем, не процессим — отдельный домен)
- Receipt position validation, recurring payments
- Дедупликация `pg_idempotency_key` (намеренно — для EX-1001 нужно её отсутствие)

## Contributing

Issues и PR-ы приветствуются. Перед PR:

```bash
make install-tools   # один раз — golangci-lint + goimports
make lint            # без замечаний
make test-all        # 150+ тестов зелёные
```

Подробный гайд — [CONTRIBUTING.md](CONTRIBUTING.md). Архитектурные конвенции — [CLAUDE.md](CLAUDE.md).

## Лицензия

[MIT](LICENSE)
