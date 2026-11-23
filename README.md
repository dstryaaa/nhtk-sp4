# Склад с резервированием товаров

REST API на Go + PostgreSQL для учёта товаров, остатков и заказов с атомарным резервированием.

## Бизнес-требования

- Товар: уникальный SKU, название, цена, физический остаток.
- Заказ: статус (`Reserved`/`Cancelled`), дата, позиции.
- Позиция: товар, количество, **цена на момент заказа** (`price_at_order`).
- SKU уникален; цена и количество > 0; остаток не ниже нуля.
- Создание заказа **атомарно**: все позиции или ничего.
- Повтор SKU в заказе **объединяется** (см. «Решение по повтору SKU»).
- Цена фиксируется при создании заказа.
- Отмена только `Reserved`; повторная отмена не меняет остаток.
- Конкурентное резервирование последней единицы не даёт отрицательного остатка.

## Реализованный функционал

| Метод | Путь | Описание | Коды |
|---|---|---|---|
| POST | `/api/products` | Создать товар | 201, 400, 409, 500 |
| GET | `/api/products` | Список с пагинацией и поиском | 200, 500 |
| GET | `/api/products/{id}` | Товар по id | 200, 400, 404, 500 |
| PATCH | `/api/products/{id}/stock` | Установить остаток | 200, 400, 404, 500 |
| POST | `/api/orders` | Создать заказ + резерв | 201, 400, 404, 409, 500 |
| GET | `/api/orders/{id}` | Заказ с позициями | 200, 400, 404, 500 |
| POST | `/api/orders/{id}/cancel` | Отменить резерв | 200, 400, 404, 500 |

## Эндпоинты — примеры

### POST /api/products
```bash
curl -X POST localhost:8080/api/products \
  -H 'Content-Type: application/json' \
  -d '{"sku":"SKU-1","name":"Widget","price":10.5,"stock":100}'
```
**201:**
```json
{"id":1,"sku":"SKU-1","name":"Widget","price":"10.50","stock":100,"created_at":"..."}
```
**409 (дубль SKU):** `{"error":"sku already exists"}`  
**400 (валидация):** `{"error":"price must be > 0"}`

### GET /api/products?limit=20&offset=0&q=widget
**200:** `[{"id":1,...}]`  
**200 (пусто):** `[]` (не `null`)

### GET /api/products/{id}
**200:** `{"id":1,"sku":"SKU-1","name":"Widget","price":"10.50","stock":100,"created_at":"..."}`  
**404:** `{"error":"product not found"}`  
**400:** `{"error":"invalid id"}` (если id не число)

### PATCH /api/products/{id}/stock
```bash
curl -X PATCH localhost:8080/api/products/1/stock \
  -H 'Content-Type: application/json' -d '{"stock":42}'
```
**200:** обновлённый товар. **400:** `{"error":"stock must be >= 0"}`.

### POST /api/orders
```bash
curl -X POST localhost:8080/api/orders \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"product_id":1,"quantity":2},{"product_id":1,"quantity":3}]}'
```
**201:** (дубли `product_id=1` слиты в quantity=5)
```json
{"id":1,"status":"Reserved","created_at":"...","cancelled_at":null,
 "items":[{"product_id":1,"quantity":5,"price_at_order":"10.50","sku":"SKU-1","name":"Widget"}]}
```
**409 (не хватает):** `{"error":"insufficient stock"}`  
**404 (нет товара):** `{"error":"product not found"}`

### GET /api/orders/{id}
**200:** заказ с позициями (как выше). **404:** `{"error":"order not found"}`.

### POST /api/orders/{id}/cancel
**200:** `{"status":"Cancelled","cancelled_at":"...",...}`  
**200 (повторно):** идемпотентно, `cancelled_at` **не меняется**, остаток **не меняется**.  
**404:** `{"error":"order not found"}`.

## Развёртывание (WSL + PostgreSQL)

