# Contributing to ChaosPay

Спасибо, что хочешь сделать вклад! 🎉

## С чего начать

```bash
git clone https://github.com/vevovip/chaospay.git
cd chaospay
make up               # docker compose build + up
make health           # → OK
make test-all         # юнит + интеграция, 150+ тестов
```

Если `make test-all` зелёный — окружение готово.

## Workflow

1. **Fork репозиторий** (если ты не коллаборатор)
2. **Создай feature-ветку**: `git checkout -b feat/your-feature`
3. **Пиши код** + **тесты** (unit или integration)
4. **Прогоняй локально**:
   ```bash
   make test-all
   ```
5. **Commit** с осмысленным сообщением:
   ```bash
   git commit -m "feat: add preset for kaspi timeout recovery"
   ```
6. **Push + open PR**
7. CI запустит lint + unit-tests + integration-tests — должно быть всё зелёное

## Гайды по добавлению фич

### Новый scenario action

См. подробно в [CLAUDE.md](CLAUDE.md#как-добавлять-scenario-actions). Кратко:

1. Константа в [`internal/domain/scenario/scenario.go`](internal/domain/scenario/scenario.go) + добавить в `AllActions`
2. Реализация:
   - Transport-уровень (универсально) → [`internal/ports/api/scenarioapply/transport.go`](internal/ports/api/scenarioapply/transport.go)
   - Content-XML → `applyScenarioAfter` в [`internal/ports/api/pay/scenario.go`](internal/ports/api/pay/scenario.go)
   - Content-JSON (wallet) → switch в [`internal/ports/api/wallet/handler.go`](internal/ports/api/wallet/handler.go)
3. Если action принимает новый параметр — добавь input в [panel/scenarios.go](internal/ports/panel/scenarios.go) и ключ в `handleScenarioAdd`
4. Юнит-тесты + интеграционный smoke-test

### Новый preset

В [`internal/application/scenario/service.go`](internal/application/scenario/service.go):

1. `PresetInfo` в `AllPresets` — обязательно с непустым `Sample` (UI ❔)
2. `case "name":` в `ApplyPreset(name)`
3. Имя влияет на группу в UI и цвет кнопки — см. `scenarioPresetGroup` в [panel/scenarios.go](internal/ports/panel/scenarios.go). При необходимости расширь функцию.

Тесты в [`internal/application/scenario/service_test.go`](internal/application/scenario/service_test.go) автоматически проверят, что preset производит scenarios и имеет Sample.

### Мок для другого банка

Архитектура hexagonal — для нового банка нужен:

- Подпапка в [`internal/infrastructure/`](internal/infrastructure/) с протоколом (например, `kaspi/`)
- Handler в [`internal/ports/api/`](internal/ports/api/) с реальными endpoint-ами банка
- (Опционально) новые actions, если протокол требует специфичных модификаций

См. [docs/architecture.md](docs/architecture.md#расширение-под-другие-банки) для подробностей.

## Конвенции кода (Go)

Следуй стилю из [CLAUDE.md](CLAUDE.md#конвенции):

- Имена файлов — `lowercase.go`, без подчёркиваний
- Идентификаторы: `userID` (не `userId`), акронимы заглавными (`HTTP`, `ID`, `API`)
- Структура файла: `package` → `import` → `const` → `var` → `type` → конструкторы → методы
- Магические строки → именованные константы
- Не использовать `interface{}` — `any` (Go 1.18+)
- Все типы — в `domain/`, без зависимостей на infra
- Без эмодзи в коде (UI-кнопки и docs — окей)

## Конвенции commit-сообщений

Conventional commits (рекомендуется):

```
<type>: <subject>

<optional body>
```

Типы:
- `feat:` — новая функциональность
- `fix:` — багфикс
- `docs:` — только документация
- `test:` — добавление/правка тестов
- `refactor:` — рефакторинг без изменения поведения
- `chore:` — обслуживание (build, deps)

Примеры:
- `feat: add kaspi qr-pay mock endpoints`
- `fix: missing_field doesn't remove pg_sig`
- `test: cover GooglePay form-encoded path`

## CI требования

Pull request пройдёт когда **все 3 job-а зелёные**:

1. `lint` — `golangci-lint v2.1.0` (см. [.golangci.yml](.golangci.yml))
2. `unit-tests` — `go test -race -count=1 ./...`
3. `integration-tests` — `make test-integration` против поднятого docker-контейнера

## Что НЕ принимаем

- Изменения функциональности без тестов
- Mass-rename без согласования
- Зависимости от внешних сервисов в юнит-тестах (только in-memory/mocks)
- Падающий или закомментированный код в `_test.go`

## Связь

Issues и discussions в репозитории — лучший способ. Для security-issues отправь email maintainer-у.

---

Happy chaos engineering! 🌪
