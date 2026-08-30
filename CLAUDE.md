# Tape

## Vision

**Tape** is a trading copilot for the terminal. Every morning before the US open it reads the
market (movers, news, calendars, the watchlist), applies a written strategy playbook, and proposes
zero to three trades with entry, stop, target, and size. The human takes or passes on each one.
Everything is journaled — including the passes and the AI's calls that turned out wrong — scored
nightly against what actually happened, and digested weekly into diffs against the playbook. The
playbook constrains the next morning. Strategy changes only through the scored record.

The honest premise, and the reason the repo exists: retail day traders lose. Roughly 97% of
persistent ones in the best population studies. Nobody has shown an LLM picking intraday trades
changes that number; the de-biased long-horizon tests say it doesn't. So Tape is **not** an oracle.
It is a disciplined analyst you argue with, and a mirror that doesn't flatter. It runs on paper
money until a quantitative gate opens (three-plus months, positive expectancy after modeled costs,
profit factor ≥ 1.3, drawdown ≤ 10%), and if that gate never opens the project still succeeded —
the verdict was the point, and it cost API fees instead of a stake.

The bar: a tool Matthew opens at 6:52 AM Mountain time because it is genuinely useful, and stats
he trusts because they are computed from Tape's own cost-modeled ledger, never from a paper
broker's fantasy fills. In one line: **the journal is the product; the AI applies rules and gets
graded.**

## The Prime Directive: Venue / Record / Brain never bleed

Three layers, each ignorant of the other two, wired by a thin CLI and one orchestration package:

1. **Venue** (`internal/broker/`, `internal/broker/alpaca/`) — executes orders and streams
   quotes. Knows nothing about strategy, stats, or the LLM. Paper and live are the same code path
   behind the `Broker` interface with a different adapter; the live adapter does not exist until
   the gate opens.
2. **Record** (`internal/journal/`, `internal/costs/`) — the SQLite journal is the **only** source
   of stats. Every order, fill, proposal, decision, briefing call, and score lives here. Every fill
   passes through the cost model on the way in. The ledger starts at `starting_equity` from config;
   the broker's balance is never read for any number a user sees.
3. **Brain** (`internal/llm/`, the playbook) — model-agnostic. Produces schema-validated JSON,
   cites the playbook rule behind every proposal, and never touches an order. It has no code path
   to place, size beyond, or override anything.

`internal/trading/` orchestrates the three and is where the **Go-enforced rules** live.
`internal/cli/` is wiring: parse flags, call one thing, print the result.

