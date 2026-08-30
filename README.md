# Tape

A trading copilot for the terminal. It reads the market every morning, proposes trades you confirm
or veto, journals every decision, and grades itself against what actually happened.

The honest premise first, because everything else follows from it: retail day traders lose. In the
best population studies — every trade on the Taiwan Stock Exchange over fourteen years, and 19,646
new Brazilian futures traders followed session by session — under 1% were predictably profitable
and 97% of the persistent ones lost money. Nobody has shown that an LLM picking trades changes
that number, and the de-biased long-horizon tests say it doesn't. So tape is not an oracle. It is
a disciplined analyst you argue with, and a mirror that doesn't flatter. It runs on paper money
until a quantitative gate opens, and if that gate never opens the project still did its job.

The bet behind the repo: **an assistant that never predicts, only applies written rules and grades
itself, is worth more than one that claims to know where the market is going.** The second kind has
no evidence behind it. The first produces the one thing a beginner cannot get any other way — an
honest record.

Phase 0 ships today: config, the Alpaca paper adapter, the SQLite journal, the cost model, the
provider-agnostic model layer, and manual orders end to end, behind 135 tests that never touch the
network. There is no briefing yet.

Note: tape has no live-broker code path. `tape mode live` is refused, and no flag, environment
variable, or config value changes that.

---

## Why I built it

I wanted to find out whether I could trade, and I wanted the answer to be a number rather than a
feeling. The obvious way to get there is to point a good model at the market every morning and ask
what to buy — treat the AI as the best information collector I could afford. That was roughly my
premise going in. It stops working the moment you ask what you would do with the answer:

- If it says buy NVDA and NVDA goes up, was the model right, or was the whole market up that day?
- When a trade loses, was it the thesis, the entry, the size, or the fill?
- On a small account, how much of the move does a $1.00 minimum commission eat before I ever see
  a P&L?
- Did I follow the plan I wrote down, or improvise and then remember the improvisation as the plan?
- When I decline a suggestion, am I saving myself money or costing myself money?

Not one of those is answerable by a better model. Every one of them is answerable by a record —
provided the record includes the trades you didn't take, and prices the ones you did the way a real
broker would. So I inverted it: **the journal is the product, and the model is a component that
gets graded like everything else in it.**

That inversion is what makes an LLM safe to have here at all. It is not being asked for an edge it
has no demonstrated claim to. It is being asked to apply rules I wrote, cite which rule it applied,
and make calls specific enough to be scored wrong.

## Influences

