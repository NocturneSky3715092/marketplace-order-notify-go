#!/bin/sh
set -eu

: "${INFRAI_API_KEY:?set INFRAI_API_KEY}"

go run ./cmd/market-notify -seller seller-4 -buyer buyer-9 &
service_pid=$!
trap 'kill "$service_pid" 2>/dev/null || true' EXIT INT TERM
sleep 1

curl --fail-with-body -sS \
  -X POST http://localhost:8080/orders/update \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-ord-17-asset-2' \
  -d '{"order_id":"ord-17","seller_id":"seller-4","buyer_id":"buyer-9","kind":"seller_asset_ready","asset_id":"asset-2"}'
printf '\n'