If a feature needs a layer to reach into another (a broker that computes P&L, a stat that reads
Alpaca's equity, a prompt that "handles" risk), the design has a gap — STOP and surface it, don't
hack across the boundary.

## Architecture Rules

### Frozen Contracts
- `internal/broker/broker.go`, `internal/journal/types.go`, `internal/llm/llm.go`, and
  `internal/config/config.go` are contracts that parallel agents code against. **Additive changes
  only** (a new field, a new method). Renaming or removing anything: STOP and surface first.
- `journal/types.go` fields are exactly what the stats compute from. A new kind of record
  (proposal, decision, score) is a new migration entry and a new type — never an overloaded `Note`.

### The Record Is the Truth
- Stats commands (`pos`, `eod`, `stats`, later `retro`) compute from the journal, then decorate
  with live broker data (current price) — never the reverse. `Ledger.RealizedPL` is already net
  of costs; do not subtract `Commissions`/`Fees` a second time.
- No fill row without a modeled price: every raw venue price goes through `costs.Model.Apply`.
  The default model is IBKR Pro fixed pricing plus regulatory fees, because that is what a small
  real account would pay.
- Paper and live records share one file, separated by `Mode`. Every query passes a mode; empty
  mode ("both") is an escape hatch for tooling, never a command default.
- Migrations are an append-only slice in `internal/journal/migrate.go`. Never edit a shipped
  migration; add the next one.
- Timestamps are stored UTC in the fixed-width layout the store defines (sortable inside SQLite).
  Day boundaries — "today's recap", "flat by close" — use the configured `account.timezone`
  (Matthew trades US markets from Mountain time); never `time.Now()` bare in a stats path.
- **Every proposal is journaled whether or not it is taken.** Passes carry the human's reason
  and get counterfactual-scored. This is the detail that makes the learning loop honest.

### Rules Live in Go, Not in Prompts
- Guardrails are code in `internal/trading/` that runs before any order reaches a venue. Phase 0:
  no shorting, no overspend against the ledger. Phase 2 adds the rest: per-trade risk cap
  (0.5–1% of ledger equity), max open positions, no averaging down, flat by close, halt after two
  stopped-out losses in a day. Each refusal names the rule and the numbers.
- The model never sizes past the cap, never chooses to skip a stop, never "decides" a guardrail
  doesn't apply. If a prompt contains the words "you may override", it is wrong.
- Proposals are schema-validated (`llm.Request.JSONSchema`) and range-checked in Go (stop on the
  correct side of entry, size within cap, symbol on the watchlist). A proposal that fails
  validation is logged as rejected-by-schema, not silently dropped, not retried into compliance.

### The Brain Is Replaceable
- One `llm.Provider` interface; native Anthropic via the official SDK, everything else through
  the single OpenAI-compatible client with presets (`openrouter`, `zai`, `deepseek`, `openai`,
  `groq`, `ollama`, custom base URL). Adding a vendor is a preset row, not a client.
- Prompts ask the model to **apply** the playbook to today's data and cite the rule, and to make
  **falsifiable** calls (direction of the day, thesis, invalidation level) that get scored. Prompts
  never ask "will this go up" as a product feature — predictive value, if any, shows up in the
  scored record.
- Model output is text or JSON, never trusted for arithmetic. Size, risk dollars, and R multiples
  are recomputed in Go from the proposal's prices.
- No secrets in prompts, logs, or the journal. Keys come from env (`ALPACA_API_KEY`,
  `ALPACA_API_SECRET`, the preset's key env) or `config.toml`; never from source.

### Paper Is Not Real, and the Code Says So
- `tape init` sets the ledger to a fundable size ($5,000 default). Alpaca's $100k paper balance is
  labelled "ignored by stats" anywhere it is shown.
- Paper fills are frictionless; the cost model is the honesty layer, and live will still be worse.
  Nothing in the codebase may describe paper results as what the user "would have made".
- `tape mode live` refuses until the gate exists. The refusal message names the gate. There is no
  flag, env var, or config value that bypasses it.

### User-Facing Output
- First line of every command output is the mode prefix, `[paper]` or `[LIVE]`.
- Tables via `text/tabwriter`; money formatted by the one helper in `internal/cli/style.go`
  (`$1,234.56`, leading `-`, `tabular` alignment); colour only when stdout is a TTY and
  `NO_COLOR` is unset. No colour library.
- Errors go to stderr with context (`tape: buy NVDA 16: ledger cash $1,204.10 < cost $2,054.40
  (rule: no overspend)`) and a non-zero exit. Never a bare `error`.
- Copy is plain and direct. Explain the product-specific thing, never universal computer skills.

### Files, Size, Dependencies
- Soft cap 250 lines per file, hard cap 400. One reason to change per file.
- Stack is locked: Go 1.26, cobra, go-toml/v2, `alpaca-trade-api-go/v3`, `modernc.org/sqlite`
  (pure Go — the binary stays cgo-free, `:memory:` with WAL is a trap, tests use temp files),
  `anthropic-sdk-go`, `net/http` for the OpenAI-compatible client. No new dependency without
  explicit approval; `bubbletea` is pre-approved for `tape watch` when the plain renderer stops
  being enough.
- Python is allowed **backstage only**: a sidecar invoked by `tape backtest` for research. No
  daily command may depend on Python being installed.

## Commands

- `make build` — `bin/tape` with the git describe baked into `tape version`.
- `make test` / `go test -race ./...` — all packages, no network, no keys needed.
- `make vet` / `make lint` — vet + gofmt check. `make lint test` must pass before any chunk is
  called done.
- Smoke without keys: `TAPE_HOME=$(mktemp -d) ./bin/tape init && ./bin/tape mode && ./bin/tape
  llm providers`. With free Alpaca paper keys exported: `tape status`, `tape buy SPY 1`,
  `tape pos`, `tape eod`.

## Layout

```
cmd/tape/            main.go — calls cli.Execute
internal/cli/        cobra commands, one file each; style.go owns formatting
internal/config/     ~/.tape/config.toml ($TAPE_HOME), env overrides, Default()
internal/broker/     Broker + MarketData contracts; alpaca/ is the paper adapter
internal/journal/    SQLite store, migrations, FIFO trade matching, ledger and recaps
internal/costs/      slippage + commission + fee model applied to every fill
internal/llm/        Provider contract, anthropic.go, openai.go (compatible client), presets
internal/trading/    orchestration + the Go-enforced rules (Submit, Sync, Flatten, Positions)
docs/DESIGN.md       the public design document: evidence, decisions, phases, the gate
docs/models.md       model recommendations per provider
```

Phase status lives in `docs/DESIGN.md` § Phases. Matthew's private working plan (research detail,
session status) is outside the repo at `~/.claude/plans/tape.md`; read it if you have it.

## Testing Strategy

The stats are what the whole project is for — test the math hard, eyeball the tables.
- **Never hit the network in tests.** Venues and LLM endpoints are `httptest` servers asserting
  request shape; trading uses the in-memory fake broker; the journal uses a temp-file SQLite.
- **Must-have coverage**: cost model table (min/max commission, sell-only fees, zero model);
  FIFO matching (full, partial, multi-lot, oversell); ledger cash and net-of-costs P&L; day
  recap across the Mountain/UTC boundary; mode separation; fill idempotency; every guardrail's
  refusal path; status mapping for every venue status; JSON extraction from fenced/prosy output.
- **Do NOT test** real Alpaca, real model endpoints, or terminal rendering. Those are the human
  smoke checks above; write down what you ran and what you saw in your report.
- `go test -race ./...` green, `gofmt -l .` empty, `go vet ./...` clean, before done.

## Development Workflow

### Session Start Protocol
1. Read this file, then `docs/DESIGN.md`. If you are dispatched for a phase or chunk, read only
   that phase's section closely.
2. `git status` and `git log --oneline -10` to see where things stand.
3. Implement ONLY the chunk you were dispatched for, in the packages you were given — other
   chunks may be in flight in parallel. Do not run `go mod tidy` unless you are the only Go agent.
4. If your chunk needs a change to a frozen contract, STOP and surface it before editing.

### Before Building
- Read the target package fully and the contract it implements. Read the SDK source in the module
  cache before calling it; do not guess API names.
- Check `internal/cli/style.go` and the existing commands before formatting anything new.

### After Building
- Run the verification for your packages, then the full `make lint test`.
- Report what you verified and, explicitly, what you could not (no keys, no network).
- Never commit. Matthew reads diffs and asks for commits himself.

## What NOT To Do
- Do NOT place, modify, or cancel an order without an explicit human command (`buy`, `sell`,
  `take`, `eod`). No autonomous trading loop, no "auto-take high-confidence" flag. Autonomy is
  earned later, one guardrail at a time, by Matthew's decision.
- Do NOT construct a live broker client, add a live adapter, or unlock `mode live` before the gate
  exists. No backdoor.
- Do NOT compute any user-facing number from broker balances. The ledger is the account.
- Do NOT insert a fill that skipped the cost model, or describe paper P&L as real.
- Do NOT let the model size, skip stops, or override a guardrail. Do NOT do arithmetic in prompts.
- Do NOT ask the model to predict prices as a feature. Score its calls; don't sell them.
- Do NOT drop a proposal, pass, or failed validation on the floor — journal it.
- Do NOT add options, crypto, shorting, or fractional shares in v1.
- Do NOT add dependencies, cgo, or a Python requirement to a daily command.
- Do NOT put API keys, account IDs, or Matthew's personal finances anywhere in the repo — it is
  public.
- Do NOT edit a shipped migration or a frozen contract without stopping first.
- Do NOT start a second chunk before the current one passes `make lint test` and its smoke check.
- Do NOT commit.
