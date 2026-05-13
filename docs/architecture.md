# Архитектура

## Слои (hexagonal / ports & adapters)

```
cmd/chaospay/main.go              ← bootstrap, DI через конструкторы

internal/
├── domain/                        ← типы и константы (без зависимостей)
│   ├── pay/                       ← Record, Status, Kind, AllowedTransitions
│   ├── qr/                        ← Code, Status, RefundTransaction, TTL
│   ├── scenario/                  ← Scenario, Action, MatchInput, Wildcard
│   └── requestlog/                ← Entry
│
├── application/                   ← оркестрация (services), зависит только от domain + интерфейсов
│   ├── pay/                       ← Service: HoldInit, Hold, Status, Capture, Cancel, Revoke, AuthorizeWallet, ApplyForce
│   ├── qr/                        ← Service: Generate, GetStatus, ChangeStatus, ConfirmRefund, ListRefundTransactions
│   └── scenario/                  ← Service: Add, Remove, Match, ApplyPreset
│
├── infrastructure/                ← внешние реализации (in-memory, HTTP клиенты, third-party)
│   ├── freedompay/                ← MD5 signature + pg_xml parser/builder + salt generator
│   ├── memstore/                  ← in-memory репозитории Pay/QR/Scenario/RequestLog
│   ├── pgclient/                  ← HTTP клиенты webhook'ов на PG (pay/card/qr)
│   └── qrgen/                     ← skip2/go-qrcode wrapper
│
├── config/                        ← Config struct + Load() из ENV
│
└── ports/                         ← внешние интерфейсы
    ├── api/                       ← HTTP handlers
    │   ├── pay/                   ← XML-эндпоинты Freedom Pay (init, direct, status, capture, ...)
    │   ├── wallet/                ← /pay/{id}/pay (ApplePay JSON, GooglePay form)
    │   ├── qr/                    ← /qr-code/* (Single QR, JSON)
    │   ├── loyalty/               ← /authservice + /loyaltyservice
    │   └── health/                ← /health
    └── panel/                     ← HTML-панель управления (5 вкладок)
```

## Зависимости между слоями

```
                ┌──────────────────────────┐
                │ cmd/chaospay (main.go)   │
                └────────┬─────────────────┘
                         │ собирает граф
                         ▼
        ┌────────────────────────────────────────┐
        │ ports/api/* + ports/panel              │
        │ (HTTP handlers / HTML)                 │
        └─────────┬──────────────────────────────┘
                  │ зовёт services
                  ▼
        ┌────────────────────────────────────────┐
        │ application/* (Service)                │
        │ зависит от: domain + Repository iface  │
        └─────────┬──────────────────────────────┘
                  │ ↓ интерфейсы
        ┌─────────┴──────────────────────────────┐
        │ infrastructure/*                       │
        │ memstore (реализует Repository),       │
        │ pgclient (реализует Webhook),          │
        │ freedompay (signature, xml),           │
        │ qrgen (генератор PNG)                  │
        └─────────┬──────────────────────────────┘
                  │
                  ▼
        ┌────────────────────────────────────────┐
        │ domain/* (types, no deps)              │
        └────────────────────────────────────────┘
```

Никаких глобальных переменных — все зависимости передаются через конструкторы (DI). Это даёт:
- Возможность подменить любой компонент в тестах (например, Repository → fake).
- Изолированные слои: domain не знает про HTTP, application не знает про XML/MD5, ports не лезет в memstore напрямую.
- Один процесс — один граф зависимостей, никакой неявной инициализации.

## Контракты application-сервисов

`application/pay/Service` принимает интерфейсы:

```go
type Repository interface {
    NextPaymentID() uint
    Create(rec *pay.Record)
    Get(paymentID uint) (*pay.Record, error)
    List() []*pay.Record
    Update(paymentID uint, fn func(rec *pay.Record) (pay.Status, string, error)) (*pay.Record, error)
    Transition(paymentID uint, allowedFrom map[pay.Status]bool, to pay.Status, reason string) (*pay.Record, error)
    MarkWebhookSent(paymentID uint)
    Reset()
}

type Webhook     interface { Send(rec *pay.Record, success, captured bool) (int, error) }
type CardWebhook interface { Send(rec *pay.Record) (int, error) }
```

`application/qr/Service`:

```go
type Repository  interface { Create, Get, UpdateStatus, MarkWebhookSent, List, ListSuccessfulPayments, ApplyRefundConfirmation }
type Generator   interface { PaymentURL(uuid string) string; Generate(content string) (string, error) }
type UUIDFactory func() string
type Webhook     interface { Send(code *qr.Code) (int, error) }
```

`application/scenario/Service`:

```go
type Store interface {
    Add(sc *scenario.Scenario)
    Remove(id string)
    Reset()
    List() []*scenario.Scenario
    Match(in scenario.MatchInput) *scenario.Scenario
}
```

## State-машина PaymentRecord

```
                        ┌──── force_failed ─────┐
                        │                       ▼
   New ─Hold─▶ HoldPending ─Hold ok─▶ Authorized ─Capture─▶ Captured
                                          │  │                  │
                                          │  └─Cancel─▶ Cancelled
                                          │                     │
                                          └─Revoke──┐  Revoke (full)─▶ Refunded
                                                    │                      ▲
                                                    └─Revoke (partial)──▶ PartialRefunded
                                                                              │
                                                                              └─Revoke (full)─▶ Refunded
```

Допустимые переходы кодифицированы в `domain/pay/payment.go::AllowedTransitions(target)`. Force-action в панели обходит проверку (для отладки).

## State-машина QR

```
   New ─scan─▶ Scanned ─confirm─▶ Success
    │            │
    │            └──TTL/cancel──▶ Expired/Cancelled/Error (все терминальные)
    │
    └──TTL──▶ Expired
```

Авто-EXPIRED через 5 минут (горутина в `application/qr/Service::autoExpire`).

## Имитация EX-1001

Повторный `Hold` на платёж в `Authorized` → `application/pay/Service.Hold` возвращает `ErrAlreadyAuthorized`. Handler в `ports/api/pay/handlers.go::handleHold` транслирует это в XML-ответ с `pg_failure_description="Неверный статус платежа"` — ровно та строка, которую `error_classifier.go` в PG матчит как ambiguous-error. См. [ex1001.md](ex1001.md).

## Не реализовано

См. [README.md](../README.md#что-не-реализовано).