```bash
# 1. PostgreSQL уже установлен в WSL (pg_lsclusters → online)
sudo service postgresql start

# 2. Создать роль и БД (если нет)
sudo -u postgres psql -c "CREATE ROLE semenov_n_m LOGIN PASSWORD 'password123' CREATEDB;"
createdb -O semenov_n_m semenov_db
createdb -O semenov_n_m semenov_test_db

# 3. Накатить миграции
psql "postgres://semenov_n_m:password123@localhost:5432/semenov_db?sslmode=disable" \
  -v ON_ERROR_STOP=1 -f migrations/0001_init.sql

# 4. Создать .env
echo 'DATABASE_URL=postgres://semenov_n_m:password123@localhost:5432/semenov_db?sslmode=disable' > .env

# 5. Запуск
set -a; source .env; set +a
go run ./cmd/api
```

## Тестирование

```bash
# Одна команда: миграции + тесты + HTML-отчёт
./run_tests.sh
```

### Что делает `run_tests.sh`
1. Накатывает миграции на `semenov_test_db` (`psql -v ON_ERROR_STOP=1 -f migrations/0001_init.sql`).
2. Запускает `go test -p 1 ./internal/product/ ./internal/order/ -coverprofile=coverage.out`.
3. Генерирует `coverage.html` (`go tool cover -html=coverage.out -o coverage.html`).
4. Печатает общее покрытие (`go tool cover -func=coverage.out | tail -1`).

Эквивалент вручную:
```bash
psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
go test -p 1 ./internal/product/ ./internal/order/ -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

- **Покрытие: 80.1%** строк.
- HTML-отчёт: `coverage.html`, путь: `file:///home/semenov-n-m/warehouse/coverage.html`.
- Исключены из подсчёта:
  - `Register` (0%) — обёртка над `http.ServeMux`, проверена через `curl`-сценарии выше;
  - ветки **500** (ошибки БД) — требуют мока `pgxpool`;
  - страховки `23514` в `Create`/`UpdateStock` — не срабатывают при валидации в Go.
- **Конкурентный тест:** `TestConcurrentReserve` — 2 горутины резервируют последнюю единицу; один 201, один 409, stock=0. Прогнан **5 раз** подряд — стабильно.

## Команды сборки/запуска

```bash
go build ./...                 # сборка
go run ./cmd/api               # запуск (нужен DATABASE_URL)
go test -p 1 ./internal/product/ ./internal/order/ -v   # тесты
./run_tests.sh                 # тесты + HTML-отчёт
```

> `-p 1` — пакеты тестируются **последовательно**, т.к. `product` и `order` используют одну тестовую БД (`semenov_test_db`) и делают `TRUNCATE`; параллельный запуск даёт гонку (deadlock / «product not found»).

## Обоснование блокировки

Выбран **атомарный `UPDATE ... WHERE stock >= $1 RETURNING price`** вместо `SELECT ... FOR UPDATE` и `SERIALIZABLE`:

- **Проверка и списание — в одном statement.** Postgres берёт row lock на время `UPDATE`; условие `WHERE stock >= $1` проверяется под локом. Гонки TOCTOU (time-of-check to time-of-use) нет **by design**.
- **Меньше round-trip'ов**, чем `SELECT FOR UPDATE` + `UPDATE` (два запроса).
- **Не нужен retry**, как при `SERIALIZABLE` (где при конфликте — `40001 serialization_failure`, и нужна логика повтора).
- **Конкурентный тест** (`TestConcurrentReserve`, 5 прогонов) подтверждает: две горутины на последнюю единицу → один 201, один 409, stock=0, без «-1».

Если `UPDATE` вернул 0 строк — различаем 404 (нет товара, через `SELECT EXISTS`) и 409 (не хватает остатка).

## Решение по повтору SKU

Если клиент присылает два раза один `product_id` — количества **суммируются** до вставки (`mergeItems` в хендлере). Составной PK `(order_id, product_id)` в `order_items` — страховка на уровне схемы (повтор физически невозможен).

API принимает `product_id` (клиент получает `id` из `GET /api/products`). Повтор `product_id` эквивалентен повтору SKU (1:1).

## Известные ограничения

- Поиск `ILIKE '%q%'` **не** использует B-tree индекс; для продакшн-объёма — `pg_trgm` + GIN. Сейчас — без индекса (объём учебный).
- `%` и `_` в `q` **не экранируются** (трактуются как wildcard). В проде — `ESCAPE '\'`.
- Ветки 500 (ошибки БД) **не покрыты** тестами — требуют мока `pgxpool`.
- `PATCH /stock` **устанавливает** абсолютное значение (не прибавляет). Для резервирования — заказы.