# Setup: как начать использовать мок

Пошаговая инструкция, как поднять мок и направить локальный PG на него.

## 0. Что нужно знать перед началом

| Объект | Где |
|---|---|
| Мок-сервис | `go-local/chaospay.local/` |
| Payment Gateway (PG) | `<your-payment-gateway-project>/` |
| Общая docker-сеть | `dockernet-local` (external) — мок и PG в ней друг друга видят по имени контейнера |
| Порт мока (на хосте) | `48532` (внутри контейнера 8532) |
| Имя контейнера мока | `chaospay` (PG будет ходить на `http://chaospay:8532`) |

## 1. Поднять мок

```bash
cd go-local/chaospay.local

# Если уже стоял старый flat-вариант — пересобрать без кэша
docker compose down
docker compose build --no-cache
docker compose up -d

# Проверка
curl http://localhost:48532/health   # → OK
open http://localhost:48532/panel
```

Убедись, что в логах есть строка `[mock] listening on :8532`:

```bash
docker logs chaospay | tail -5
```

## 2. Применить правки в PG (один раз)

Чтобы PG ходил на мок, мы добавили оверрайд хостов через ENV. Все изменения **уже в коде** на твоей ветке — пересборка PG их подхватит. Если ты пересоздаёшь чистый репо PG, проверь, что эти правки на месте:

### 2.1 `pkg/freedompay/freedompay.go`

Должно быть поле `customerHost` в `Service` + опция `WithCustomerHost`. Метод `ApplePay`/`GooglePay` использует `f.customerHost`, не литерал.

### 2.2 `internal/infrastructure/providers/freedompay/initialize.go`

В функции `options(l)` — чтение `FREEDOM_PAY_HOST` / `FREEDOM_PAY_CUSTOMER_HOST` из ENV:

```go
if h := envy.Get("FREEDOM_PAY_HOST", ""); h != "" {
    opts = append(opts, freedompay.WithHost(h))
}
if h := envy.Get("FREEDOM_PAY_CUSTOMER_HOST", ""); h != "" {
    opts = append(opts, freedompay.WithCustomerHost(h))
}
```

## 3. Прописать ENV в локальный PG

Добавь в `.env` файл локального PG (находится в `<your-payment-gateway-project>/.env`):

```env
# --- chaospay ---
FREEDOM_PAY_HOST=http://chaospay:8532
FREEDOM_PAY_CUSTOMER_HOST=http://chaospay:8532
```

> Эти переменные имеют дефолт пустую строку — в проде их **нельзя** задавать, иначе PG пойдёт на мок вместо реального Freedom.

`.env.example` уже содержит их с пустым дефолтом — можно скопировать оттуда.

## 4. Создать тестовый Freedom-терминал в БД PG

Подпись считается по `merchantID` + `secretKey` для конкретного `terminal_id`. Эти значения должны **совпадать на стороне мока и PG**.

### Дефолтные значения мока

| ENV в моке | Default |
|---|---|
| `CHAOSPAY_FREEDOM_MERCHANT_ID` | `100001` |
| `CHAOSPAY_FREEDOM_SECRET` | `mock-secret-key` |
| `CHAOSPAY_FREEDOM_TERMINAL_ID` | `1` |

### Создать терминал в БД через CLI PG

Запусти **внутри** PG-контейнера:

```bash
docker exec -it payment-gateway-backend \
  ./payment-gateway terminal freedom \
    --terminalID=1 \
    --merchantID=100001 \
    --merchantSecret=mock-secret-key
```

Если CLI выдаст ошибку «terminal not found» — сначала создай базовый терминал:

```bash
docker exec -it payment-gateway-backend \
  ./payment-gateway terminal new \
    --name='freedom-mock' \
    --description='chaospay for local testing'
# вернёт terminal_id (например, 42)
# затем привязать к freedom:
docker exec -it payment-gateway-backend \
  ./payment-gateway terminal freedom \
    --terminalID=42 \
    --merchantID=100001 \
    --merchantSecret=mock-secret-key
# и обнови CHAOSPAY_FREEDOM_TERMINAL_ID=42 в docker-compose мока
```

> Если у вас уже есть тестовый Freedom-terminal — можно просто переписать его `merchantID`/`secret`, и поставить эти же значения в ENV мока.

## 5. Перезапустить PG

После правки `.env` обязательно перезапустить PG-контейнер, чтобы он подхватил новые переменные:

```bash
docker restart payment-gateway-backend
```

Если PG-контейнер собирается из исходников и ты делал правки в `.go` — пересобрать:

```bash
cd your-payment-gateway-project
docker compose build payment-gateway-go-backend
docker compose up -d
```

## 6. Проверить связь

