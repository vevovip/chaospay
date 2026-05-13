.PHONY: help install-tools install-lint build run up down restart logs health lint lint-fix fmt test test-unit test-integration test-all clean

MOCK_URL ?= http://localhost:48532
# golangci-lint версия пинится через go.mod (`tool` directive). Эта переменная — для install-lint.
GOLANGCI_LINT_VERSION ?= v2.9.0

help: ## Список команд
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' Makefile | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

install-tools: install-lint ## Установить dev-инструменты (через go.mod tool directive)
	@echo "✓ tools installed via go.mod tool directive"

install-lint: ## Запинить golangci-lint в go.mod (один раз)
	go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: ## Запустить golangci-lint (версия из go.mod)
	go tool golangci-lint run -v

lint-fix: ## golangci-lint --fix
	go tool golangci-lint run --fix

fmt: ## gofmt -w
	gofmt -w .

build: ## go build всех пакетов
	go build ./...

run: ## Локальный запуск chaospay (без docker)
	go run ./cmd/chaospay

up: ## Поднять docker-контейнер
	docker compose build && docker compose up -d

down: ## Остановить контейнер
	docker compose down

restart: ## Пересобрать + рестарт
	docker compose build && docker compose up -d --force-recreate

logs: ## Логи контейнера
	docker logs -f chaospay

health: ## Проверка /health
	@curl -sS -m 3 $(MOCK_URL)/health && echo " ← $(MOCK_URL)" || (echo "DOWN — попробуй make up" && exit 1)

test: test-unit ## Алиас на test-unit

test-unit: ## Юнит-тесты пакетов (без поднятого контейнера)
	go test -race -count=1 ./...

test-integration: ## Интеграционные тесты (требуют запущенный мок на $(MOCK_URL))
	@$(MAKE) health
	cd tests/integration && go build -o /tmp/chaospay-itest . && /tmp/chaospay-itest

test-all: test-unit ## Юнит + интеграционные. Сам поднимет контейнер если нужно.
	@curl -sS -m 2 $(MOCK_URL)/health >/dev/null 2>&1 || $(MAKE) up
	@sleep 1
	@$(MAKE) test-integration

clean: ## Очистить временные артефакты
	rm -f /tmp/chaospay-itest
	go clean -testcache
