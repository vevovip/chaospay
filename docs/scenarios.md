# Сценарии имитации

Сценарий — правило, по которому мок отвечает на следующий запрос, попадающий под matcher. Полезно для воспроизведения нештатных ответов банка (timeout, ambiguous error, invalid signature и т.д.) без модификации кода.

> 👉 Если тебе нужно **просто воспроизвести конкретную ошибку** (insufficient funds, retry-exhausted, ...) — открывай [errors-playbook.md](errors-playbook.md). Этот файл — справочник по action-ам.

Реализация: [`internal/domain/scenario`](../internal/domain/scenario), [`internal/application/scenario`](../internal/application/scenario), [`internal/infrastructure/memstore/scenario.go`](../internal/infrastructure/memstore/scenario.go). Применение: общий transport-applier [`internal/ports/api/scenarioapply/transport.go`](../internal/ports/api/scenarioapply/transport.go), XML-content modifier [`internal/ports/api/pay/scenario.go`](../internal/ports/api/pay/scenario.go), wallet (JSON) — [`internal/ports/api/wallet/handler.go`](../internal/ports/api/wallet/handler.go).

UI: вкладка [Scenarios](http://localhost:48532/panel?tab=scenarios) в панели управления.

## Структура сценария

```go
type Scenario struct {
    ID          string             // sc-N, генерируется автоматически
    Endpoint    string             // "*" или один из {init, direct, get_status3.php, ...}
    PaymentID   string             // "*" или конкретный
    OrderID     string             // "*" или конкретный
    MerchantID  string             // "*" или конкретный
    Action      Action             // что вернуть
    Params      map[string]string  // параметры action-а (см. ниже)
    ConsumeOnce bool               // удалить после первого срабатывания
    HitCount    int                // сколько раз сработал
}
```

`*` совпадает с любым значением. Порядок добавления = порядок матчинга — выигрывает первый совпавший.

## Доступные actions

### `timeout`

Имитирует пропадание ответа банка. Мок засыпает на N секунд, затем закрывает соединение.

| Параметр | Default | Описание |
|---|---|---|
| `seconds` | 20 | сколько спать (должно превышать клиентский таймаут PG, который 15s + retry) |

Поведение PG: `smarthttp.WithRetryRequest` ретранит несколько раз; в итоге транзакция уходит в pending.

### `ambiguous_error`

**Главный сценарий для воспроизведения EX-1001.** Возвращает XML с `pg_status=error` и заданной `pg_failure_description`. Состояние PaymentRecord в моке **не меняется**.

| Параметр | Default | Описание |
|---|---|---|
| `message` | `Неверный статус платежа` | `pg_failure_description` |
| `error_code` | `120` | `pg_error_code` |

Список ambiguous-маркеров в PG: ``error_classifier.go``.

### `force_status`

Применяется к **ответу `get_status3.php`** — подменяет `pg_payment_status` на заданное.

| Параметр | Default | Описание |
|---|---|---|
| `payment_status` | `success` | одно из `success` / `process` / `new` / `failed` / `revoked` |

### `delay`

Sleep N секунд, после чего нормальный handler-ответ.

| Параметр | Default | Описание |
|---|---|---|
| `seconds` | 5 | задержка |

### `http_error`

Возвращает HTTP-error без тела.

| Параметр | Default | Описание |
|---|---|---|
| `http_status` | `500` | код ответа |

### `invalid_signature`

Корректный XML, но `pg_sig` подменён на нули (`00000000000000000000000000000000`). PG отклонит.

### `partial_amount`

Подменяет `pg_amount`/`pg_clearing_amount` на заданное (для тестов сверки).

| Параметр | Default | Описание |
|---|---|---|
| `amount` | — | новое значение |

### `force_failure`

Произвольная ошибка платежа.

| Параметр | Default | Описание |
|---|---|---|
| `message` | `forced failure` | `pg_error_description` |
| `error_code` | `100` | `pg_error_code` |

## Transport-level actions

Применяются в общем [`scenarioapply.Transport`](../internal/ports/api/scenarioapply/transport.go) — работают и для XML endpoints, и для wallet (JSON), и для QR.

### `connection_reset`

Hijack + Close TCP-сокета без любого ответа. На стороне PG — connection-error → retry. Аналог `timeout` с `seconds=0`, но без задержки и без try-фолбэка на 504.

### `empty_response`

Отдаёт `200 OK` с пустым body. PG-парсер падает на `unexpected EOF` или возвращает нулевую структуру.

### `malformed_body`

Отдаёт `200 OK` с произвольным телом. Полезно для тестов парсеров.

| Параметр | Default | Описание |
|---|---|---|
| `body` | `<<<NOT_VALID_XML_OR_JSON>>>` | Тело ответа |
| `content_type` | `application/xml; charset=utf-8` | Заголовок Content-Type |

### `slow_body`

Headers пришли мгновенно, тело отдаётся побайтово с задержкой. Триггерит **Read-timeout** клиента (отличается от `timeout`, который ловит connect-timeout / awaiting headers).

| Параметр | Default | Описание |
|---|---|---|
| `body` | `<response><pg_status>ok</pg_status></response>` | Тело ответа |
| `chunk_delay_ms` | 500 | Задержка между байтами в мс |

### `wrong_status_code`

Возвращает заданный HTTP-код без `http.Error` (т.е. без стандартного текста ошибки и без body). Чистый отправ статуса.

| Параметр | Default | Описание |
|---|---|---|
| `http_status` | 418 | HTTP-код |

## Content-level actions (XML)

Применяются после нормального handler-выполнения, модифицируют поля валидного XML-ответа.

### `wrong_payment_id`

Подменяет `pg_payment_id` в ответе.

| Параметр | Default | Описание |
|---|---|---|
| `payment_id` | `9999999999` | Новое значение |

### `wrong_amount`

Подменяет `pg_amount` и `pg_clearing_amount`.

| Параметр | Default | Описание |
|---|---|---|
| `amount` | — | Новое значение |

### `missing_field`

Удаляет поле из ответа.

| Параметр | Default | Описание |
|---|---|---|
| `field` | — | Имя удаляемого поля (например, `pg_sig`) |

### `extra_garbage`

Добавляет мусорные поля в ответ.

| Параметр | Default | Описание |
|---|---|---|
| `count` | 5 | Сколько полей `pg_garbage_N` добавить |

## Возвраты

Freedom заводит на каждый `revoke.php` отдельный refund-платёж со своим `pg_payment_id`
и суммой минусом. Он попадает в `pg_refund_payments` статуса исходной оплаты, а в агрегат
`pg_refund_amount` — только если прошёл успешно. Мок повторяет это поведение, включая то,
что поиск статуса по `pg_order_id` после возврата отдаёт сам refund-платёж
(`pg_amount` отрицательный, `pg_clearing_amount=0`).

### `refund_declined`

Матчить на `revoke.php` (по `payment_id` — `pg_order_id` в этот запрос PG не кладёт).
Ответ на revoke — `ok`, но refund-платёж заводится со статусом `error` и денег не возвращает.

### `refund_pending`

То же, но refund-платёж остаётся в статусе `process` — возврат принят и ещё не завершён.

### `refund_invisible`

Матчить на `get_status3.php`. Отдаёт статус так, будто возврата ещё нет: без
`pg_refund_payments` и без возвращённых сумм. Воспроизводит задержку, с которой Freedom
отражает возврат в статусе исходного платежа. С `consume_once=true` скрывает один ответ —
удобно проверять, что вызывающая сторона доживает до подтверждения ретраем.

## Pre-set'ы (кнопки в панели)

Список всех пресетов с описанием их назначения — в [errors-playbook.md](errors-playbook.md). Программный список — переменная `AllPresets` в [`internal/application/scenario/service.go`](../internal/application/scenario/service.go).

Категории:
- **Сценарные:** `ex1001`, `desync`, `hold_timeout`
- **Retry-exhausted:** `init_retry_exhausted`, `hold_init_retry_exhausted`, `wallet_retry_exhausted`, `context_deadline`
- **Бизнес-ошибки Freedom:** `insufficient_funds`, `card_declined`, `fraud_suspected`, `expired_card`, `3ds_failed`, `limit_exceeded`
- **Битые ответы:** `wallet_empty_response`, `wallet_malformed`, `init_malformed_xml`, `slow_body_capture`
- **Data integrity:** `wrong_payment_id`, `missing_signature`, `wrong_amount`

Reset всех активных сценариев — кнопка `Reset all` или `POST /panel/scenarios/reset`.

## API программно

```go
sc := &scenario.Scenario{
    Endpoint:    "direct",
    PaymentID:   scenario.Wildcard,
    OrderID:     "9999",          // только для конкретного заказа
    MerchantID:  scenario.Wildcard,
    Action:      scenario.ActionAmbiguousError,
    Params:      map[string]string{"message": "Неверный статус платежа"},
    ConsumeOnce: true,
}
scenarios.Add(sc)
```

## Где применяются сценарии

В каждом handler-е первой строкой после декода и проверки подписи:

```go
sc := scenarios.Match(scenario.MatchInput{
    Endpoint:   "direct",
    PaymentID:  req.Get("pg_payment_id", ""),
    OrderID:    req.Get("pg_order_id", ""),
    MerchantID: req.Get("pg_merchant_id", ""),
})
if sc != nil && c.applyScenarioBefore(w, sc, ...) {
    return // ответ уже отправлен
}
```

Применение делится на три фазы:

1. **Transport-level** (общий [`scenarioapply.Transport`](../internal/ports/api/scenarioapply/transport.go)) — вызывается первым во всех handler-ах. Обрабатывает: `timeout`, `connection_reset`, `http_error`, `wrong_status_code`, `empty_response`, `malformed_body`, `slow_body`. Если применился — handler выходит, ответ уже отправлен.
2. **applyScenarioBefore** ([`pay/scenario.go`](../internal/ports/api/pay/scenario.go)) — XML-specific варианты бизнес-ошибок: `delay`, `ambiguous_error`, `force_failure`.
3. **applyScenarioAfter** — модификации валидного ответа: `force_status`, `partial_amount`, `wrong_payment_id`, `wrong_amount`, `missing_field`, `extra_garbage`. `invalid_signature` применяется отдельно в render-фазе.
