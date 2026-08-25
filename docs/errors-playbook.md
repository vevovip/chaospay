# Errors Playbook — воспроизведение ошибок банка

Руководство для команд, тестирующих `payment-gateway` (PG) против `chaospay`. Каждый раздел — конкретная ошибка PG, как её воспроизвести в моке, как проверить, что PG её увидел.

## Содержание

- [Как применять пресеты](#как-применять-пресеты)
- [Транспортные ошибки (retry-exhausted, timeout)](#транспортные-ошибки)
- [Бизнес-ошибки банка (insufficient_funds, declined, ...)](#бизнес-ошибки-банка)
- [Битые ответы (empty, malformed, slow)](#битые-ответы)
- [Data integrity (wrong payment_id, missing signature, ...)](#data-integrity)
- [Сценарные кейсы (EX-1001, desync)](#сценарные-кейсы)
- [Кастомный сценарий через UI](#кастомный-сценарий-через-ui)
- [Кастомный сценарий через curl](#кастомный-сценарий-через-curl)
- [Verify-чеклист и troubleshooting](#verify-чеклист)

---

## Как применять пресеты

**UI:** открыть [http://localhost:48532/panel?tab=scenarios](http://localhost:48532/panel?tab=scenarios) → нажать кнопку нужного preset → триггерить обычный платёжный flow в PG.

**Curl** (для CI/автотестов):
```bash
# Применить preset
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=<имя_пресета>"

# Очистить все сценарии
curl -X POST http://localhost:48532/panel/scenarios/reset
```

После применения — сценарий лежит в очереди матчинга. На следующий запрос от PG, попадающий под matcher, мок ответит заданным действием. Если `ConsumeOnce=true` (по умолчанию для бизнес-ошибок) — сработает один раз и удалится. Для retry-exhausted `ConsumeOnce=false` — матчится на каждую retry-попытку PG.

---

## Транспортные ошибки

PG использует [hashicorp/go-retryablehttp](https://github.com/hashicorp/go-retryablehttp), который ретранит 3 раза при connection-level ошибках или 5xx. После 3 неудач → `giving up after 3 attempt(s)`.

### `init_retry_exhausted` — retry-exhausted на init_payment.php

**Что увидит PG:**
```
: Post "https://api.freedompay.kz/init_payment.php": POST https://api.freedompay.kz/init_payment.php
giving up after 3 attempt(s): Post "...": net/http: request canceled while waiting for connection
(Client.Timeout exceeded while awaiting headers)
```

**Применение:**
```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=init_retry_exhausted"
```

**Что внутри:** один сценарий `Endpoint=init_payment.php`, `Action=timeout`, `seconds=15`, `ConsumeOnce=false`. PG-клиент Freedom настроен на `Client.Timeout` около 10s — мок засыпает на 15s и обрывает соединение → каждая retry-попытка падает по таймауту.

**Когда использовать:** тест fallback-поведения PG при недоступности банка на init-фазе. Платёж должен уйти в `failed` или `pending` в зависимости от логики.

### `hold_init_retry_exhausted` — retry-exhausted на /v1/merchant/{id}/card/init

**Что увидит PG:**
```
: не удалось сделать инициализацию платежа: Post "https://api.freedompay.kz/v1/merchant/554415/card/init":
POST https://api.freedompay.kz/v1/merchant/554415/card/init giving up after 3 attempt(s)
```

**Применение:**
```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=hold_init_retry_exhausted"
```

### `wallet_retry_exhausted` — retry-exhausted на ApplePay/GooglePay

**Что увидит PG:**
```
: Post "https://customer.freedompay.kz/pay/019e0186-f16b-739f-8ccb-33fff17781ad/pay":
POST https://customer.freedompay.kz/pay/.../pay giving up after 3 attempt(s)
```

**Применение:**
```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=wallet_retry_exhausted"
```

**Что внутри:** два сценария на `applepay` и `googlepay` с timeout=15s.

### `context_deadline` — context cancellation

**Что увидит PG:**
```
giving up after 3 attempt(s): Post "...": context deadline exceeded
```

**Применение:**
```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=context_deadline"
```

**Что внутри:** wildcard сценарий на любой endpoint, timeout=60s — гарантированно больше любого `context.WithTimeout` в PG.

---

## Бизнес-ошибки банка

Banking-уровень: банк ответил **валидным** XML/JSON, но с `pg_status=error` и `pg_error_code=N`. PG читает `pg_error_code` (int), маппит через ``getError(errCode)`` в `common.Err*`, и пользователь видит русский текст из ``internal/domain/common/error.go``.

**Важно:** коды соответствуют **реальным** Freedom Pay кодам из прод-выдачи и PG-маппинга — один-в-один с `error_mapping.go`. Каждый код триггерит **конкретную** domain-ошибку PG.

| Preset | Freedom code | PG domain error | UI-текст пользователю |
|---|---|---|---|
| `insufficient_funds` | 10009 | `ErrNotEnoughMoney` | "недостаточно средств на карте" |
| `card_declined` | 10007 | `ErrDeclinedByIssuer` | "оплата отклонена банком" |
| `card_data_input` | 10005 | `ErrCardDataInput` | "введены неверные данные" |
| `expired_card` | 10017 | `ErrCardExpired` | "срок действия карты истек" |
| `3ds_failed` | 10004 | `Err3DSFail` | "3DS проверка не пройдена" |
| `limit_exceeded` | 10006 | `ErrCardLimitationsExceeded` | "превышен лимит по карте" |
| `code_limit_exceeded` | 10003 | `ErrCodeLimit` | "превышен лимит попыток ввода кода" |
| `emitter_error` | 10001 | `ErrEmitter` | "ошибка на стороне эмитента" |
| `country_not_supported` | 10013 | `ErrCountryNotSupported` | "карта данной страны не разрешена" |
| `transaction_amount_zero` | 11016 | `ErrTransactionAmountIsZero` | "сумма транзакции равна нулю" |
| `unknown_bank_error` | 9992 | `ErrUnknown` | "не ожидаемая ошибка, требуется помощь поддержки" |
| `default_bank_error` | 99999 (не в маппинге) | `ErrDefault` | "не ожидаемая ошибка, обратитесь в банк" |

**Альтернативные коды** в этих же категориях (если хочешь варьировать) — см. `error_mapping.go`. Например, `ErrNotEnoughMoney` триггерится также кодами `11006, 8888, 100091, 100094`; `ErrDeclinedByIssuer` — кодами `10010, 11011, 11012, 10038, 10039, ...` и т.д. Любой код из этой группы даст одинаковую domain-ошибку.

**Применение** (на примере insufficient_funds):
```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=insufficient_funds"
# Триггерим оплату в rahmet-app/postman
# В PG логах: pg_error_code=10009 → ErrNotEnoughMoney → клиент видит "недостаточно средств на карте"
```

**Что внутри:** wildcard сценарий `Action=force_failure` с конкретным `error_code` и `message`. Применяется к любому endpoint первой попавшейся попыткой.

**Кастомизация:** хочешь свой код из той же категории (например, не 10009 а 8888 для insufficient_funds) — открой [internal/application/scenario/service.go](../internal/application/scenario/service.go), скопируй case, поменяй параметры, перезапусти контейнер. Или используй **custom scenario** через UI/curl с любым кодом.

---

## Битые ответы

Тесты парсеров на стороне PG: как ведёт себя клиент при некорректном HTTP-ответе.

### `wallet_empty_response` — 200 OK, пустое тело

**Что увидит PG:** клиент получит `200 OK` без body. JSON-decoder упадёт с `unexpected EOF` или вернёт нулевую структуру.

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=wallet_empty_response"
```

### `wallet_malformed` — битый JSON

**Что увидит PG:** `200 OK` + `Content-Type: application/json` + body `{"data":{` (обрезанный JSON). Парсер вернёт `unexpected end of JSON input` или подобное.

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=wallet_malformed"
```

### `init_malformed_xml` — мусор вместо XML

**Что увидит PG:** `200 OK` + `Content-Type: application/xml` + body `<<<NOT_XML>>>`. XML-парсер PG упадёт.

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=init_malformed_xml"
```

### `slow_body_capture` — body отдаётся побайтово

**Что увидит PG:** headers пришли мгновенно (`200 OK`), но тело отдаётся 1 байт в секунду. Триггерит `Client.Timeout` на чтении ответа (НЕ на connect, как в retry_exhausted). Другой класс ошибок в PG-логах.

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=slow_body_capture"
```

**Применяется к:** `do_capture.php` (можно поменять в [service.go](../internal/application/scenario/service.go) на любой XML endpoint).

---

## Data integrity

Банк отвечает валидной структурой, но с искажёнными бизнес-данными. Тестирует валидацию на стороне PG.

### `wrong_payment_id` — Hold вернул чужой pg_payment_id

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=wrong_payment_id"
```

**Что внутри:** Hold-ответ корректен по структуре, но `pg_payment_id=9999999999` (не тот, что отправлял PG). PG должен либо отклонить ответ, либо распознать рассинхрон.

### `missing_signature` — ответ без pg_sig

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=missing_signature"
```

**Что внутри:** на `direct` мок вернёт XML без поля `pg_sig`. PG должен отклонить из-за невалидной подписи.

### `wrong_amount` — Status вернул другую сумму

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=wrong_amount"
```

**Что внутри:** на `get_status3.php` `pg_amount=1` (вместо реальной суммы). Тест сверки.

---

## Сценарные кейсы

### `ex1001` — Reproduce EX-1001 (ambiguous Hold → recovery)

Полный сценарий восстановления через ReconcilingClient. Подробности — в [ex1001.md](ex1001.md).

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=ex1001"
```

**Что внутри:** два consume-once сценария:
1. `direct` → `ambiguous_error` (PG считает результат неопределённым)
2. `get_status3.php` → `force_status: success` (Reconciler видит, что банк всё-таки списал → лечит транзакцию)

PG-лог при успехе: `reconciliation: recovered payment from false fail`.

### `desync` — рассинхрон без recovery

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=desync"
```

**Что внутри:** на `direct` мок отвечает fatal ошибкой (не ambiguous). PG-транзакция уходит в `failed`. После этого ВРУЧНУЮ через UI (`Force Captured` в Card Payments) ставишь статус на стороне мока — получаешь `PG=failed`, `Bank=CAPTURED`. Тест detect-логики десинхронизации.

### `hold_timeout` — таймаут на Hold

```bash
curl -X POST http://localhost:48532/panel/scenarios/preset -d "preset=hold_timeout"
```

20-секундный таймаут на `direct`. Тест pending-fallback в PG.

---

## Кастомный сценарий через UI

Если ни один preset не подходит — собери сценарий вручную:

1. Открой [http://localhost:48532/panel?tab=scenarios](http://localhost:48532/panel?tab=scenarios).
2. Заполни форму:
   - **Endpoint:** на какой endpoint матчить (`*` — любой, или конкретный из dropdown).
   - **PaymentID / OrderID / MerchantID:** matcher (`*` — любой).
   - **Action:** что вернуть (см. каталог в [scenarios.md](scenarios.md)).
   - **Параметры action-а:** заполни только нужные поля.
   - **ConsumeOnce:** `true` — сработает один раз, `false` — каждый матч.
3. **Add scenario** → готово.

---

## Кастомный сценарий через curl

```bash
curl -X POST http://localhost:48532/panel/scenarios/add \
  -d "endpoint=direct" \
  -d "payment_id=*" \
  -d "order_id=*" \
  -d "merchant_id=*" \
  -d "action=force_failure" \
  -d "error_code=99999" \
  -d "message=мой кастомный ответ банка" \
  -d "consume_once=true"
```

**Полный список параметров формы:**

| Параметр | Применим к action-ам | Описание |
|---|---|---|
| `seconds` | timeout, delay | задержка в секундах |
| `http_status` | http_error, wrong_status_code | HTTP-код ответа |
| `error_code` | force_failure, ambiguous_error | pg_error_code |
| `message` | force_failure, ambiguous_error | pg_error_description |
| `payment_status` | force_status | success/failed/process/new/revoked |
| `amount` | partial_amount, wrong_amount | новое значение pg_amount |
| `field` | missing_field | имя удаляемого поля |
| `body` | malformed_body, slow_body | произвольное тело ответа |
| `content_type` | malformed_body | Content-Type заголовок |
| `chunk_delay_ms` | slow_body | мс между байтами тела |
| `count` | extra_garbage | сколько мусорных полей добавить |
| `payment_id_param` | wrong_payment_id | новое значение pg_payment_id |

**Endpoints, по которым можно матчить:** `init`, `direct`, `get_status3.php`, `do_capture.php`, `cancel.php`, `revoke.php`, `init_payment.php`, `add2`, `remove`, `applepay`, `googlepay`, или `*` (любой).

Для Halyk Epay: `epay_token`, `epay_cryptopay`, `epay_card_auth`, `epay_confirm`, `epay_charge`, `epay_cancel`, `epay_refund`, `epay_status`.

## Проверка 3DS у Halyk Epay

Оплата кошельком через Epay идёт одним запросом `POST /api/payment/cryptopay` с `paymentType` = `applePay` либо `googlePay`. Карта из аккаунта Google (`PAN_ONLY`) приходит без криптограммы, поэтому банк возвращает блок `secure3D` — и платёж требует проверки у эмитента.

Пресеты:

| Пресет | Что имитирует | Ожидаемое поведение PG |
|---|---|---|
| `epay_3ds_required` | `cryptopay` возвращает `secure3D` | заказ в `action_required`, пользователь уходит на страницу 3DS |
| `epay_3ds_confirm_declined` | `confirm` отвечает отказом | заказ в неуспешные, состояние операции `FAILED` |
| `epay_3ds_confirm_timeout` | `confirm` не отвечает | итог берётся из состояния операции, заказ не остаётся подвешенным |

Подтверждение приходит на `POST /api/payment/confirm` с полями `ID`, `PaRes`, `MD`. Real Halyk отвечает на него редиректом, поэтому мок отдаёт 200 и переводит операцию, а исход платежа PG уточняет запросом состояния операции.

---

## Verify-чеклист

После того, как preset применён и оплата триггернута:

1. **Открой Request Log** в моке: [http://localhost:48532/panel?tab=log](http://localhost:48532/panel?tab=log). Видны все запросы PG к моку, какой сценарий применился, и итоговое тело ответа.
2. **Логи PG:**
   ```bash
   docker logs payment-gateway-backend 2>&1 | tail -50
   docker logs payment-gateway-backend 2>&1 | grep -E "freedom|webhook|error|retry"
   ```
3. **Статус заказа в PG:** через API PG (`GET /payment-gateway/v1/order/{id}/retrieve`) или напрямую в БД.

## Troubleshooting

| Симптом | Причина | Решение |
|---|---|---|
| PG не дёргает мок вообще | Не настроен FREEDOM_PAY_HOST | Проверь `docker exec payment-gateway-backend env \| grep FREEDOM_PAY_HOST` — должен указывать на `http://chaospay:8532` |
| Подпись не сходится | Расхождение секретов | `CHAOSPAY_FREEDOM_SECRET` в моке == `merchantSecret` тестового терминала в БД PG (по умолчанию `mock-secret-key`) |
| Сценарий не срабатывает | Не тот endpoint matcher | Открой `?tab=log`, посмотри `Endpoint` поля у входящего запроса — оно должно совпадать с matcher-ом сценария |
| Сценарий уже сработал и удалился | `ConsumeOnce=true` + повторный запрос | Применяй preset заново перед каждым тестом, или ставь `ConsumeOnce=false` для retry-проверок |
| Контейнер не видит мок | Не в одной сети | Оба контейнера должны быть в `dockernet-local` |

См. также: [setup.md](setup.md), [scenarios.md](scenarios.md), [ex1001.md](ex1001.md).
