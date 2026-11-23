#!/usr/bin/env bash
set -euo pipefail

TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://semenov_n_m:password123@localhost:5432/semenov_test_db?sslmode=disable}"
COVERAGE_FILE="coverage.out"
COVERAGE_HTML="coverage.html"

echo "==> Накатываю миграции на тестовую БД"
psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql >/dev/null 2>&1

echo "==> Запускаю тесты с покрытием (последовательно, -p 1)"
TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -p 1 \
  ./internal/product/ \
  ./internal/order/ \
  -coverprofile="$COVERAGE_FILE"

echo "==> Генерирую HTML-отчёт"
go tool cover -html="$COVERAGE_FILE" -o "$COVERAGE_HTML"

echo "==> Общее покрытие:"
go tool cover -func="$COVERAGE_FILE" | tail -1

echo "==> HTML-отчёт: file://$(pwd)/$COVERAGE_HTML"