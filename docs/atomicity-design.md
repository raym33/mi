# Design: cross-ledger accounting atomicity

Status: **proposal, not implemented.** This documents the remaining tail of
ARCHITECTURE.md limitation #2 (billing is not crash-transactional) and the
options to close it, so the fix is chosen deliberately rather than rushed. It
exists because the naive "just wrap both writes in a transaction" approach does
not work here, for reasons made concrete below.

## The problem

A completed request produces two separate writes:

1. **Usage** — `city.Market.RecordReserved` increments per-consumer and
   per-provider aggregate counters and persists them to the city database.
2. **Settlement** — `settlement.Ledger.Record` appends a hash-chained event
   (debit, reward, penalty) to the settlement database.

They are not atomic. A crash between them leaves usage and settlement
inconsistent, and both errors are currently only logged, not surfaced.

## Why the obvious fix does not apply

- **Two physical databases.** City state and settlement use different SQLite
  files (`city.sqlite_path`, `settlement.sqlite_path`). SQLite transactions are
  per-database, so the two writes cannot share one transaction as the code
  stands.
- **Usage is an aggregate, not a log.** `addUsage` keeps running totals
  (`requests`, `prompt_tokens`, `completion_tokens`) keyed by account. There are
  no per-request rows, so usage cannot be deduplicated or replayed by
  `request_id`.
- **Settlement cannot fully reconstruct usage today.** `settlement.Snapshot`
  exposes per-account balances with `TotalTokens`, but not the
  prompt/completion split that usage tracks. So "rebuild usage from settlement"
  needs a settlement schema/API addition first.

## Options

### A. Unify the two ledgers in one database
Move usage and settlement tables into a single SQLite database and wrap
`RecordReserved` + `Record` in one transaction. Strongest guarantee (true
atomic commit). Largest change: merges two packages' persistence and their
lifecycle/config; settlement's hash chain and the usage tables would share a
connection and migration story.

### B. Settlement as source of truth; usage as a derived projection
Make the hash-chained settlement log authoritative. Usage becomes an in-memory
projection seeded from settlement at startup and updated per request. On a crash
between the two writes, the next boot re-derives usage from settlement, so the
money record (settlement) is never lost and usage self-heals. Requires:
- settlement to expose per-account prompt/completion totals (schema/API add), so
  the projection can be rebuilt exactly;
- the coordinator (which already composes both) to seed `Market` usage from
  settlement at startup — keeping the packages decoupled (no `city → settlement`
  import).
Caveat: only helps when settlement is enabled; with settlement disabled, usage
is the sole record and there is a single write (no cross-ledger problem).

### C. Ordered write + intent outbox
Write an intent row first, then settlement, then usage, marking the intent done;
reconcile dangling intents on startup. Closest to classic outbox semantics, but
adds the most moving parts and a reconciliation path to test.

## Recommendation

**Option B**, with a deliberate ordering change as an immediate, low-risk
half-step:

- **Now (safe, small):** write **settlement before usage**. Settlement is the
  durable, hash-chained money record; usage is a quota cache. Writing settlement
  first means a crash can only leave settlement-without-usage (money recorded,
  quota counter slightly behind) rather than usage-without-settlement (quota
  consumed, money record lost). The latter is the worse direction.
- **Next:** add prompt/completion totals to the settlement per-account API, seed
  `Market` usage from settlement at startup, and treat usage as a projection.

Option A remains the long-term "true atomicity" answer if the project later
needs it; B gets correctness-of-record (you never lose a settlement event to an
ordering crash) at a fraction of the risk.

## Test plan

- Crash-injection unit test: fail the usage write after a successful settlement
  write; assert the settlement event is intact and that a restart re-derives the
  matching usage (option B).
- Property: for any sequence of completions, rebuilt usage equals the running
  totals (no drift).
- Ordering: a settlement write failure must not leave usage incremented.
- Existing `go test -race ./...` must stay green.

## Scope note

This is a deliberate, reviewable plan. Implementing it changes billing
semantics (usage derived from settlement) and adds a settlement schema field, so
it should land as its own reviewed change with the tests above — not folded into
unrelated work.
