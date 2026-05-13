# Подпись Freedom Pay

Алгоритм 1-в-1 повторяет SDK PG (``pkg/freedompay/freedompay.go::GetSignature``).

Реализация мока: [`internal/infrastructure/freedompay/signature.go`](../internal/infrastructure/freedompay/signature.go).

## Алгоритм

```
md5( scriptName + ";" + values_in_sorted_key_order + ";" + secretKey )
```

### Шаги

1. **Собрать поля** в map (ключ → значение). Не включать `pg_sig`.
2. **Отсортировать ключи** по алфавиту (`sort.Strings`).
3. **Извлечь значения** в том же порядке. Для скаляров — само значение. Для вложенных map (например `merchant_params`) — рекурсивная сортировка вложенных ключей и извлечение значений. Для `[]map` — обход в порядке элементов.
4. **Собрать строку**: `[scriptName, value_1, value_2, ..., secretKey]`. scriptName может быть пустой строкой — тогда не включается. secretKey — последний элемент.
5. **Соединить через `;`** и взять `md5` от результата (hex lower).

```
md5("init;1500;tok-abc;оплата заказа № 9999;100001;9999;test1234;1;mock-secret-key")
  = 0ef41acd5c1e830a5589826fda623f54
```

## scriptName для запросов

Берётся из последнего сегмента URL:

| Endpoint URL | scriptName |
|---|---|
| `/v1/merchant/{id}/card/init` | `init` |
| `/v1/merchant/{id}/card/direct` | `direct` |
| `/get_status3.php` | `get_status3.php` |
| `/do_capture.php` | `do_capture.php` |
| `/cancel.php` | `cancel.php` |
| `/revoke.php` | `revoke.php` |
| `/init_payment.php` | `init_payment.php` |
| `/v1/merchant/{id}/cardstorage/add2` | `add2` |
| `/v1/merchant/{id}/cardstorage/remove` | `remove` |

## scriptName для ответов (важная тонкость!)

**scriptName в ответе НЕ всегда совпадает со scriptName запроса.** Берётся из `*Response.GetScriptName()` каждой команды в PG SDK (``pkg/freedompay/commands/*/response.go``).

| Команда | scriptName ответа |
|---|---|
| HoldInit | `""` (пусто) |
| Hold | `""` (пусто) |
| Status | `get_status3.php` |
| Capture | `do_capture.php` |
| Cancel | `cancel.php` |
| Revoke | `revoke.php` |
| Payment / PayPage | `init_payment.php` |
| AddCard | `add2` |
| RemoveCard | `remove` |

Если ошибиться — PG отвергнет подпись в `VerifySignatureByResponse`. Мок настроен корректно: см. [`internal/ports/api/pay/handler.go::Register`](../internal/ports/api/pay/handler.go), второй параметр `xmlEndpoint(...)` это и есть scriptName ответа.

## scriptName для webhook'ов

| Webhook | scriptName |
|---|---|
| `/api/v1/payment-gateway/webhook/freedompay` | `freedompay` |
| `/api/v1/payment-gateway/webhook/freedompay/card` | `card` |

Это последний сегмент URL — PG так и парсит в ``verifier.go``.

## Соль

`pg_salt` — 8 случайных символов из `[a-zA-Z]` ([`GenerateSalt`](../internal/infrastructure/freedompay/signature.go)). Алфавит совпадает с PG SDK (``salt_generator.go``).

## Проверка входящей подписи

Мок проверяет `pg_sig` каждого XML-запроса в [`xmlEndpoint`](../internal/ports/api/pay/handler.go). При невалидной подписи возвращает валидно-подписанный XML с `pg_status=error`, `pg_error_code=2000`, `pg_error_description="invalid signature: got <hash>"`. Это удобно для отладки в Request Log.

## Ограничения текущей реализации

В PG SDK `getSignatureValue` для нестроковых нестандартных типов (float и т.п.) silently дропает значения. Мок это поведение **не повторяет** — приводит всё к строке через `fmt.Sprintf("%v", val)`. Для текущих контрактов это совместимо: PG парсит ответы через `xmlparser` который всегда возвращает строки, поэтому подпись со строкового представления float совпадает с обеих сторон.

Если в будущем добавятся числовые поля без строкового представления (целые float-ы, NaN и т.д.) — тут придётся синхронизировать поведение.