**Gary Stevenson's _The Trading Game_ — the origin, and not the lesson I went in for.** I read it
and came away thinking trading was an information problem, which is what sent me looking for a tool
that collects information faster. The research killed that idea: retail cannot win on speed, since
firms hold co-located servers and, through
[payment for order flow](https://en.wikipedia.org/wiki/Payment_for_order_flow), wholesalers see
retail orders before the market does. What survives from the book is duller and better. Stevenson's
edge was a macro thesis nobody believed, held with patience and sized with conviction. The
transferable part is not "get information faster", it is **know exactly why you are in a trade and
what would make you wrong** — which is a journaling discipline, and the thing tape enforces.

**The Taiwan and Brazil day-trader studies — the numbers that set the bar.** Barber, Lee, Liu and
Odean had every trade on the Taiwan Stock Exchange from 1992 to 2006; Chague, De-Losso and
Giovannetti followed 19,646 new Brazilian futures day traders and found that of those who lasted
300 or more sessions, 97% lost money and 1.1% earned more than minimum wage. Persistence made
outcomes worse.
[Summary with figures](https://www.currentmarketvaluation.com/posts/the-data-on-day-trading.php).
This is why paper mode is not a beginner ramp — it is the experiment, and "you have no edge" is a
valid result the design has to be able to produce.

**FINSABER, TradingAgents and TradeTrap — what the LLM trading literature actually shows.**
[TradingAgents](https://arxiv.org/abs/2412.20138) reports a large edge from a multi-agent setup,
over three months, on a handful of tech stocks the models were pretrained on the news for.
[FINSABER](https://arxiv.org/abs/2505.07078) re-ran LLM timing strategies across two decades and
over a hundred symbols with survivorship and data-snooping controls, and the advantage evaporated —
too conservative in bull markets, too aggressive in bear markets.
[TradeTrap](https://arxiv.org/abs/2512.02261) stress-tests the agents, and when six frontier models
were handed real money on crypto perpetuals ([Alpha Arena](https://nof1.ai/)) most drew down hard by
over-leveraging. The models differed far more in position sizing than in direction. That is the
whole argument for a human confirming size and for risk limits living in compiled code.

**Tradervue and TraderSync — the category that already works.** Trade journaling and analytics is
something traders pay real money for, every month, and it contains no prediction at all. When the
proven product in a space is the record and the unproven one is the signal, build the record.
[Tradervue](https://www.tradervue.com/) · [TraderSync](https://tradersync.com/)

**OpenBB — the warning about vendor wrappers.** The best open-source research terminal nearly died
maintaining hundreds of data-vendor integrations before
[sunsetting the terminal](https://openbb.co/blog/sunsetting-openbb-terminal-why-how-and-what-now/).
Tape gets few providers, each behind a Go interface, so replacing a dead vendor is one adapter file
and not a project.

**Shadow, then score, then gate — the deployment habit.** I have shipped a forecaster this way
before: run it in production predicting nothing anyone can see, score its predictions against what
actually happened, and only then decide whether a user should ever be shown the number. It is
slower and it is the only way I know to tell a working system from a plausible one. Tape is that
shape end to end. Paper is the shadow, the nightly scoring is the scoring, and the gate is the
decision — with the gate criteria written down in advance, where I cannot move them later.

## How it works

Two loops. The second one is why this is more than a chat wrapper.

```
      market data + playbook.md
                 │
                 ▼
               model
                 │
                 ▼
      briefing + 0-3 proposals         each one cites a playbook rule
                 │
                 ▼
         you take or pass              taken orders go to the broker
                 │
                 ▼
              journal                  every proposal, taken or passed, with its reason
                 │
                 ▼
          nightly scoring              P&L for what you took, counterfactuals for what
                 │                     you passed, right or wrong for the call of the day
                 ▼
           weekly retro                proposes diffs to playbook.md
                 │
                 ▼
            you approve  ───────────►  playbook.md constrains tomorrow's briefing
```

**Morning.** Quotes, movers, news and the calendars come in. The model produces a briefing — market
state, regime, what the calendar is about to do, and a falsifiable call of the day — then proposes
zero to three trades, each tied to a named rule in the playbook and each carrying its stop, target
and size before it carries anything else. You take or pass. Taken orders go to the broker.
Everything goes into the journal.

**Learning.** Nightly, the journal is scored against what actually happened. Weekly, the model reads
its own scored record and proposes diffs to `playbook.md`, which you review like a pull request.
That edge — playbook to briefing — is the only way strategy changes. The model does not improvise.

The detail most journals miss is the pass side. Every proposal is journaled whether or not it is
taken, with the reason it was declined, and the ones you declined get counterfactually scored. Over
months that answers a question no amount of reading will: are your vetoes helping or hurting?

### What the model is not allowed to touch

Four rules, and none of them are stylistic.

**It never places an order.** `internal/llm` imports neither `internal/broker` nor
`internal/trading`, so there is no code path from a completion to a venue. The only thing that
transmits is a command you typed.

**It never does the arithmetic that matters.** Size, risk dollars and R multiples are recomputed in
Go from the proposal's own prices. A model that can produce a number in its output can produce a
wrong one, and a schema-valid JSON proposal with a fabricated stop-loss looks exactly like a good
one — which is the failure mode the financial-document benchmarks in
[docs/models.md](docs/models.md) are chosen to detect.

**It never argues with a guardrail.** Limits are functions in `internal/trading` that run before an
order leaves the machine. There is no prompt sentence that turns one off, because prompts are
suggestions and a function call is a wall. If a prompt in this repo ever contains the words "you
may override", it is a bug.

**It never sees a secret.** Keys resolve from the environment at the edge of the process and go into
no prompt, no log, and no journal row.

Phases 1 and 2 build the proposal path; those rules are the constraints it is being built against.
The first and fourth are already structural today.

## Why it's built this way

Each of these is a decision with a consequence, in the order they were made.

**The journal is the source of truth, not the broker.** Every stat comes out of SQLite and is then
decorated with live broker data, never the reverse. `Ledger` starts at the `starting_equity` in
config and computes cash and P&L from tape's own fills. Alpaca hands out $100,000 of paper money,
which tells you nothing about how you would behave with an account you could actually fund, so that
balance is printed once under a heading that says `(ignored by stats)` and used for nothing. The
consequence: the numbers you judge yourself on survive changing brokers, and they are the numbers
for the account size you configured.

It runs the other way at the close too. `eod` sells the quantity the *ledger* holds, symbol by
symbol, never the venue's. Where the two disagree it says so and refuses to trade the difference —
a broker holding shares tape never recorded is a reconciliation problem for you, not a position for
tape to liquidate on your behalf.

**Every fill is re-priced before it lands.** The package comment in `internal/costs` is the whole
argument: *paper venues fill at the quote with no commission; a small account trading through IBKR
does not, and the gap is large enough to flip a strategy's sign.* So slippage moves the price
against you in basis points, commission is per-share with a minimum and a percent-of-value cap
mirroring IBKR Pro fixed pricing, and the SEC fee and FINRA TAF land on sells. The broker's raw
price is kept; the modeled price is what the stats use. On the ten-share round trip further down,
the paper venue's answer is $100.00 and tape's is $96.92 — 3% of the move gone on a winner, and all
of it on a scratch.

**Guardrails are Go, not prompt text.** Phase 0 refuses an invalid order, a sell larger than the
ledger holds, and a buy the ledger cannot pay for. Each refusal names the rule and quotes its
numbers, because a refusal the trader cannot verify is a refusal they will work around:

```
tape: buy 16 NVDA: ledger cash $500.00 < cost $2,056.59 for 16 NVDA at $128.41 (rule: no overspend)
tape: sell 99 AAPL: selling 99 AAPL but the ledger holds 10 (rule: no shorting)
```

Three details in there are the difference between a rule and a rule that works. The cost quoted is
the *modeled* cost — the same slippage, commission and fees the fill would be journaled with — so
the check refuses the order that would actually overdraw the ledger, not the one that looks
affordable at the quote. Cash and shares that resting orders have already claimed are subtracted
before the comparison, so two orders cannot spend the same dollar or sell the same share:

```
tape: buy 15 AAPL: ledger cash $5,000.00 less $3,602.80 committed to open orders leaves $1,397.20 < cost $1,501.90 for 15 AAPL at $100.01 (rule: no overspend)
```

And `buy` and `sell` reconcile the journal against the venue before the guardrails read it, so a
stop that fired since your last command is seen. The cost of that: a venue outage refuses new
orders rather than trading on a stale record, which is the right way round.

The reservation rule has a sharp edge worth knowing before it surprises you. A bracket's stop and
target are both open sells, and each reserves the full position, so a ten-share bracketed long
reserves twenty shares and a manual `tape sell AAPL 10` is refused:

```
tape: sell 10 AAPL: selling 10 AAPL but the ledger holds 10 with 20 already committed to open sells, leaving -10 (rule: no shorting)
```

Only one leg can ever trade, so counting both is stricter than reality — and strict is the correct
direction for a rule whose failure mode is an accidental short. Cancel the bracket to free the
shares: `eod` does it, or the broker's UI, because there is no `tape cancel` yet.

**The brain is replaceable.** `llm.Provider` is one `Complete` call taking a system prompt, messages
and an optional JSON schema. Anthropic is native through the official SDK, the local Claude Code CLI
is a third implementation, and everything else is one OpenAI-compatible client with a preset base
URL — so adding a vendor is a row in a table rather than a client. The reason is the learning loop:
if every briefing call is scored, swapping the model and comparing the scores is an experiment
instead of an opinion.

**Go for the tool, Python backstage only.** Go gives a single cgo-free binary, a maintained official
Alpaca SDK, and the concurrency a quote stream wants. What Go does not have is a research stack —
no pandas, no backtrader-class portfolio backtester with walk-forward and realistic fills — and
every piece of prior art in this space is Python. So a sidecar script is permitted for research, and
no daily command may depend on Python being installed.

**One SQLite file.** Briefings, proposals, decisions, fills and scores all live in
`~/.tape/tape.db`: trivial to back up, trivial to query, trivial to hand to that sidecar. Pure-Go
`modernc.org/sqlite`, so the binary has no cgo.

**The broker is an interface.** `broker.Broker` covers account, positions, orders and flattening;
`broker.MarketData` covers quotes and streams. Alpaca paper is one adapter, and IBKR live would be
another behind the same seam with no change to the CLI. That seam is the entire reason "paper now,
real money later" is a path rather than a rewrite.

**Live mode is locked, and the lock is not a setting.** `tape mode live` returns an error naming the
gate. There is no flag or environment variable that opens it and no live adapter to open it onto;
building one is a deliberate future commit, not a configuration mistake somebody makes at 6am.

## What Tape is not

**Not an auto-trader.** There is no unattended loop and no auto-take flag. Every order comes from a
command you ran. Autonomy is something the record can earn later, one guardrail at a time.

**Not a signal service.** It is not going to tell you what will go up. The call of the day exists as
a scored experiment; if it turns out to have predictive value the stats will say so, and selling
that claim before the stats exist would mean building on the one thing the evidence rejects.

**Not financial advice.** It is an engineering plan for measuring something. See the disclaimer.

**No options, crypto, shorting, or fractional shares in v1.** Options are where beginners lose
fastest and they multiply every mistake. Crypto has thin qualitative information and never closes,
which fights a morning routine. Shorting is one more way to be wrong while learning. All of them fit
behind the same interfaces later.

**Not a backtester, yet.** Phase 3 brings scoring against real history; a walk-forward backtest is
the Python sidecar's job, and neither exists today.

**Not a data terminal.** Few providers, well-tested and replaceable, is the entire ambition — see
the OpenBB influence above.

## Disclaimer

Tape is a research tool, not investment advice. It reads public market data and news, asks a
language model what it makes of them, and writes down the answer. The model is often wrong, the data
is sometimes wrong, and neither of them knows anything about your finances.

Run it in paper mode until you have enough entries in the journal to judge it. That is the whole
reason the journal exists — a proposal you did not record is a proposal you cannot grade — and it is
why real money sits behind a gate with the criteria fixed in advance.

Read the source before you trade on anything it says. It is about 5,700 lines of Go, and the two
parts that decide whether the numbers mean anything are small enough to read in a sitting:
`internal/costs` is 121 lines and `internal/trading/rules.go` is 135.

## Install

Requires Go 1.26 or later.

```sh
go install github.com/trapp01/tape/cmd/tape@latest
```

Or from a clone, which bakes `git describe` into `tape version`:

```sh
make build      # ./bin/tape
make lint test  # vet + gofmt check + the full suite, no network and no keys needed
```

## Quick start

```console
$ tape init

[paper] init

config   ~/.tape/config.toml
journal  ~/.tape/tape.db
timezone America/Edmonton

Next steps
  1. Get free Alpaca paper keys at https://app.alpaca.markets
  2. export ALPACA_API_KEY=...
     export ALPACA_API_SECRET=...
  3. export ANTHROPIC_API_KEY=...   (llm provider anthropic)
  4. tape status

tape's ledger starts at $5,000.00 regardless of Alpaca's paper balance.
```

Alpaca paper keys are free and take a couple of minutes. Setting `$TAPE_HOME` moves the config and
the journal somewhere other than `~/.tape`, which is how the test suite keeps runs isolated. Then:

```sh
export ALPACA_API_KEY=... ALPACA_API_SECRET=...
export ANTHROPIC_API_KEY=...      # or whichever provider you configured

tape status          # ledger, broker balance, market clock
tape buy SPY 1       # market order, journaled and cost-modeled
tape pos             # open positions from the journal, priced live
tape eod             # flatten everything and recap the day
```

The blocks below are real output, rendered through the in-memory venue the tests use rather than a
live Alpaca account — see [Status](#status). Every number in them comes from the same code path a
real fill takes.

```console
$ tape buy AAPL 10 --note "range break"

[paper] buy 10 AAPL

  journal id  #1
  order       buy 10 AAPL market
  status      filled
  broker id   fake-1

Fills
QTY  RAW      MODELED  COMMISSION  FEES   COST
10   $100.00  $100.05  $1.00       $0.00  $1,001.50
```

`RAW` is what the venue reported. `MODELED` is that price plus five basis points of slippage against
you. The $1.00 is the commission minimum: at half a cent a share the ten-share order earns five
cents of commission, and the floor charges twenty times that. Small orders are where the floor
hurts, and it is worth seeing on the first one.

```console
$ tape pos

[paper] positions

SYMBOL  QTY  AVG ENTRY  CURRENT  COST BASIS  MARKET VALUE  UNREALIZED
AAPL    10   $100.05    $110.00  $1,000.50   $1,100.00     +$99.50 (+9.95%)

  unrealized total  +$99.50

$ tape eod

[paper] end of day

Flatten
  orders cancelled  0
  positions closed  1
  fills recorded    1
  closed sell 10 AAPL (journal #2, filled)

Recap 2026-08-30
  orders                  2
  trades closed           1
  wins / losses           1 / 0
  gross                   +$98.95
  costs on closed trades  $2.03
  costs on today's fills  $2.03
  net                     +$96.92

flat.
```

The stock moved from $100 to $110 and the paper venue's arithmetic says $100.00. Tape says $96.92.
That difference is the entire reason `internal/costs` was the first package written.

Two cost lines, because they answer different questions. `costs on closed trades` is what the day's
round trips paid, and it is the number `net` is computed from. `costs on today's fills` is every
commission and fee the day incurred, including on positions still open. After a clean `eod` they
agree; when they don't, something you opened today is still costing you and hasn't been graded yet.

The same fill table appears on a sell, with `NET` in place of `COST` — a buy pays the commission and
fees on top of the shares, a sell has them taken out of the proceeds, and they are not the same
quantity:

```console
$ tape sell AAPL 10

[paper] sell 10 AAPL

  journal id  #2
  order       sell 10 AAPL market
  status      filled
  broker id   fake-2

Fills
QTY  RAW      MODELED  COMMISSION  FEES   NET
10   $110.00  $109.95  $1.00       $0.03  $1,098.42
```

`FEES` is $0.03 here and $0.00 on the buy: the SEC fee and the FINRA TAF fall on sells only. Note
the slippage direction too — the buy was modeled above the quote, this sell below it. Both against
you.

## Configuration

`~/.tape/config.toml`, written by `tape init` and edited by hand. Secrets belong in the environment;
`ALPACA_API_KEY` and `ALPACA_API_SECRET` override whatever is in the file, and `tape mode paper`
will not write an env-supplied key back into it.

```toml
mode = 'paper'            # 'live' is refused until the gate opens

[account]
starting_equity = 5000.0  # tape's ledger, and the basis of every stat. Not Alpaca's balance.
timezone = 'America/Edmonton'   # where day boundaries are measured; blank means the machine's zone

[broker]
name = 'alpaca'

[broker.alpaca]
api_key = ''              # prefer $ALPACA_API_KEY
api_secret = ''           # prefer $ALPACA_API_SECRET
data_feed = 'iex'         # 'iex' is free; 'sip' needs a paid Alpaca data plan

[costs]                   # defaults mirror IBKR Pro fixed pricing
slippage_bps = 5.0        # moved against you on every fill
commission_per_share = 0.005
commission_min = 1.0      # the floor that dominates small orders
commission_max_pct = 1.0  # percent of trade value; outranks the floor on cheap stocks

[llm]
provider = 'anthropic'
model = 'claude-opus-5'
base_url = ''             # required for 'openai-compatible'; overrides the preset for the rest
api_key = ''              # prefer the provider's key env var
```

The SEC fee and the FINRA TAF are not in the file. They are not yours to choose, so they live in
`costs.Default()` and apply to every sell regardless of what the config says.

Setting `slippage_bps = 0` and `commission_min = 0` will make paper look better. It will also make
every number tape produces a lie, which is the one thing this repo is built not to do.

## Providers

```console
$ tape llm providers

[paper] llm providers

NAME               BASE URL                        KEY ENV             DEFAULT MODEL  DOCS
anthropic          https://api.anthropic.com       ANTHROPIC_API_KEY   claude-opus-5  https://platform.claude.com/docs/en/api/messages
claude-code        -                               -                   opus           https://code.claude.com/docs/en/headless.md (your own Claude Code login, personal use only)
openrouter         https://openrouter.ai/api/v1    OPENROUTER_API_KEY  -              https://openrouter.ai/docs/quickstart
zai                https://api.z.ai/api/paas/v4    ZAI_API_KEY         -              https://docs.z.ai/guides/overview/quick-start
deepseek           https://api.deepseek.com        DEEPSEEK_API_KEY    deepseek-chat  https://api-docs.deepseek.com/
openai             https://api.openai.com/v1       OPENAI_API_KEY      -              https://platform.openai.com/docs/api-reference/chat
groq               https://api.groq.com/openai/v1  GROQ_API_KEY        -              https://console.groq.com/docs/openai
ollama             http://localhost:11434/v1       -                   -              https://docs.ollama.com/api/openai-compatibility
openai-compatible  -                               TAPE_LLM_API_KEY    -              https://platform.openai.com/docs/api-reference/chat
```

A provider with no default model needs `llm.model` set; `tape init` says so when you pick one.
Anything that speaks chat-completions and is not in the table — Google's OpenAI-compatible endpoint,
for instance — works as `openai-compatible` with `base_url` set, or through `openrouter`.
`tape llm ping` checks that the configured one answers before you rely on it at 6:52am.

**About `claude-code`.** It is not an API provider. It shells out to the Claude Code CLI on your own
machine (`claude -p`) and runs on your Claude subscription rather than API credits, which makes it
the cheapest way to develop against a strong model. That is personal use on your own machine only:
Anthropic does not permit offering a claude.ai login to third parties, so a hosted or distributed
tape has to use API keys. [Headless mode](https://code.claude.com/docs/en/headless.md), and
[docs/models.md](docs/models.md#the-claude-code-provider) for the flags and the caveats.

Which model to actually run is its own document. [docs/models.md](docs/models.md) ranks twenty of
them against this specific workload — one call a morning, 30–80k tokens of messy text in, strict
JSON out — with prices, long-context benchmark scores, latency budgets and structured-output
guarantees. The headline is Claude Opus 5 for quality, Gemini 3.7 Flash for value, and GLM-5.3-Flash
for almost nothing at all — and the spread between the best and the cheapest sane option is about
$93 a year. At one call a day, optimise for the briefing being right, not for the bill.

## Commands

```
tape init            write config.toml and create the journal
tape status          ledger, broker balance (labelled ignored), and the market clock
tape buy SYM QTY     buy, with --limit, --stop, --target, --note
tape sell SYM QTY    sell shares the ledger holds, with --limit and --note
tape pos             open positions from the journal, priced live
tape orders          journaled orders, with --open and --since
tape watch SYM...    stream live quotes until Ctrl-C
tape eod             flatten everything and recap the day
tape mode [paper]    show or set the mode; `live` is refused
tape llm ping        check the configured provider answers
tape llm providers   list the known providers
tape version         version, platform, and Go toolchain
```

Every command prints its mode on the first line, so no output is ever ambiguous about which account
it was talking about. That tag is `[paper]`, or `[LIVE — locked]` if you hand-edit `mode = "live"`
into the config: the file can say live, and the banner will still tell you tape is not allowed to
trade it. There is no bare `[LIVE]` anywhere in the binary. `--config` points at a config file
anywhere.

There is no `tape cancel` yet. `eod` cancels resting orders on its way to flat, and anything else
goes through the broker's own UI.

## Roadmap

**Phase 0 — plumbing.** Config, broker adapter, journal, cost model, model layer, manual paper
orders end to end. Done.

**Phase 1 — the briefing.** Data collectors behind provider interfaces, and `tape brief` with a
scored call of the day, before any trade proposals exist.

**Phase 2 — co-pilot.** A first playbook, schema-validated proposals, take and pass with reasons,
and the rest of the Go-enforced guardrails: per-trade risk cap, max open positions, no averaging
down, flat by close, halt after two stopped-out losses in a day.

**Phase 3 — the mirror.** Nightly scoring including counterfactuals, `tape stats`, weekly
`tape retro` with playbook diffs. Then the actual experiment: three or more months of real mornings
on paper.

**The gate.** No real money moves until every box ticks, and the boxes are written down now so they
cannot be softened later:

- Three or more months and fifty or more sessions through the full system
- Positive expectancy after modeled slippage and commissions
- Profit factor of 1.3 or better, maximum drawdown of 10% or less
- Zero guardrail breaches in the final month
- The stats identify *which* playbook rules carry the edge. Funding a mystery is gambling.

**Phase 4 — real money**, only if the gate opens. A live adapter behind the existing interface, a
small account, the same guardrails and the same journal. Live results will be worse than paper; the
gate's margins exist partly to absorb that, and the gate can close again.

If the gate never opens, phase four never runs, and the project still did its job. The full
reasoning, the evidence behind each decision, and what I cut are in
[docs/DESIGN.md](docs/DESIGN.md).

## Status

Early, and honest about it. Phase 0 is complete and tested: config and env overrides, the Alpaca
paper adapter, the SQLite journal with FIFO trade matching and day recaps, the cost model, the
provider-agnostic model layer with its three clients — native Anthropic, OpenAI-compatible, and
Claude Code — the trading engine with its Phase 0 guardrails, and eleven commands. 135 tests, none
of which touch the network: venues and model endpoints are `httptest` servers asserting request
shape, trading runs against an in-memory fake broker, and the journal uses a temp-file SQLite.

Not done: **tape has not yet been run against a real Alpaca paper account.** Everything above the
adapter is verified; the adapter itself is tested against a fake HTTP server and nothing more. There
is no briefing, no playbook, no proposals, no scoring, and no stats command. There is no `tape
cancel`, so a bracket you want out of has to go through `eod` or the broker. The journal schema will
change before any of those land.

I'm open-sourcing it at this stage because the interesting part is not the trading. It is that the
honest version of this tool — cost-modeled fills, a ledger that ignores the broker, limits in
compiled code, and a gate whose criteria were fixed before any results existed — is a specific and
mostly unglamorous set of engineering decisions, and they are all readable in one afternoon.

## License

MIT — see [LICENSE](LICENSE).
