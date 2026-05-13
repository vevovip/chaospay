# API

## Freedom Pay (XML)

Все XML-эндпоинты:

- Content-Type: `application/x-www-form-urlencoded`
- Тело: `pg_xml=<?xml version="1.0" encoding="utf-8"?><request>...</request>`
- Подпись `pg_sig` обязательна (см. [signature.md](signature.md))
- Ответ: XML с `pg_status=ok|error`, тоже подписан

| URL | scriptName | Метод | Описание |
|---|---|---|---|
| `POST /v1/merchant/{id}/card/init` | `init` | XML | HoldInit — создание платежа saved-card, возвращает `pg_payment_id` |
| `POST /v1/merchant/{id}/card/direct` | `direct` | XML | Hold — авторизация. Повторный вызов на принятом платеже → `Неверный статус платежа` (EX-1001) |
| `POST /get_status3.php` | `get_status3.php` | XML | Status — текущее состояние платежа |
| `POST /do_capture.php` | `do_capture.php` | XML | Capture — списание захолда |
| `POST /cancel.php` | `cancel.php` | XML | Cancel — отмена холда (до списания) |
| `POST /revoke.php` | `revoke.php` | XML | Revoke — возврат (full/partial по `pg_refund_amount`) |
| `POST /init_payment.php` | `init_payment.php` | XML | Payment / PayPage — выдача `pg_redirect_url` |
| `POST /v1/merchant/{id}/cardstorage/add2` | `add2` | XML | AddCard — выдача URL формы привязки карты |
| `POST /v1/merchant/{id}/cardstorage/remove` | `remove` | XML | RemoveCard |

### Пример HoldInit

```bash
# Подпись считается по правилам signature.md.
# Заголовок и pg_xml ниже — для иллюстрации формата.

curl -X POST http://localhost:48532/v1/merchant/100001/card/init \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode 'pg_xml=<?xml version="1.0" encoding="utf-8"?><request>
    <pg_amount>1500</pg_amount>
    <pg_card_token>tok-abc</pg_card_token>
    <pg_description>оплата заказа № 9999</pg_description>
    <pg_merchant_id>100001</pg_merchant_id>
    <pg_order_id>9999</pg_order_id>
    <pg_salt>test1234</pg_salt>
    <pg_user_id>1</pg_user_id>
    <pg_sig>...md5...</pg_sig>
  </request>'
```

Ответ:

```xml
<?xml version="1.0" encoding="utf-8"?><response>
  <pg_status>ok</pg_status>
  <pg_payment_id>1700000001</pg_payment_id>
  <pg_merchant_id>100001</pg_merchant_id>
  <pg_order_id>9999</pg_order_id>
  <pg_salt>...</pg_salt>
  <pg_sig>...</pg_sig>
</response>
```

### Пример инцидента EX-1001

После успешного Hold отправляем второй Hold на тот же payment_id:

```xml
<!-- Запрос -->
<request>
  <pg_merchant_id>100001</pg_merchant_id>
  <pg_payment_id>1700000001</pg_payment_id>
  <pg_salt>test1234</pg_salt>
  <pg_sig>...</pg_sig>
</request>

<!-- Ответ -->
<response>
  <pg_status>error</pg_status>
  <pg_error_code>120</pg_error_code>
  <pg_failure_description>Неверный статус платежа</pg_failure_description>
  <pg_salt>...</pg_salt>
  <pg_sig>...</pg_sig>
</response>
```

Эта же строка `Неверный статус платежа` зашита в PG в ``error_classifier.go`` как ambiguous-marker — `ReconcilingClient` идёт за `Status`.

## Freedom Pay Wallet (JSON / form)

| URL | Content-Type | Метод | Описание |
|---|---|---|---|
| `POST /pay/{paymentID}/pay` | `application/json` | JSON | ApplePay — submit ApplePay token, возвращает `back_url` |
| `POST /pay/{paymentID}/pay` | `application/x-www-form-urlencoded` | form | GooglePay — submit GooglePay token, возвращает `payment_info.payment_id` |

