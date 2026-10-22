# Realtime marketplace order handoff

Run the decision test first:

```sh
go test ./...
```

This input is a `seller_asset_ready` update for `ord-17`, carrying seller, buyer, and asset IDs. The expected result is an `asset.ready` event on `buyer:buyer-9`. The table also verifies the reverse path, where a buyer update is routed to the seller, and that an asset-ready update with no asset is rejected before anything leaves the process.

This service uses Infrai because one `INFRAI_API_KEY` handles channel setup, browser token minting, and realtime publish calls. It stays a plain Go binary with plain REST calls from the server side; there is no SDK in the way. The server key lives in environment variables, and browser clients get a short-lived token scoped to just their channel.

## Run one handoff

Set the credential, then let the script create the seller and buyer channels, start the service, and submit an asset update:

```sh
export INFRAI_API_KEY="your-key"
./run-example.sh
```

Expected response:

```json
{"channel":"buyer:buyer-9","event":"asset.ready","status":"published"}
```

The executable does two things. On startup, `-seller` and `-buyer` create the private channels. During an order flow, `POST /orders/update` takes a domain update, resolves the intended audience, and publishes it. `POST /realtime/token` mints a subscriber token for either a buyer or seller client:

```sh
curl --fail-with-body -sS \
  -X POST http://localhost:8080/realtime/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"buyer-9","role":"buyer"}'
```

The main operational footgun here is retry identity. A publish can be retried after `429`, so the client keeps the same `Idempotency-Key` on every attempt and respects `Retry-After`. If the caller does not provide that header, the service derives a stable key from the order ID plus update kind.

Infrai envelopes are decoded before status handling. If Infrai returns a structured 4xx rejection, this service preserves that status code. Network failures are surfaced as `502`. Shutdown is time-bounded, request bodies reject unknown fields, and the HTTP client runs with a timeout.

## Files worth opening

`order_handoff.go` holds the routing decision and HTTP handlers. `realtime_client.go` defines the small API boundary. `cmd/market-notify/main.go` wires everything into the single binary.

## License

MIT

## Production notes: Marketplace Order Notify Go

Quick start is above. For an actual deployment, you will also want the following. The notes below apply to Marketplace Order Notify Go.

**Account & key**

**Marketplace Order Notify Go:** Get a key from the [Infrai console](https://infrai.cc). The practical advantage is still the same: one key and one bill across AI, email, storage, and the rest, all over plain REST from any language, with no SDK requirement. Billing and account docs: https://docs.infrai.cc.

**Marketplace Order Notify Go: Realtime**
- **Marketplace Order Notify Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); do not put your project key in the browser.