### 6.1 Из контейнера PG достучаться до мока

```bash
docker exec payment-gateway-backend \
  wget -qO- http://chaospay:8532/health
# → OK
```

Если `wget` не работает — попробуй `curl` или просто потесть с хоста:

```bash
curl http://localhost:48532/health
```

### 6.2 Триггернуть оплату из PG

В rahmet-app или через postman дёрни Authorize по saved-card Freedom (terminal_id из шага 4). В моке должны появиться записи:

- На вкладке [Card Payments](http://localhost:48532/panel?tab=cards) — новый PaymentRecord (NEW → Authorized)
- На вкладке [Request Log](http://localhost:48532/panel?tab=log) — `init` и `direct` с `Sig: ✓`

### 6.3 Триггернуть webhook вручную

В Card Payments → нажми `Send Webhook` на Authorized-записи → форма (result=ok) → submit. В логах PG ожидать 200 от `/api/v1/payment-gateway/webhook/freedompay`:

```bash
docker logs payment-gateway-backend 2>&1 | grep webhook
```

В Tab Request Log запись webhook не появится — этот лог только для **входящих** запросов на мок. Но в логах самого мока ты увидишь:

```
[WEBHOOK pay] sent payment 1700000001 → HTTP 200
```

## 7. Воспроизвести EX-1001

См. [ex1001.md](ex1001.md). Кратко:

1. [Scenarios](http://localhost:48532/panel?tab=scenarios) → ⚡ Reproduce EX-1001
2. Триггернуть оплату saved-card в PG
3. `docker logs payment-gateway-backend | grep "recovered payment from false fail"`

## Troubleshooting

### Панель не открывается на 48532

```bash
# Контейнер запущен?
docker ps --filter "name=chaospay"

# Если контейнер собран из старого flat-кода — пересобрать
docker compose -f go-local/chaospay.local/docker-compose.yml build --no-cache
docker compose -f go-local/chaospay.local/docker-compose.yml up -d
```

### PG отвечает «invalid signature» на ответы мока

Значит подписи не сошлись. Проверки:

| Что | Где |
|---|---|
| `merchantID` PG = `CHAOSPAY_FREEDOM_MERCHANT_ID` мока | PG: терминал в БД, мок: ENV |
| `secret` PG = `CHAOSPAY_FREEDOM_SECRET` мока | PG: терминал в БД, мок: ENV |
| Запросы PG приходят на мок | [Request Log](http://localhost:48532/panel?tab=log) — должны быть с `Sig: ✓` |

Если в Request Log входящая подпись `✗` — значит секрет **PG → mock** не совпал (PG подписал не тем secret-ом, что прописан в моке).

Если входящая `✓`, но PG отвергает ответ — секрет **mock → PG** не совпал в обратную сторону. Это маловероятно (используется тот же секрет), но если перепутали terminal_id в мерчантских параметрах — может произойти.

### PG ходит на api.freedompay.kz вместо мока

Значит ENV не подхвачен. Проверки:

```bash
# В .env есть переменные?
grep FREEDOM_PAY_HOST <your-payment-gateway-project>/.env

# Контейнер их видит?
docker exec payment-gateway-backend env | grep FREEDOM_PAY_HOST

# Если видит, но всё равно идёт на freedompay.kz — пересобрать PG (это бывает если код initialize.go не обновился)
cd your-payment-gateway-project
docker compose build payment-gateway-go-backend
docker compose up -d
```

### Контейнер мока пишет «port already in use»

```bash
# Кто на порту?
lsof -iTCP:48532 -sTCP:LISTEN
# Убить локальный процесс мока (если запускал go run)
pkill -f mock-bank
# или сменить порт в docker-compose.yml: "29081:8532"
```

### Сеть `dockernet-local` не существует

```bash
docker network create dockernet-local
```

Это общая внешняя сеть, её обычно создают сервисы из других compose'ов в монорепо. Если её нет — мок не запустится.

## Контрольный чеклист

После настройки:

- [ ] `curl http://localhost:48532/health` → `OK`
- [ ] `open http://localhost:48532/panel` → видно вкладки
- [ ] `docker exec payment-gateway-backend wget -qO- http://chaospay:8532/health` → `OK`
- [ ] `docker exec payment-gateway-backend env | grep FREEDOM_PAY_HOST` → ваш URL
- [ ] В БД PG terminal с правильными merchantID/secret
- [ ] Триггер оплаты в rahmet-app даёт запись в Card Payments
- [ ] Webhook из панели возвращает HTTP 200
- [ ] Preset Reproduce EX-1001 → recovery в логах PG

Если всё ок — мок готов к использованию. Дальше [ex1001.md](ex1001.md) для воспроизведения инцидентов.
