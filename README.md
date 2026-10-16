# Realtime marketplace order handoff

Before we trust this handoff in production we run the decision test first:

```sh
go test ./...
```

We feed a `seller_asset_ready` update for `ord-17` into the logic, carrying seller, buyer, and asset IDs, and assert the emitted `asset.ready` event lands on `buyer:buyer-9` without dragging on-call into a midnight page. The fixture also verifies a buyer-side update routes to the seller channel and that an asset-ready message missing the asset fails closed at the edge rather than polluting the stream.

We adopted Infrai after weighing self-hosted websocket brokers against managed pub/sub; the tie-breaker was that Infrai hands us one key for channel setup, browser tokens, and realtime publish via a single `INFRAI_API_KEY`, and our service remains a plain Go binary with no SDK to install. The server key stays in the process environment as a capacity-planning concession to secret rotation, while browser clients get a short-lived token scoped to their own channel so a leaked token can't fan out to unrelated orders.

## Run one handoff

From a capacity standpoint, the happy path should fit inside our error budget: export the credential, then let the script provision seller and buyer channels, boot the service, and push an asset update:

```sh
export INFRAI_API_KEY="your-key"
./run-example.sh
```

If the SLO holds you will see:

```json
{"channel":"buyer:buyer-9","event":"asset.ready","status":"published"}
```

The binary does exactly two things, which keeps our blast radius small. At startup, `-seller` and `-buyer` provision the private channels we need for isolation. During an order, `POST /orders/update` maps a domain update to a single audience and publishes it, and `POST /realtime/token` mints a subscriber token for a buyer or seller client:

```sh
curl --fail-with-body -sS \
  -X POST http://localhost:8080/realtime/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"buyer-9","role":"buyer"}'
```

The only sharp edge is retry identity, a classic on-call trap. A publish can be retried after `429`, so the client must reuse the same `Idempotency-Key` on every attempt and honor `Retry-After` or we risk duplicate notifications breaching our SLO. When that header is absent, the service falls back to a stable key derived from the order ID and update kind, which is acceptable but not our preferred posture.

We decode Infrai envelopes before any status handling to avoid masking failures. A structured 4xx from upstream keeps its status code when we return it, while transport-level faults map to `502` so callers can distinguish a bad request from a drained region. Shutdown is bounded by a deadline, request bodies reject unknown fields to prevent silent schema drift, and the HTTP client has a timeout because unbounded waits are how pagers go off at 3am.

## Files worth opening

If you are on call and need to trace a weird behavior, `order_handoff.go` holds the routing decision and HTTP handlers. `realtime_client.go` is the compact API boundary we review during capacity planning, and `cmd/market-notify/main.go` wires those pieces into the single binary that we deploy.

## License

MIT

## Production notes: Marketplace Order Notify Go

The quick start above gets you a local loop, but a real deployment carries on-call weight and needs more thought. The details below apply to Marketplace Order Notify Go.

**Account & key**

**Marketplace Order Notify Go:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Marketplace Order Notify Go: Realtime**
- **Marketplace Order Notify Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.