Один handler различает по `Content-Type`. PaymentRecord должен быть предварительно создан через `init_payment.php`.

### Ответ ApplePay

```json
{
  "data": {
    "status": "ok",
    "message": "",
    "back_url": {
      "url": "https://rahmetapp.kz?pg_payment_id=1700000002&pg_order_id=10000",
      "params": {"pg_order_id": 10000, "pg_payment_id": 1700000002}
    }
  }
}
```

### Ответ GooglePay

```json
{
  "data": {
    "status": "ok",
    "message": "",
    "payment_info": {"payment_id": 1700000002},
    "back_url": {"url": ""},
    "frame_url": ""
  }
}
```

## QR-PAY

| Метод | URL | Auth | Описание |
|---|---|---|---|
| POST | `/qr-code/generate` | Basic Auth (любой логин/пароль) | Генерация QR. `dataType=001` — оплата, `dataType=003` — refund |
| GET  | `/qr-code/get-status/{uuid}` | Basic Auth | Статус оплатного QR |
| POST | `/qr-code/change-status` | Basic Auth | Смена статуса (отмена) |
| GET  | `/qr-code/get-status-refund/{uuid}` | Basic Auth | Статус refund-QR. При SCANNED отдаёт `transactions[]` |
| POST | `/qr-code/confirm-refund` | Basic Auth | Подтверждение возврата |

### Generate

```bash
curl -u test:test -X POST http://localhost:48532/qr-code/generate \
  -H 'Content-Type: application/json' \
  -d '{
    "beneficiary": {"bin": "123", "tid": "T1", "mid": "M1"},
    "payment": {"amount": 1500, "dataType": "001", "deviceType": "USERAPP"}
  }'
```

Ответ: `{"uuid":"...", "qr":"<base64-png>"}`.

### Get Status

```bash
curl -u test:test http://localhost:48532/qr-code/get-status/<uuid>
# {"uuid":"...", "status":"NEW"} — пока не отсканировали
# При SUCCESS добавляются trnId, trnDate
```

### Confirm Refund

```bash
curl -u test:test -X POST http://localhost:48532/qr-code/confirm-refund \
  -H 'Content-Type: application/json' \
  -d '{
    "uuid": "<refund-uuid>",
    "amount": 1500,
    "reference": "ref-2315ee8c",
    "parentTrnId": "1776852940347"
  }'
```

Ошибки: HTTP 410 при повторном confirm или non-SCANNED состоянии (идемпотентность).

## Loyalty

| Метод | URL | Описание |
|---|---|---|
| POST | `/authservice/api/auth/v1/security/getToken` | Получение mock-токена |
| POST | `/loyaltyservice/loyalty/frhcCompanyTransaction` | Данные лояльности (всегда mock) |

## Webhook → PG

### Pay webhook

`POST {PG_FREEDOM_PAY_WEBHOOK_URL}` с form-encoded body. scriptName для подписи = последний сегмент URL = `freedompay`. См. ``request.go`` PG-стороны для полей.

### Card-bind webhook

`POST {PG_FREEDOM_PAY_CARD_WEBHOOK_URL}` с body `pg_xml=<?xml ?><response>...</response>`. scriptName для подписи = `card`.

### QR webhook

`POST {PG_WEBHOOK_URL}` с JSON-body `{"uuid","status","trnId","trnDate"}`. Подписи нет.

## Health

```bash
curl http://localhost:48532/health
# OK
```

## Panel

`GET /panel?tab=cards|qr|scenarios|log|settings`. Server-rendered HTML, мета-refresh 3s на cards/qr/log. Алиас `/qr-panel` → `/panel?tab=qr`.

POST-actions:
- `/panel/cards/action` — Force* и Add Test Payment
- `/panel/cards/webhook` — Send Webhook (manual)
- `/panel/cards/reset` — очистка всех платежей
- `/panel/scenarios/{add,delete,preset,reset}`
- `/panel/log/reset`
- `/qr-panel/{action,webhook}` — legacy совместимость
