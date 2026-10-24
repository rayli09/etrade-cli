# etrade-cli

A Go command-line client for the **E*TRADE Developer Platform REST API
v1** — modeled 1:1 on [schwab-cli](https://github.com/rayli09/schwab-cli)
(same layout, conventions, and safety rails), with the endpoint surface
taken from the official E*TRADE API documentation and cross-checked
against the Python wrapper
[pyetrade](https://github.com/jessecooper/pyetrade).

There is no official E*TRADE CLI or SDK. What E*TRADE publishes is the
REST API plus sample code; this project implements the full documented
v1 surface as a CLI: OAuth 1.0a authorization, accounts, alerts, market
data, and orders — 24 endpoints, 24 leaf commands.

OAuth 1.0a HMAC-SHA1 signing is hand-written on the Go standard library
(`crypto/hmac`, `crypto/sha1`) — no third-party OAuth library. Signing is
verified in tests against the canonical RFC 5849 test vector.

## Install

```sh
# With Go 1.27+
go install github.com/rayli09/etrade-cli@latest

# Or build from source
git clone https://github.com/rayli09/etrade-cli.git
cd etrade-cli
go build -o etrade-cli .
```

## Getting E*TRADE API keys

You need an E*TRADE account and a **consumer key / consumer secret**
pair. Sandbox and production use *separate* keys.

1. **Sandbox key** (self-serve): log in to your E*TRADE account and
   request one at <https://us.etrade.com/etx/ris/apikey>.
2. **Live (production) key**: on
   [developer.etrade.com/getting-started](https://developer.etrade.com/getting-started),
   complete, in order:
   1. the **API User Intent Survey**,
   2. the **API Developer Agreement**,
   3. the **Market Data Agreement** (plus the Extended Hours Trading
      Agreement if you want after-hours trading).

   **Individual keys are issued immediately on the page** once those are
   satisfied, and work only with your own account. Vendor keys (for
   multi-user apps) are issued inactive and activated only after a demo
   with E*TRADE's Product and Legal teams. A Market Data Attestation
   must be renewed annually.

## Auth setup

```sh
export ETRADE_CONSUMER_KEY=...        # production
export ETRADE_CONSUMER_SECRET=...
# or, for the sandbox:
export ETRADE_SANDBOX_CONSUMER_KEY=...
export ETRADE_SANDBOX_CONSUMER_SECRET=...

etrade-cli auth login                 # add --sandbox for the sandbox
```

`auth login` performs the OAuth 1.0a three-legged flow by copy-paste:

1. It fetches a request token and prints the authorize URL
   (`https://us.etrade.com/e/t/etws/authorize?key=…&token=…`).
2. Open it, log in, allow access, and copy the **verifier code**.
3. Paste it at the prompt (or pass `--verifier`). The CLI exchanges it
   for an access token and writes the token file (`./token.json`, or
   `./token.sandbox.json` with `--sandbox`; override with `--token-path`
   / `ETRADE_TOKEN_PATH`) with **0600 permissions**, atomically.

Credentials can also come from `--consumer-key` / `--consumer-secret`
flags, or from a JSON config file (`--config`, default
`~/.etrade-cli.json` when present — keep it 0600; the CLI warns if it is
group/world-readable):

```json
{
  "consumer_key": "...",
  "consumer_secret": "...",
  "sandbox_consumer_key": "...",
  "sandbox_consumer_secret": "..."
}
```

No command ever prints token or secret values — `auth status` reports
only ages and expiry state.

### The daily-login reality

E*TRADE access tokens **expire at midnight US Eastern Time** on the day
they are issued. There is no refresh token. If a token sees no API calls
for **two hours** it goes *inactive*; `etrade-cli auth renew` (the Renew
Access Token endpoint) reactivates it — but only the same day. After
midnight, renewal fails and `auth login` is the only way back in. This is
strictly worse than Schwab's 7-day refresh-token cycle, and it is a
property of E*TRADE's platform, not of this CLI: plan on one browser
login per trading day. Commands run with an expired stored token fail
with a clear “expired at midnight US Eastern; re-run `etrade-cli auth
login`” error before anything is sent.

## Usage

Commands are grouped by API area; every read command prints the API
response (pretty-printed JSON by default; `--format xml` requests XML
instead). Production is the default environment; pass `--sandbox` for
the sandbox (`https://apisb.etrade.com` vs `https://api.etrade.com`).

```sh
etrade-cli accounts list
etrade-cli accounts balance <account-id-key> --real-time-nav true
etrade-cli accounts portfolio <account-id-key> --view COMPLETE --lots-required true
etrade-cli accounts portfolio-lot <account-id-key> <position-id>
etrade-cli accounts transactions <account-id-key> --start-date 01012026 --end-date 01312026
etrade-cli accounts transaction-details <account-id-key> <transaction-id>

etrade-cli market lookup "apple"
etrade-cli market quote AAPL MSFT --detail-flag ALL
etrade-cli market quote GOOG:2027:1:15:CALL:200        # option symbol format
etrade-cli market option-chains GOOG --expiry-date 2027-01-15 --chain-type CALL
etrade-cli market option-expire-dates GOOG --expiry-type ALL

etrade-cli orders list <account-id-key> --status OPEN
etrade-cli orders details <account-id-key> <order-id>
etrade-cli alerts list --status UNREAD --category STOCK
etrade-cli alerts details <alert-id>
```

Global flags: `--consumer-key`, `--consumer-secret`, `--config`,
`--sandbox`, `--token-path`, `--timeout`, `--base-url` (testing
override), `--format json|xml`. Every command has `--help`.

### Order safety: dry-run by default

`orders place`, `orders place-changed`, `orders cancel`, and
`alerts delete` **send nothing** unless you pass `--execute`. Without it
they print the exact request (method, path, JSON body) they would send —
no credentials or token are needed to produce that preview.

Order bodies are the full `PreviewOrderRequest` / `PlaceOrderRequest`
JSON objects from the official Order API documentation, supplied with
`--file`, `--json`, or stdin. Because every documented order type
travels in that body, all of them are supported: equity (`EQ`), single-
and multi-leg options (`OPTN`, incl. `SPREADS`), mutual funds (`MF`),
and the rest of the documented vocabulary.

```sh
# 1. Preview (changes nothing; returns a previewId)
etrade-cli orders preview <account-id-key> --file order.json

# 2. Place — dry-run first, then for real. The previewId is REQUIRED:
# E*TRADE will not place an order that was not previewed.
etrade-cli orders place <account-id-key> --file place.json --preview-id 12345
etrade-cli orders place <account-id-key> --file place.json --preview-id 12345 --execute

# Changing an order is the same two-step: change-preview, then place-changed.
# (E*TRADE implements a change as cancel-original + create-new.)
etrade-cli orders change-preview <account-id-key> <order-id> --file change.json
etrade-cli orders place-changed <account-id-key> <order-id> --file change.json --preview-id 67890 --execute

etrade-cli orders cancel <account-id-key> <order-id>            # dry-run
etrade-cli orders cancel <account-id-key> <order-id> --execute
```

If a place body already contains `PreviewIds`, `--preview-id` is not
needed; if neither is present, the command fails with an explanation of
the preview-first rule before anything is sent.

## Parity checklist (E*TRADE API v1 → etrade-cli)

### Authorization (OAuth 1.0a)

| Official API | etrade-cli | Endpoint |
|---|---|---|
| Get Request Token | inside `auth login` | `GET /oauth/request_token` |
| Authorize Application | inside `auth login` (prints URL) | `GET https://us.etrade.com/e/t/etws/authorize?key=…&token=…` |
| Get Access Token | inside `auth login` | `GET /oauth/access_token` |
| Renew Access Token | `auth renew` | `GET /oauth/renew_access_token` |
| Revoke Access Token | `auth revoke` | `GET /oauth/revoke_access_token` |
| — (local) | `auth status` | reads the token file; no API call |

### Accounts

| Official API | etrade-cli | Endpoint |
|---|---|---|
| List Accounts | `accounts list` | `GET /v1/accounts/list` |
| Get Account Balances | `accounts balance` | `GET /v1/accounts/{accountIdKey}/balance` |
| View Portfolio | `accounts portfolio` | `GET /v1/accounts/{accountIdKey}/portfolio` |
| View Portfolio (position lot) | `accounts portfolio-lot` | `GET /v1/accounts/{accountIdKey}/portfolio/{positionId}` |
| List Transactions | `accounts transactions` | `GET /v1/accounts/{accountIdKey}/transactions` |
| List Transaction Details | `accounts transaction-details` | `GET /v1/accounts/{accountIdKey}/transactions/{transactionId}` |

### Alerts

| Official API | etrade-cli | Endpoint |
|---|---|---|
| List Alerts | `alerts list` | `GET /v1/user/alerts` |
| List Alert Details | `alerts details` | `GET /v1/user/alerts/{id}` |
| Delete Alert | `alerts delete` (dry-run unless `--execute`) | `DELETE /v1/user/alerts/{id,...}` |

### Market

| Official API | etrade-cli | Endpoint |
|---|---|---|
| Look Up Product | `market lookup` | `GET /v1/market/lookup/{search}` |
| Get Quotes | `market quote` | `GET /v1/market/quote/{symbols}` |
| Get Option Chains | `market option-chains` | `GET /v1/market/optionchains` |
| Get Option Expire Dates | `market option-expire-dates` | `GET /v1/market/optionexpiredate` |

Option chains expose every documented filter: `symbol`, `expiryDay`,
`expiryMonth`, `expiryYear` (or `--expiry-date YYYY-MM-DD`), `chainType`,
`strikePriceNear`, `noOfStrikes`, `includeWeekly`, `skipAdjusted`,
`optionCategory`, `priceType`.

### Orders

| Official API | etrade-cli | Endpoint |
|---|---|---|
| List Orders | `orders list` | `GET /v1/accounts/{accountIdKey}/orders` |
| (order details) | `orders details` | `GET /v1/accounts/{accountIdKey}/orders/{orderId}` |
| Preview Order | `orders preview` | `POST /v1/accounts/{accountIdKey}/orders/preview` |
| Place Order | `orders place` (dry-run unless `--execute`) | `POST /v1/accounts/{accountIdKey}/orders/place` |
| Change Previewed Order | `orders change-preview` | `PUT /v1/accounts/{accountIdKey}/orders/{orderId}/change/preview` |
| Place Changed Order | `orders place-changed` (dry-run unless `--execute`) | `PUT /v1/accounts/{accountIdKey}/orders/{orderId}/change/place` |
| Cancel Order | `orders cancel` (dry-run unless `--execute`) | `PUT /v1/accounts/{accountIdKey}/orders/cancel` |

pyetrade equivalents are covered one-for-one as well, including
`find_option_orders` semantics (a client-side filter over
`orders list --status OPEN`, not a separate endpoint) and its
preview-equity / preview-option split (a distinction that exists only
in the request body: `orderType` / `securityType`).

### Not ported, honestly

- **Streaming / push notifications.** The legacy E*TRADE streaming API
  is a Comet-style long-polling service documented separately from the
  REST v1 API; it is not part of the v1 endpoint surface above and is
  not implemented. Poll `orders list` / `alerts list` instead.
- **Legacy pre-v1 REST services** (`etws.etrade.com` — e.g. the old
  notifications and rate-limit services) are superseded by the v1 API
  and are not implemented. The current v1 documentation defines no
  notifications or system-status endpoints.
- **Equity Edge / corporate-services APIs** (`corpservices.etrade.com`)
  are a different product for stock-plan administrators, out of scope.

### Format notes

Reads request JSON via the `.json` path suffix used by the official
examples and pyetrade (e.g. `/v1/market/quote/AAPL.json`); writes send
JSON bodies with an `Accept: application/json` header. `--format xml`
drops the suffix and requests XML; response bodies are printed verbatim
when they are not JSON.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

Tests never touch the real API: signing is checked against the RFC 5849
vector, the OAuth flow and every REST endpoint are exercised against a
local `httptest` stub that records method, path, query, headers, and
body, token-file permissions/expiry are tested on disk, and the dry-run
rail is tested by running the real command tree without `--execute`
(which succeeds with no credentials present — proof nothing was sent).

CI (GitHub Actions) runs gofmt, vet, build, and tests on every push.

## License & attribution

MIT. The endpoint surface and OAuth flow follow the official
[E*TRADE Developer Platform](https://developer.etrade.com)
documentation and were cross-checked against
[pyetrade](https://github.com/jessecooper/pyetrade) — Copyright (c)
Jesse Cooper and the pyetrade contributors (see `LICENSE` and `NOTICE`).
Not affiliated with or endorsed by E*TRADE or Morgan Stanley. Trading
involves risk; this tool's dry-run default exists for a reason — check
every request before you add `--execute`.
