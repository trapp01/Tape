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

Phases 0 through 3 ship today: config, the Alpaca paper adapter, the SQLite journal, the cost model,
the provider-agnostic model layer, manual orders end to end, the morning briefing, the co-pilot, and
the mirror. `tape brief` reads the market, applies a playbook you wrote, files one falsifiable call
and a bias on each watchlist symbol, and puts zero to three trade ideas on a slate you take or pass
one at a time. `tape score` grades the call and the biases, and replays every decided idea against
the minute bars of the session that followed — taken, passed, rejected, expired, unfilled alike.
`tape stats` cuts the result by setup, by regime and by decision, and checks it against a gate that a
zero-edge record has to fail. `tape retro` reads the scored record back and proposes exact edits to
the playbook, which you apply one at a time. 886 tests, none of which touch the network.

What is not done is the part that isn't code. Tape has never been run against a real Alpaca account,
a real calendar API, or a real model. The next phase of this project is three or more months of real
mornings on paper — not more features.

Note: tape has no live-broker code path. `tape mode live` is refused, and no flag, environment
variable, or config value changes that.

---

## Why I built it

I wanted to find out whether I could trade, and I wanted the answer to be a number rather than a
feeling. The obvious way to get there is to point a good model at the market every morning and ask
what to buy — treat the AI as the best information collector I could afford. That was my premise
going in, and this repo is an experiment that is still running.

The premise did not survive the research Claude did before anything was built. It stops working
the moment you ask what you would do with the answer:

- If it says buy NVDA and NVDA goes up, was the model right, or was the whole market up that day?
- When a trade loses, was it the thesis, the entry, the size, or the fill?
- On a small account, how much of the move does a $1.00 minimum commission eat before I ever see
  a P&L?
- Did I follow the plan I wrote down, or improvise and then remember the improvisation as the plan?
- When I decline a suggestion, am I saving myself money or costing myself money?

Not one of those is answerable by a better model. Every one of them is answerable by a record —
provided the record includes the trades you didn't take, and prices the ones you did the way a real
broker would. So the design inverts it: **the journal is the product, and the model is a component
that gets graded like everything else in it.**

That inversion is what makes an LLM safe to have here at all. It is not being asked for an edge it
has no demonstrated claim to. It is being asked to apply rules I wrote, cite which rule it applied,
and make calls specific enough to be scored wrong.

## How it was built

The split is the one in
[Getting Out of the AI's Way](https://matt-trapp.com/posts/getting-out-of-the-ais-way/). Four
things are mine: the goal above, the playbook, the ledger that starts at $5,000 rather than
Alpaca's $100,000, and the gate — paper only until a bar written down in advance opens. Claude did
the research the next section summarises, wrote [docs/DESIGN.md](docs/DESIGN.md) off what it found
and its own [CLAUDE.md](CLAUDE.md) off that, and made the engineering decisions: the journal as the
source of truth, the cost model, the guardrails in Go, the provider layer, the replay conventions,
and the statistics behind the gate. A supervising agent with parallel subagents built the packages
against frozen contracts. I review every diff, and nothing is committed until I have read it.

Where this README explains why something is built the way it is, that is Claude's reasoning, kept
as written, unless it says otherwise.

## Influences

**Gary Stevenson's _The Trading Game_ — the origin, and not the lesson I went in for.** I read it
and came away thinking trading was an information problem, which is what sent me looking for a tool
that collects information faster. The research Claude dug up killed that idea: retail cannot win on speed, since
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

The briefing and the proposal path are both built inside those rules. The first and fourth are
structural. The second is why a call's threshold, the move it is graded on and the verdict are all
computed in Go from the session's own bars rather than read out of the reply — and why a proposal's
share count, risk dollars and reward/risk come from `internal/risk` and the ledger, never from the
model's JSON.

## Why it's built this way

Each of these is a decision with a consequence, in the order they were made. They are Claude's,
except where marked.

**The journal is the source of truth, not the broker.** Every stat comes out of SQLite and is then
decorated with live broker data, never the reverse. `Ledger` starts at the `starting_equity` in
config and computes cash and P&L from tape's own fills. The starting balance is my call: Alpaca
hands out $100,000 of paper money,
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

**Guardrails are Go, not prompt text.** Eleven rules run before an order can leave the machine, and
they are the same eleven whether the order came from `tape buy` or from `tape take` — an entry with
no stop, a target under the entry, an invalid order, a short, a trade over the risk cap, one
position too many, an average-down, an entry inside the flat-by-close window, a day already halted
on losses, a level the tape has moved away from, and a buy the ledger cannot pay for. They are
listed with what each one refuses under [The co-pilot](#the-co-pilot). Each refusal names the rule
and quotes its numbers, because a refusal the trader cannot verify is a refusal they will work
around:

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

A bracket's stop and target are both open sells over the same position, and only one of them can
ever trade, so the reservation counts the larger leg once rather than both. An exit is never
blocked by the bracket that opened it: a manual sell that the resting legs still stand in front of
cancels them first and says so, and the cancellations are journaled like any other order.

```
$ tape sell NVDA 10

cancelled 2 resting bracket legs for NVDA
```

Selling more than the ledger holds is still a short, and still refused. `tape cancel 4` and
`tape cancel --all` take resting orders off the books on their own.

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

**Not an auto-trader.** There is no unattended loop and no auto-take flag, not even for a
high-confidence idea. The model proposes; only `tape take` transmits. Autonomy is something the
record can earn later, one guardrail at a time.

**Not a signal service.** It is not going to tell you what will go up. The call of the day exists as
a scored experiment; if it turns out to have predictive value the stats will say so, and selling
that claim before the stats exist would mean building on the one thing the evidence rejects.

**Not financial advice.** It is an engineering plan for measuring something. See the disclaimer.

**No options, crypto, shorting, or fractional shares in v1.** Options are where beginners lose
fastest and they multiply every mistake. Crypto has thin qualitative information and never closes,
which fights a morning routine. Shorting is one more way to be wrong while learning. All of them fit
behind the same interfaces later.

**Not a backtester.** The counterfactual replay under [The mirror](#the-mirror) only ever replays
ideas the model actually filed, on the one session each was filed for, at the levels it wrote down.
It cannot tell you what a rule would have done over the last five years, because there is no record
of it having been applied then. A walk-forward backtest over historical bars is the Python sidecar's
job, and that sidecar does not exist yet.

**Not a data terminal.** Few providers, well-tested and replaceable, is the entire ambition — see
the OpenBB influence above.

## Disclaimer

Tape is a research tool, not investment advice. It reads public market data and news, asks a
language model what it makes of them, and writes down the answer. The model is often wrong, the data
is sometimes wrong, and neither of them knows anything about your finances.

Run it in paper mode until you have enough entries in the journal to judge it. That is the whole
reason the journal exists — a proposal you did not record is a proposal you cannot grade — and it is
why real money sits behind a gate with the criteria fixed in advance.

Read the source before you trade on anything it says. It is about 19,600 lines of Go, and the parts
that decide whether the numbers mean anything are small enough to read in a sitting: `internal/costs`
is 121 lines, `internal/risk` — all of the position sizing — is 156, the guardrails in
`internal/trading/rules.go` and `internal/trading/entry_rules.go` are 214 and 222, the replay that
grades every idea you passed on is 208 lines in `internal/counterfactual`, and the null trader and
the bootstrap the gate leans on are 132 lines in `internal/stats/significance.go`.

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
playbook ~/.tape/playbook.md (wrote the default playbook)
timezone America/Edmonton

Next steps
  1. Get free Alpaca paper keys at https://app.alpaca.markets
  2. export ALPACA_API_KEY=...
     export ALPACA_API_SECRET=...
  3. export ANTHROPIC_API_KEY=...   (llm provider anthropic)
  4. tape status, then tape brief

Optional, for the briefing's calendars
  export FRED_API_KEY=...      economic releases (https://fredaccount.stlouisfed.org/apikeys)
  export FINNHUB_API_KEY=...   watchlist earnings (https://finnhub.io/register)
  Without them the briefing still runs and names the calendars it went without.

tape's ledger starts at $5,000.00 regardless of Alpaca's paper balance.
```

Alpaca paper keys are free and take a couple of minutes. FRED and Finnhub are optional and also
free; without them the economic and earnings calendars come back empty and the briefing prints the
sources it went without, under `SOURCES`. Setting `$TAPE_HOME` moves the config, the journal and the
playbook somewhere other than `~/.tape`, which is how the test suite keeps runs isolated.

`init` writes `playbook.md` once and never again. A second `init --force` rewrites the config and
leaves the playbook alone, because the strategy file is yours.

The ritual is `brief` before the open, one decision on each idea it puts up, and `eod` at the close:

```sh
export ALPACA_API_KEY=... ALPACA_API_SECRET=...
export ANTHROPIC_API_KEY=...      # or whichever provider you configured

tape brief                        # the read, one graded call, and 0-3 sized ideas — before the open
tape take 1                       # trade idea 1 as a bracket, at the size Go computed
tape pass 2 --reason "gap already ran"
tape eod                          # flatten, expire what you never decided, recap the day
tape score                        # grade the call and the biases, replay every idea, after 16:30 ET
```

Then once a week, when the sessions have piled up:

```sh
tape stats --month                # what the record says, and where it stands against the gate
tape retro                        # the review, and the playbook edits it proposes
tape retro apply 3 --diff 1       # write the ones you agree with
```

`take` and `pass` are the whole decision surface: there is no auto-take flag, and a pass will not
go through without a reason, because the pass side is what gets counterfactually scored later.
`tape why 1` prints everything behind an idea, sizing arithmetic included, before you commit to it.

`eod` runs the scoring pass itself when you run it late enough, so `tape score` is for the days you
closed out early. The manual orders are still there when you want them:

```sh
tape status                       # ledger, broker balance, risk walls, market clock
tape buy SPY 1 --stop 511.00      # --stop is required; journaled and cost-modeled
tape pos                          # open positions from the journal, priced live
```

The blocks below are real output, rendered through the in-memory venue the tests use rather than a
live Alpaca account — see [Status](#status). Every number in them comes from the same code path a
real fill takes.

```console
$ tape buy AAPL 10 --stop 98 --note "range break"

[paper] buy 10 AAPL

  journal id  #1
  order       buy 10 AAPL market
  status      filled
  broker id   fake-1
  stop        $98.00

Fills
QTY  RAW      MODELED  COMMISSION  FEES   COST
10   $100.00  $100.05  $1.00       $0.00  $1,001.50
```

The stop is not optional. Ten shares stopping two dollars lower risks $20, which fits under the
$25 the 0.5% per-trade cap allows on a $5,000 ledger; a stop at $95 would risk $50 and be refused
before it reached the venue.

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
  orders cancelled  1
  positions closed  1
  fills recorded    1
  closed sell 10 AAPL (journal #3, filled)

Recap 2026-08-30
  orders                  3
  trades closed           1
  wins / losses           1 / 0
  refusals today          0
  gross                   +$98.95
  costs on closed trades  $2.03
  costs on today's fills  $2.03
  net                     +$96.92

Call
  call grades after 16:30 ET; run `tape score` later.

flat.
```

Three orders for one round trip: the entry, the stop leg the bracket rested behind it — journaled
from birth, so a stop that fires is a fill tape can see — and the sell that closed it. `orders
cancelled` is that stop leg coming off the books on the way to flat. `refusals today` counts the
guardrails that had to say no; the gate asks for a month of zeroes there.

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

cancelled 1 resting bracket leg for AAPL

  journal id  #3
  order       sell 10 AAPL market
  status      filled
  broker id   fake-3

Fills
QTY  RAW      MODELED  COMMISSION  FEES   NET
10   $110.00  $109.95  $1.00       $0.03  $1,098.42
```

`FEES` is $0.03 here and $0.00 on the buy: the SEC fee and the FINRA TAF fall on sells only. Note
the slippage direction too — the buy was modeled above the quote, this sell below it. Both against
you.

## The briefing

`tape brief` is the morning ritual. It reads the market, hands the model everything it collected
together with your playbook, and archives the reply with one call that gets graded after the close.
It places no orders — the slate it prints is covered under [The co-pilot](#the-co-pilot) and waits
on a command you type. The call was built before the proposals were, so that the question of whether
the reads are worth anything gets asked before anything is riding on them.

**What the model is shown**, all of it archived verbatim next to the reply so an old briefing can be
re-read against what actually happened:

- The index ETFs — `SPY QQQ IWM DIA` by default — with the last trade, the previous close, and the
  move between them.
- A regime label computed in Go from eighty daily bars of `SPY`: a 20- and 50-day moving average
  and annualised realised volatility over twenty days, turned into `uptrend, low vol` by fixed
  thresholds. The model reads the label and is told to let it set the day's ceiling. It does not get
  to choose it, and the bar for today's unfinished session is dropped before the averages are taken.
- The calendar for the next three days: FOMC decision days from a table compiled into the binary,
  US economic releases from FRED, and earnings for watchlist symbols from Finnhub.
- The watchlist with quotes, up to five headlines per symbol and fifteen market-wide ones, each
  story's summary clipped to 300 characters.
- The session's top gainers, losers and most actives from Alpaca's screener.
- The ledger's cash and equity, the venue clock, and the list of sources that were unreachable.
- The risk limits, under a heading that says they are enforced in code and cannot be moved, so the
  model plans inside the walls rather than being told off by them afterwards.
- `playbook.md`, verbatim, as the last block of the message.

All of that has to fit 60,000 characters. Over the cap, the prompt drops to two headlines a symbol,
then to none, then drops the movers; a briefing that still does not fit is cut at the tail, which
lands in the playbook, because a playbook that long is yours to shorten and the morning should still
run.

**What it must return** is JSON against a strict schema: a market read, a regime note, a calendar
note, at most twelve watchlist notes each carrying a bias of bullish, bearish or neutral — every one
of which is graded after the close, exactly like the call — at most five risks, at most three trade
proposals, and one call of the day. The call is an instrument, a
direction of up, down or flat, a threshold in percent — or null to take the desk's configured
default — a rationale that names the playbook rule it rests on, and an invalidation: the one
observation that would prove it wrong before the close. `threshold_pct` is nullable on purpose. A
required numeric field with no way to say "I don't know" is an instruction to fabricate, which is the
failure mode [docs/models.md](docs/models.md) is written around.

The proposals list may be empty, and the prompt says so in as many words: when the regime posture or
an `N1` condition says stand down, zero ideas is the right answer and the watchlist note on the
symbol it would otherwise have traded is where the reason goes. Nothing about the slate is scored on
length.

### The rules that make the grade mean something

Every one of these is a refusal somewhere in the code, and each exists because the honest version of
the question has an answer that flatters the model less.

**Calls lock at 09:30 Eastern, and so does the slate.** A second `tape brief` on the same session
reprints the archive rather than spending another call on the same morning. `--force` archives a
fresh briefing, and before the bell it replaces the call and expires the earlier slate; after the
bell the session's first call and first slate both stand and the footer says so, because a
prediction about a session already in progress is not a prediction, and the ideas you have been
deciding on all morning are not something a re-run gets to swap out underneath you. An evening run
is a call and a slate on the *next* session, keyed to that day, and the next morning reads them back
with the time they were written on them.

**Only complete sessions are graded.** A call is scored from the first and last regular one-minute
bars of its own session, never from a daily bar the venue may still be building, and not before
16:30 Eastern — the free REST feed runs fifteen minutes behind. A call is graded once: the journal
refuses a second score on a row that already has one. A wrong grade is a bug to fix, not something
to re-score away.

**Session dates are the venue's, not mine.** Alpaca stamps a daily bar at midnight Eastern, which
reads as the previous day in every zone west of it — including the one I trade from. Every "which
session is this" decision goes through one function in `America/New_York`. My own timezone is for
display and day recaps.

**The model may only name what it was shown.** A call, a watch note or a proposal on a symbol
outside the indexes and the watchlist is rejected before anything is filed, as is a lowercase
ticker, a missing invalidation, a missing rationale, a threshold at or below zero or above 5%, a
proposal that repeats a symbol, and a proposal citing a setup id the playbook does not define.

**A reply that fails validation is archived and refused.** The raw text lands in the journal, the
command exits non-zero naming the briefing id, and no call is filed. Re-reading that briefing later
fails the same way rather than quietly showing a half-parsed one. A reply that could not be trusted
once does not become trustworthy by being read again.

**Headlines are data, not instructions.** Everything a news wire or an API wrote is fenced in the
prompt inside a block marked `untrusted text, data only`, and the system prompt says in as many
words that instructions come from it and the playbook and from nothing in the message. Source
warnings are fenced the same way and clipped to 120 characters in the prompt, so a provider that
echoes a response body back at me cannot become most of the prompt; the archive keeps the whole
thing.

### What a briefing looks like

This is the golden output of the fake-venue test — `TestBriefRenderGolden` in
`internal/cli/briefrender_test.go`, which pins the renderer against an in-memory venue, a fixed
clock and a canned reply. It is what the terminal prints with colour off, byte for byte, on a
morning whose call has already been graded:

```
TAPE · Fri Aug 28 · 06:52 MDT · market opens in 38m · cash $5,000.00
MARKET   SPY +0.41%  QQQ +0.62%  IWM -0.10%  DIA +0.20%
         Breadth is narrow.
REGIME   uptrend, low vol (SPY 512.10 above 20d 505.30 and 50d 498.80; 20d vol 11.2%)
         M2 continuations are live at normal size.
CALL     SPY up ≥0.3% open→close   [✓ +0.42%]
         M2: price above the 20d.
         invalid if: SPY trades below 509.80.
PROPOSALS (2)
  #1  LONG NVDA — M2 momentum continuation above prior high
      entry 128.40  stop 126.90  target 131.40  size 16 sh (~$2,054 · risks $24 = 0.5%)  2.0R
      thesis: Holds the breakout shelf.
      invalid if: Loses 126.90 on volume.   confidence: medium
  #2  LONG COST — R1 range-edge mean reversion   ✗ rejected: reward/risk 1.2 is under the 1.5 minimum (rule: reward/risk)
act: tape take 1 · tape pass 1 --reason "…" · tape why 1
CALENDAR
  08:30     CPI (high)
         CPI is the session's only scheduled risk.
WATCHLIST
  NVDA  +3.1%  bullish  Holds above 118.40.
MOVERS   gainers: ABCD +12.3%   losers: WXYZ -8.4%
RISKS    • A quiet open can fade.
SOURCES  FRED calendar unavailable: FRED_API_KEY not set
briefing #12 · fake-model-1 · 41.2k in / 1.1k out · 38s · est. $0.12
```

Computed facts sit on the label lines and the model's words sit underneath them, so it is always
obvious which is which. `CALL` carries its verdict inline — `[scored after close]` until the session
is read, then the mark and the actual move. Under `PROPOSALS`, the sizes and the R multiple are
computed here and the thesis is the model's; `#2` was refused before the trader ever saw it, and is
still on the screen and on the record. `SOURCES` is what the briefing was written without, and it is
on the screen rather than in a log because a read built without the economic calendar is a different
read.

The archive is a command too:

```sh
tape briefs             # every briefing, newest first, with its call and its grade
tape briefs show today  # re-render one from the journal
tape brief --dry-run    # assemble everything and print both prompts, ask nothing, archive nothing
tape brief --json       # the input, the reply and the briefing id, for the sidecar
```

`--dry-run` is the one to reach for before you trust a morning: it prints the entire prompt the
model would have been sent, character count and all, without spending a call.

### The playbook is yours

`playbook.md` sits next to the config and the journal, and tape reads it, cites it, and never
writes to it. `init` seeds it once with something you can actually run against: a posture for each
regime the classifier can produce, four setups with ids — `M1` gap-and-go continuation, `M2`
momentum continuation above the prior high, `R1` range-edge mean reversion, and `N1`, the
conditions that end the discussion for a symbol — a set of risk rules that restate what Go enforces,
and the rules for the call of the day.

A proposal has to cite one of those ids, and `N1` is not one of them: an N-rule is a no-trade
condition, so citing it would be arguing for the trade it forbids. Restating the risk rules in the
file is what lets the briefing plan inside them; changing a number there does not move the wall,
which lives in `[risk]` and in Go.

It is a starting point and not a recommendation. It is in the repo so the first briefing has
something to cite, and the whole design of the learning loop is that you change it, in git, against
a scored record. `tape playbook` prints the one you have.

## The co-pilot

The briefing ends with a slate: zero to three trade ideas you take or pass one at a time. Nothing on
it reaches a venue until you type `tape take N`. There is no auto-take flag and no unattended loop,
and the model has no code path to either.

**A proposal is nine fields and every one of them is required.** A symbol the briefing actually
showed — an index ETF or something on your watchlist, nothing else. A side, which is `long`, because
v1 does not short. A setup id the playbook defines, like `M2`; an id starting with `N` is a no-trade
condition and is refused, since a proposal citing one argues for the trade its own rule forbids. An
entry, a stop below it and a target above it, all three present, because an idea without its exit is
a watchlist note. A thesis, and an invalidation: the one observation that would say this is not
working. And a confidence of low, medium or high — the model's own read, and the only field that
changes nothing at all. It does not scale the size. It is on the record so the scoring can find out
later whether it ever meant anything.

**Size is never the model's.** The model supplies prices; `risk.SizeWithin` supplies the share count,
from the ledger and the limits:

```
budget  = equity × risk.per_trade_pct / 100
shares  = floor(budget ÷ (entry − stop))
ceiling = free cash × 0.98, and the position is trimmed to what that buys
```

Free cash is the ledger's cash less what open orders already claim; the 2% left over absorbs the
slippage and the commission the entry price does not carry but the ledger pays. Risk bounds the
loss, cash bounds the bill, and a $5,000 ledger cannot buy $8,000 of stock however good the idea is.
A size the risk budget alone would have allowed but the cash ceiling cut is labelled `cash-capped`
on the slate, so it is never a mystery why sixteen shares became nine. `tape why N` prints the whole
calculation.

Two limits are checked before an idea is sized at all: the target has to pay at least
`risk.min_reward_risk` times what the stop costs, and the entry has to sit within
`risk.max_entry_deviation_pct` of the price the briefing was shown that symbol at. An idea that
fails either — or that the risk budget cannot buy a single share of — is still written to the
journal, as `rejected`, with the rule text on the row. Nothing the model said is dropped, and
nothing it said is sized by it.

This is the slate half of the golden further up — `TestBriefRenderGolden` in
`internal/cli/briefrender_test.go`, the renderer pinned byte for byte against an in-memory venue, a
fixed clock and a canned reply, with colour off:

```
PROPOSALS (2)
  #1  LONG NVDA — M2 momentum continuation above prior high
      entry 128.40  stop 126.90  target 131.40  size 16 sh (~$2,054 · risks $24 = 0.5%)  2.0R
      thesis: Holds the breakout shelf.
      invalid if: Loses 126.90 on volume.   confidence: medium
  #2  LONG COST — R1 range-edge mean reversion   ✗ rejected: reward/risk 1.2 is under the 1.5 minimum (rule: reward/risk)
act: tape take 1 · tape pass 1 --reason "…" · tape why 1
```

`$24 = 0.5%` is the point of the whole block: what the stop costs, and what fraction of the account
that is. The `2.0R` is what the target pays for it. Both are computed here, from the prices above
them.

### The slate, and what becomes of each idea

Every idea gets exactly one ending:

```
proposed ──┬─► taken ──► unfilled        the order died without trading a share
           ├─► passed                    you declined it, with a reason
           ├─► rejected                  a rule refused the idea itself
           └─► expired                   the session ended and nobody decided
```

Between `proposed` and `taken` sits `submitting`. `take` claims the idea before it sends anything, so
a crash between the venue accepting an order and the decision landing leaves a claim rather than a
takeable proposal — and a claimed idea can never be taken twice. `tape proposals --reconcile` closes
that window from the record without sending anything: the order at the venue is already the trade. A
claim released by a rule that only refused today goes back to `proposed`, because the idea survived
the refusal.

| Command | What it does |
| --- | --- |
| `tape proposals` | the session's slate and what became of each idea; `--day`, `--reconcile` |
| `tape take N` | submits idea N as a bracket — a limit at its entry with the stop and target attached — at the size Go computed. `--qty` may lower that size, never raise it |
| `tape pass N --reason "…"` | records the veto and why. The reason is required |
| `tape why N` | everything behind one idea: levels, thesis, invalidation, status, and the sizing arithmetic |
| `tape cancel ID...` | takes resting orders off the books; `--all` cancels everything working |

The tests pin what `take` prints on the fake venue — `[paper] take 1`, then
`taken: proposal #1 NVDA 16 sh → order #1` — and what `why 1` shows under
`Sizing (computed in Go, never by the model)`: `$5,000.00 equity × 0.5% = $25.00`, then
`$25.00 / ($120.60 − $119.10) = 16, rounded down`. When `--qty` lowered a take, the `proposals`
table carries both numbers, `16→4` and `$24.00→$6.00`, because the record has to say what was
traded next to what was sized.

**The slate locks at the open, like the call.** A forced re-run before 09:30 Eastern expires the
earlier slate and files a new one. After the bell the earlier slate stands and the new briefing is a
second read, not a second set of trades. And the slate day is the *session* day: an evening briefing
is a slate on tomorrow, and `take`, `pass`, `why`, `proposals` and `eod` all resolve `N` against
that session rather than against your calendar date, so the slate that printed is the slate you act
on.

**`eod` closes the record as well as the positions.** It expires every idea nobody decided — an
undecided proposal is not one you can take tomorrow, it is one the session ran out on — and, once
the venue has been cancelled and flattened, it marks any take whose order died without trading a
share as `unfilled`. That one was never a trade, so the stats never count it as one — its levels
still replay, but it contributes nothing to execution drag, because there is no fill to compare
against. Then, if the session is behind us, `eod` runs the scoring pass itself.

### The guardrails

Eleven rules run in Go before an order can leave the machine. An entry clears ten of them, and `tape
take` and `tape buy` take exactly the same path through them. A sell clears the two that can apply
to an exit. Each refusal names the rule and its numbers and is written to the `refusals` table,
which is where "zero guardrail breaches in the final month" gets counted from.

The column that matters is the last one. A rule that refuses a fact about the *idea* decides it: the
numbers will not change, so the proposal is marked `rejected` and is done. A rule that refuses
today's *circumstances* decides nothing — the proposal stays open and only the refusal is recorded,
because a position limit that lifts in an hour is not a verdict on the trade.

| Rule | Refuses | Verdict on the idea |
| --- | --- | --- |
| valid order | an empty symbol, a non-positive quantity, a limit price of zero, an unknown side | intrinsic — rejects it |
| no entry without a stop | a buy with no stop, a stop of zero, or a stop at or above the entry | intrinsic — rejects it |
| target above entry | a take-profit at or below the price the position opens at | intrinsic — rejects it |
| no shorting | a sell larger than the ledger holds free of what resting sells already claim | intrinsic — rejects it |
| risk cap | a trade that would lose more than `per_trade_pct` of ledger equity at its stop | situational — leaves it open |
| max positions | one more open position than `max_positions`, counting pending entries; adding to a symbol already held takes no new slot | situational — leaves it open |
| no averaging down | a second entry below the average already paid for that symbol | situational — leaves it open |
| flat by close | a new entry inside `no_entries_before_close_minutes` of the bell | situational — leaves it open |
| daily halt | any new entry once `max_daily_losses` positions have closed the day at a gross loss | situational — leaves it open |
| stale entry | an entry more than `max_entry_deviation_pct` from the last price, or one the tape has already traded through its own stop | situational — leaves it open |
| no overspend | a buy costing more than ledger cash less what open orders already claim, priced with the cost model | situational — leaves it open |

The halt counts *positions*, not exits. Scaling out of one winner in three clips is not three
losses, and a position that nets negative only because the commission floor landed twice was not a
losing read — so neither ends the day.

**An exit is always allowed.** Sells run none of the entry rules, and a sell blocked only by the
bracket legs resting over its own shares cancels those legs first and says so. A rule that traps you
in a position is worse than the one it enforces. Selling more than the ledger holds is still a
short, and still refused.

## The mirror

A record you never grade is a diary. Three commands turn it into evidence: `tape score` settles what
the sessions have answered, `tape stats` says what the settled record adds up to, and `tape retro`
reads that back and proposes changes to the playbook. None of them can be re-run into a nicer
number — every grade is written once, and the journal refuses a second one.

### What gets graded

`tape score` runs after the close; `tape eod` calls it for you when you run it late enough. It
settles three things for every session on or before the day it is asked about.

**The call of the day**, from its own session's first and last regular minute bars.

**Every watchlist bias.** Up to twelve a morning, each graded against its own symbol's session at
the threshold that morning's call was filed under — or the desk's `call_threshold_pct` when the
briefing filed no call, so a note and the call it sat beside are settled by the same size of move.
Bullish has to clear the threshold, bearish has to clear it downward, neutral has to stay inside it.
One call a day is a sample of one; twelve notes a day is the only way the model's read reaches a
sample size in under a decade. A symbol nobody can grade is named and left out — it does not cost
the session its other eleven — and one symbol carries one grade per session, however many briefings
that session archived.

**A counterfactual replay of every decided idea.** Taken, passed, rejected, expired and unfilled all
replay the same way, against the minute bars of the session the idea was written for, at the levels
the model wrote down. That symmetry is the entire point: a pass and a take that are scored by
different machinery cannot be compared, and comparing them is the question the whole journal exists
to answer.

```console
$ tape score

[paper] score

Calls through 2026-08-28
  2026-08-28  SPY  up ≥0.3%  actual +0.50%  ✓

Replays through 2026-08-28
  2026-08-28  #1 NVDA  M2  passed    target  +$53.31  +2.39R
  2026-08-28  #2 AMZN  M1  taken     target  +$81.65  +3.61R
  2026-08-28  #3 AAPL  R1  rejected  target  $0.00  +1.67R

Notes through 2026-08-28
  2026-08-28  NVDA  bullish  actual +0.50%  ✓

last 30 days: 1/1 (100%)
1 calls graded in all; this needs 3+ months to mean anything.
```

That block, and every other one in this section, is real output rendered against the in-memory venue
and the fake model the test suite uses — see [Status](#status). `#3 AAPL` shows why the R column and
the dollar column are separate: it was the idea the reward/risk rule rejected before it was ever
sized, so it replays at zero shares. The levels still score, the dollars are zero, and the record can
still tell you later whether the rule that refused it was refusing good trades.

### The replay rules, and which of them are conventions

A minute bar is a summary. It says the high and the low that minute printed and not the order they
printed in, so a replay has to decide some things a bar cannot. Tape decides them the same way every
time, writes down which replays needed deciding, and reports the count next to the numbers it shapes.

- **The entry fills on touch.** A long limit fills on the first bar whose low reaches the entry. It
  pays `min(entry, bar open)`, so a bar that gapped underneath the limit fills at the open rather
  than at a price nobody was offering. **Fill-on-touch is the optimistic convention here** — a real
  resting limit is behind a queue and may not fill on a low that only kisses it. It is written down
  rather than hidden.
- **The fill bar can stop you out but cannot reach the target.** Within the minute you filled, the
  stop was live from the moment of entry and the take-profit was not resting yet when that minute's
  high printed.
- **A bar that spans both the stop and the target is a stop.** One minute cannot say which level
  came first, so the conservative reading wins and the outcome is marked `Ambiguous`. `tape stats`
  reports the count as `decided by the stop-first rule`, and any replay it decided prints as
  `stop (stop-first)` rather than a bare `stop`.
- **A stop gapped through fills at the open**, `min(stop, bar open)`, not at the level nobody offered.
- **Anything still open at the last bar exits at the close.** No overnight, matching the flat-by-close
  rule the live path enforces.
- **Both sides pay the cost model.** Commission, regulatory fees and slippage land on the replayed
  entry and the replayed exit, so a replay's net is comparable with what the journal books for the
  same round trip rather than flattering it.
- **Size is what you would actually have held** — the sized quantity, or the smaller number a
  `take --qty` sent. The R multiple is per share, so an idea nobody could size still scores its levels.
- **The bars are sorted before they are walked.** Oldest first, stably, so the path is the replay's to
  establish and not the feed's to promise.
- **A replay that cannot be trusted is refused, not filed.** Levels that do not describe a long trade,
  a session with no prints, and a fill bar whose open sits more than 25% from the proposed entry — the
  signature of a split the feed has since adjusted for — are all reported and left for a later run.

**Only complete sessions are graded, and a half day is a complete session.** When the broker adapter
can answer for the venue's own calendar, a session counts as finished once its last print reaches
within five minutes of that day's actual close, so a 13:00 ET early close grades like any other day.
The fixed 15:55 ET floor is only the fallback for an adapter that cannot say.

### What `tape stats` shows

Nine sections, computed from the journal and nothing else:

```console
$ tape stats --all

[paper] stats

the whole record, through 2026-08-28 · 1 session(s) · paper

TRADES
  nothing closed in this window.

EQUITY (the whole record; a window never moves the account)
  start         $5,000.00
  end           $5,000.00
  return        +0.00%
  max drawdown  $0.00 (0.0%)
  this window   $0.00 (+0.00%)

BY SETUP
  SETUP  TRADES  WIN  EXPECTANCY  NET    REPLAYS     REPLAY NET
  M1     0       -    $0.00       $0.00  1/1 filled  +$81.65
  M2     0       -    $0.00       $0.00  1/1 filled  +$53.31
  R1     0       -    $0.00       $0.00  1/1 filled  $0.00

BY REGIME
  REGIME            SESSIONS  CALLS  NOTES  TRADES  NET
  uptrend, low vol  1         1/1    1/1    0       $0.00

CALLS / NOTES
  KIND   GRADED  CORRECT   PENDING  INSIDE NOISE BAND
  calls  1       1 (100%)  0        0
  notes  1       1 (100%)  0        0
  2 reads graded here; this needs 3+ months to mean anything.

PROPOSALS
  status                           proposed 0 · taken 0 · passed 1 · rejected 1 · expired 0 · unfilled 1
  passes that would have profited  1 ($53.31 left on the table)
  losses the vetoes avoided        $0.00
  execution drag on takes          $0.00
  replays                          3 replayed · 3 filled (2 win / 0 loss) · net +$134.96 · avg 2.56R
  decided by the stop-first rule   0

REFUSALS
  no guardrail had to say no.

SIGNIFICANCE
  too few trades to build a zero-edge trader from.

GATE
  reading from 2026-08-31
  playbook version #1 (first snapshot) was recorded that day; the gate reads only what came after it, so a rule fitted to the record is never graded on the record that produced it.
  CHECK                   ACTUAL                                NEEDED
  months covered          0.0 mo since 2026-08-31               3 mo                ✗
  sessions                0 since 2026-08-31                    50+                 ✗
  trades                  0 since 2026-08-31                    100+                ✗
  expectancy              $0.00/trade since 2026-08-31          > $0.00             ✗
  expectancy lower bound  insufficient trades since 2026-08-31  > $0.00             ✗
  profit factor           0.00 since 2026-08-31                 >= 1.30             ✗
  max drawdown            0.0% since 2026-08-31                 <= 10.0%            ✓
  null pass rate          insufficient trades since 2026-08-31  <= 10.0%            ✗
  refusals last month     0 since 2026-08-31                    <= 0                ✓
  setups identified       0 since 2026-08-31                    1+ (10 trades, +E)  ✗

the gate is shut. tape trades paper until every line above reads ✓.
```

Four of those readouts answer questions you cannot get from a broker statement:

**By setup** puts each playbook rule's real trades next to the replay of every idea that cited it,
taken or not. A rule can be net positive on the two trades you happened to take and net negative
across all thirteen it proposed; the second column is the one that says whether the rule works,
because the first is filtered by your own decisions.

**By regime** cuts the same record by the market label the briefing archived that morning — the only
regime reading the archive can still prove, since it was computed in Go from daily bars before the
model saw it. Days that ran with no archived briefing get their own row rather than being absorbed
into somebody else's.

**Veto quality** is the `passes that would have profited` and `losses the vetoes avoided` pair.
Between them they are the answer to the question in [Why I built it](#why-i-built-it) that started
this whole thing: when you decline a suggestion, are you saving yourself money or costing yourself
money?

**Execution drag** is the replay's net minus what a take actually booked, summed. The replay fills at
the level; you filled where the venue filled you, at the moment you typed the command. The gap is
what your execution costs, separated from whether the idea was any good — and it is only counted
where the order actually traded, because a take that never filled has nothing to compare against.
That is why the idea `tape score` printed as `taken` shows up under `unfilled` above: by the time
`eod` ran, its limit had never traded a share. Its levels still replayed; its drag is not a number.

`INSIDE NOISE BAND` counts the reads whose actual move landed within 5 basis points of the threshold
they were graded against. Those were decided inside the feed's own measurement error rather than by
the read, and they are printed beside the accuracy they inflate.

**A window scopes the description, never the account.** `--month`, `--all` and `--from`/`--to` cut
the trades, the setups, the regimes, the reads, the proposals and the refusals. `EQUITY` and `GATE`
always read the whole record through the window's end, so `tape stats --month` and `tape gate` can
never disagree about the account, the drawdown, or whether the gate is open. The only window-shaped
line in `EQUITY` is `this window`, and it says so.

`tape gate` is the last two sections on their own, over the whole record. `tape stats --json` prints
the entire report for the sidecar.

### Why the gate is harder than the numbers look

A profit factor of 1.3 over fifty trades is not evidence of anything. A trader with no edge at all —
winning exactly as often as their own win and loss sizes require to break even, and no more — clears
it often enough that the threshold on its own is decoration. So `tape stats` builds that trader and
runs him: ten thousand records at your own sample size, each trade winning at that break-even rate, each
trade's size drawn from your own wins and your own losses — so a profit factor resting on one
outlier is tested against a null that can draw that outlier too. A path that breaches the drawdown
ceiling dies where it breached it. The gate requires that at most `max_null_pass_rate` of those
paths clear the thresholds you did. Beside it sits a bootstrap: ten thousand resamples of your net
P&L with replacement, of which the 2.5th percentile of the resampled means has to be above zero —
the low end of a 95% interval on expectancy, not the point estimate. Both stay silent under twenty
trades or fewer than five on either side, and the gate wants a hundred trades before it will pass
that line at all. And the gate reads only the sessions after the most recent playbook or rule
change, because the retro fits the playbook to the record and a rule fitted to a record must not
then be graded on the record that produced it. That is in-sample optimisation, and it is the exact
failure [FINSABER](https://arxiv.org/abs/2505.07078) found when it re-ran LLM timing strategies with
data-snooping controls: an edge that survives only until somebody controls for the fitting. Every
one of these makes the gate slower to open, and slower is the correct direction.

**What resets that window is what changes the meaning of a trade**: the playbook text, the risk
limits, the cost model, the regime symbol, the call threshold, and the model or the provider. Never
the watchlist, the news lookback, or a key — a symbol you added changes what the model looks at, not
what a trade means. A hand edit of `playbook.md` counts exactly like an edit a review applied, because
the file is fingerprinted, not trusted. Every snapshot is a row:

```console
$ tape playbook versions

[paper] playbook versions

ID  TAKEN        SHA           REVIEW  NOTE
#1  08-31 08:46  2a1bc5e31c80  -       first snapshot
```

The snapshot is taken by `stats`, `gate` and `retro` when they read the record, not by the listing,
so the row is written the moment the change is first observed rather than the moment you look.

### The weekly review

`tape retro` is one model call a week. It reads the last `[retro] weeks` of sessions, or `--weeks N`:
the stats report for that window, the whole-record gate table with its significance test, the three
best and worst trades by name, every pass with the replay of what it cost or saved, the guardrail
refusals by rule, the previous review's own summary, the risk limits, the setup ids the playbook
defines, and `playbook.md` verbatim as the last block. Everything in there that somebody else wrote
— your pass reasons, the earlier review's summary — is fenced as untrusted data with markers it
cannot close from inside. The playbook is the one trusted block in the message, which is exactly why
its edits go through you.

```console
$ tape retro

[paper] retro

review #1 · 2026-08-22 → 2026-08-28 · fake-model-1 (fake) · 41.2k in / 1.1k out

SUMMARY
  One trade and one call is not a sample; the only defensible change is a note, not a
  rule.

FINDINGS
  1. M1 is the only rule that traded  (low confidence)
     1 trade, 1 replay, +$0 expectancy
  2. Every veto so far was on an extended open  (low confidence)
     1 pass, replayed to its target

PLAYBOOK DIFFS
  1. add under ## Setups
     why: The record shows no second continuation rule.
     + ### M3 midday continuation - When: the noon high breaks on rising volume. -
     + Invalidation: a close back under the noon high.
  2. add under ## Posture by regime
     why: The week's only regime was uptrend, low vol.
     + **Note.** Every session in the record so far has been uptrend, low vol.
act: tape retro apply 1 --diff 1 · --all
```

**A diff is an exact text edit, not a description of one.** It names a heading that already exists in
the playbook and one of three changes: `add` appends text under that section, `edit` replaces a
quoted `before` with an `after`, `remove` deletes the `before`. For an edit or a remove the `before`
has to appear *exactly once* under the named section — zero matches and it is refused, two matches
and it is refused, because a diff that could land in two places is a diff nobody chose.

**What a diff may never touch.** Not `## Risk rules`, and not anything nested under it: those numbers
are enforced in Go, the model is shown them only so that what it writes plans inside them, and a diff
naming that section is rejected unread. New text may not open a section of its own — no level-1 or
level-2 heading, and no setext underline, which renders as one. A new `### <ID> title` has to carry a
real setup id and one the playbook does not already define, because an id names one rule. At most
eight findings and five diffs, every finding carrying the numbers behind it and its own confidence,
every diff carrying a rationale.

**An empty diff list is frequently the right answer**, and the prompt says so in as many words. A
dozen trades cannot separate a rule from a coin flip and a week of calls cannot either; a review that
says the sample is too small and proposes nothing has done its job. The section renders as
`none. an empty list is a real answer: the record could not carry a change.`

**`tape retro apply` is the only thing that edits a rule**, and it edits nothing you did not name:
`--diff 1,3` or `--all`, never by default. Each chosen diff is resolved against the playbook *as it
stands now*, not as it stood when the review was written, so a file you have since edited by hand
either still matches or the apply refuses — and that hand edit gets its own snapshot first, so the
two changes are two versions rather than one. Then it is atomic in the ways that matter: the new
file is staged beside the old one and moved into place by rename, so no reader ever sees half a
strategy file; the file it replaced is kept in `playbook.history/`, named for the moment it stopped
being in force; and the version row plus the mark on every applied diff commit in one transaction,
before the file moves, so a crash can never leave edits that nothing records as landed. A diff
applies once — a second `apply` of the same one is refused. The command prints `applied 1 edit(s)`,
the `previous playbook` path, the new `version`, and the line that matters: `the gate now reads only`
the sessions from here on.

`tape retro --dry-run` prints both prompts, character counts and all, and asks nothing.
`tape retro show <id|latest>` re-renders an archived review, and `--json` gives the sidecar the
input, the reply and the diffs. A reply that fails validation is archived verbatim and reported as
failed — never rendered as if it had passed.

## Configuration

`~/.tape/config.toml`, written by `tape init` and edited by hand. Secrets belong in the environment;
`ALPACA_API_KEY`, `ALPACA_API_SECRET`, `FRED_API_KEY` and `FINNHUB_API_KEY` override whatever is in
the file, and a command that rewrites the config — `tape mode paper`, `tape watchlist add` — will
not write an env-supplied key back into it.

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

[data]                    # the briefing's optional calendars; both keys are free
fred_api_key = ''         # prefer $FRED_API_KEY
finnhub_api_key = ''      # prefer $FINNHUB_API_KEY

[brief]
watchlist = ['SPY', 'QQQ', 'AAPL', 'MSFT', 'NVDA', 'AMZN', 'GOOGL', 'META']
index_symbols = ['SPY', 'QQQ', 'IWM', 'DIA']   # the MARKET line, and the symbols a call may name
regime_symbol = 'SPY'     # what the regime is classified from
call_threshold_pct = 0.3  # the move a call must clear when it leaves its own threshold null
news_lookback_hours = 18  # 0 turns news off rather than asking for stories since right now
movers_top = 10           # 0 skips the screener
calendar_days = 3         # how far ahead the calendar looks

[risk]                    # the walls; the model is shown them and cannot move them
require_stop = true       # every entry carries its exit, `tape buy` included
per_trade_pct = 0.5       # share of ledger equity one trade may lose at its stop. Must be in (0, 5]
max_positions = 3         # open positions plus pending entries. At least 1
max_daily_losses = 2      # positions closed at a gross loss before the day is over. At least 1
no_entries_before_close_minutes = 30   # no new entries this close to the bell
min_reward_risk = 1.5     # smallest target, in multiples of the stop distance. At least 1
max_entry_deviation_pct = 5.0          # how far a proposed entry may sit from the last price

[retro]                   # the weekly review
weeks = 1                 # how many weeks of sessions a review reads by default. At least 1
model = ''                # overrides [llm] model for the review only; blank runs the same model

[gate]                    # the real-money threshold. Reading it unlocks nothing
min_months = 3            # calendar months the gate window has to span. At least 1
min_sessions = 50         # sessions inside that window. At least 1
min_trades = 100          # closed trades. At least 30; below that, edge and noise look identical
min_profit_factor = 1.3   # gross profit over gross loss. At least 1
max_drawdown_pct = 10.0   # deepest fall from a peak, over the whole record. Within (0, 50]
min_expectancy_usd = 0.0  # net per trade has to come out above this
max_refusals_last_month = 0            # guardrail breaches allowed in the final 30 days
max_null_pass_rate = 0.1  # how often a zero-edge trader may clear the thresholds. Within (0, 0.5]
```

The SEC fee and the FINRA TAF are not in the file. They are not yours to choose, so they live in
`costs.Default()` and apply to every sell regardless of what the config says.

`brief.watchlist` is also editable from the CLI, which is the same file with the validation and the
env-secret scrub already wired: `tape watchlist ls`, `tape watchlist add TSLA AMD`, `tape watchlist
rm META`.

```console
$ tape watchlist ls

[paper] watchlist

SPY  QQQ  AAPL  MSFT  NVDA  AMZN  GOOGL  META
8 symbol(s) · indexes SPY QQQ IWM DIA · regime SPY
```

`call_threshold_pct` will not take a zero. A zero threshold makes an unchanged close count as both
up and down and never flat, which is a call that cannot be wrong — the same reason a negative cost
is refused two blocks up.

The `[risk]` walls will not go to zero either. `per_trade_pct` has to sit inside `(0, 5]`,
`max_positions` and `max_daily_losses` at one or more, `min_reward_risk` at one or more, and
`max_entry_deviation_pct` above zero. The gate is measured *inside* these walls, so a wall widened
to nothing flatters every stat it feeds. `require_stop` is the one you can turn off, and turning it
off is a decision to trade without a bounded loss.

`[gate]` has a floor of its own, for the same reason: the numbers were written down before any
results existed and they are not supposed to soften afterwards. `min_profit_factor` will not go
under 1, `max_drawdown_pct` has to sit inside `(0, 50]`, `min_trades` refuses anything under 30
because fewer trades cannot separate edge from noise, and `max_null_pass_rate` has to sit inside
`(0, 0.5]` — a gate a coin flip passes half the time is not a gate. You can make every one of them
*harder* than the default.

Raising the bar is free: `[gate]` is not part of the fingerprint that restarts the gate's window,
because moving the pass mark does not change what a trade meant. Moving a *wall* is not free.
`[risk]`, `[costs]`, `brief.regime_symbol`, `brief.call_threshold_pct` and the model are all in that
fingerprint, so tightening one costs you the record traded under the old one. That is the trade, and
it is the honest way round.

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
them against this specific workload — one call a morning, up to 60,000 characters of messy text in,
strict JSON out — with prices, long-context benchmark scores, latency budgets and structured-output
guarantees. The headline is Claude Opus 5 for quality, Gemini 3.7 Flash for value, and GLM-5.3-Flash
for almost nothing at all — and the spread between the best and the cheapest sane option is about
$93 a year, at prices worked out on a prompt four times the size tape actually sends. At one call a
day, optimise for the briefing being right, not for the bill.

## Commands

```
tape init                write config.toml, the journal, and the default playbook
tape status              ledger, broker balance (labelled ignored), the risk walls, the clock

tape brief               the morning read, one falsifiable call, and 0-3 sized ideas
                         --dry-run, --json, --force
tape briefs              archived briefings, newest first, with --limit
tape briefs show ID      re-render one from the journal; ID or "today"
tape watchlist ls        the symbols the briefing reads
tape watchlist add SYM   add symbols to the watchlist
tape watchlist rm SYM    remove symbols from the watchlist
tape playbook            print the strategy file; --write creates it if missing

tape proposals           the session's slate and what became of it; --day, --reconcile
tape take N              trade idea N as a bracket at the computed size; --qty lowers it
tape pass N              decline idea N; --reason is required
tape why N               levels, thesis, invalidation, status, and the sizing arithmetic

tape score               grade the calls and the watchlist biases, replay every decided idea
                         --through picks the last session to settle
tape stats               the whole report: trades, equity, setups, regimes, reads, vetoes,
                         refusals, significance, the gate
                         --month, --all, --from/--to, --json
tape gate                the significance test and the gate table, over the whole record; --json
tape retro               the weekly review: what the record shows, and exact playbook diffs
                         --weeks, --dry-run, --json
tape retro show ID       re-render an archived review; ID or "latest"
tape retro apply ID      write the edits you name; --diff 1,3 or --all
tape playbook versions   the snapshots the gate reads from, newest first; --limit

tape buy SYM QTY         buy, with --stop (required), --limit, --target, --note
tape sell SYM QTY        sell shares the ledger holds, with --limit and --note
tape cancel ID...        cancel resting orders; --all cancels everything working
tape pos                 open positions from the journal, priced live
tape orders              journaled orders, with --open and --since
tape watch SYM...        stream live quotes until Ctrl-C
tape eod                 flatten, expire the undecided ideas, recap the day, then run `score`

tape mode [paper]        show or set the mode; `live` is refused
tape llm ping            check the configured provider answers
tape llm providers       list the known providers
tape version             version, platform, and Go toolchain
```

`briefs`, `briefs show`, `proposals`, `why`, `pass`, `watchlist`, `playbook`, `playbook versions`,
`stats`, `gate`, `retro show` and `retro apply` read the config and the journal and nothing else, so
they work on a machine with no keys on it at all. Reading the record is not supposed to cost you an
API call:

```console
$ tape briefs

[paper] briefs

no briefings yet; run `tape brief`.

$ tape proposals

[paper] proposals

no proposals for 2026-08-30; run `tape brief`.

$ tape retro show latest

[paper] retro show latest
tape: no reviews archived yet; run `tape retro`
```

A pass is scored later, so it has to say why, and the refusal comes before anything else happens:

```console
$ tape pass 1

[paper] pass 1
tape: pass 1: --reason is required; a pass is scored later and needs to say why
```

Everything that touches the market says plainly when it can't, and says it before doing any work.
That includes the dry run: it asks the model nothing, but assembling the briefing still means
reading quotes and the venue clock.

```console
$ tape brief --dry-run

[paper] brief
tape: alpaca: ALPACA_API_KEY / ALPACA_API_SECRET not set (free paper keys: https://app.alpaca.markets)

$ tape take 1

[paper] take 1
tape: alpaca: ALPACA_API_KEY / ALPACA_API_SECRET not set (free paper keys: https://app.alpaca.markets)
```

`take` is on that list because it places an order. `why` and `pass` are not: reading an idea back
and declining it are decisions about the record, and the record is local.

Every command prints its mode on the first line, so no output is ever ambiguous about which account
it was talking about. That tag is `[paper]`, or `[LIVE — locked]` if you hand-edit `mode = "live"`
into the config: the file can say live, and the banner will still tell you tape is not allowed to
trade it. There is no bare `[LIVE]` anywhere in the binary. `--config` points at a config file
anywhere.

## Roadmap

**Phase 0 — plumbing.** Config, broker adapter, journal, cost model, model layer, manual paper
orders end to end. Done.

**Phase 1 — the briefing.** Data collectors behind provider interfaces, a rule-based regime
classifier, the seeded playbook, and `tape brief` with a scored call of the day, before any trade
proposals exist. Done.

**Phase 2 — co-pilot.** Schema-validated proposals against the playbook's setup ids, sized in Go
from equity and the stop distance and capped by free cash, `take` and `pass` with reasons, the slate
lifecycle in the journal, and the rest of the Go-enforced guardrails: per-trade risk cap, max open
positions, no averaging down, flat by close, stale entries, and a halt once two positions have
closed the day at a gross loss. Done.

**Phase 3 — the mirror.** The counterfactual replay of every decided idea, graded watchlist biases,
`tape stats` with its by-setup, by-regime, veto and execution-drag cuts, the null-trader and
bootstrap tests behind the gate, the playbook-version window the gate reads from, and `tape retro`
with reviewed playbook diffs and an atomic apply. Done.

**Next, and it is not code.** Three or more months of real mornings on paper, through the full
system, with a real model and real market data. Nothing in this repository has met either yet, so
the first thing owed is a smoke run against a real Alpaca paper account and a real provider — one
morning, end to end, watching what breaks. After that it is a routine, not a feature: run it, decide,
score it, review it weekly, and let the record say what it says.

**The gate.** No real money moves until every box ticks, and the boxes were written down before any
results existed so that they cannot be softened afterwards. `tape gate` prints them:

- Three or more months and fifty or more sessions, measured from the last rule change
- One hundred or more closed trades
- Positive expectancy after modeled slippage and commissions, and a bootstrap 95% lower bound on
  that expectancy which is also above zero
- Profit factor of 1.3 or better, maximum drawdown of 10% or less
- A zero-edge trader with the same trade sizes and the same sample size clears those thresholds in
  at most 10% of ten thousand simulated runs
- Zero guardrail breaches in the final month
- At least one playbook rule with ten or more trades and positive expectancy. Funding a mystery is
  gambling.

**Phase 4 — real money**, only if the gate opens *and* a kill switch is written before the first live
order, not after the first bad week. A live adapter behind the existing interface, a small account,
the same guardrails and the same journal. The gate validates the strategy; it cannot validate the
trader, because paper money cannot reproduce what a real loss does to the person holding it. Live
results will be worse than paper, the gate's margins exist partly to absorb that, and the gate can
close again.

If the gate never opens, phase four never runs, and the project still did its job. The full
reasoning, the evidence behind each decision, and what Claude cut are in
[docs/DESIGN.md](docs/DESIGN.md).

## Status

Early, and honest about it. Phase 0 is complete and tested: config and env overrides, the Alpaca
paper adapter, the SQLite journal with FIFO trade matching and day recaps, the cost model, the
provider-agnostic model layer with its three clients — native Anthropic, OpenAI-compatible, and
Claude Code — and the trading engine.

Phase 1 is complete and tested too: the read-only market client over Alpaca's snapshots, daily bars,
one-minute sessions, screener and news; the three calendars; the rule-based regime classifier; the
seeded playbook; `tape brief` with its prompt assembly, strict schema, Go-side validation and
archive; `tape briefs`, `tape score`, `tape watchlist` and `tape playbook`.

So is Phase 2: the proposal half of the schema and its validation, `internal/risk` and the sizing
that goes with it, the slate lifecycle in the journal, the eleven guardrails and the `refusals`
table they write to, and `tape proposals`, `tape take`, `tape pass`, `tape why` and `tape cancel`
around them.

And so is Phase 3, the mirror: `internal/counterfactual` replaying every decided idea against its
own session's minute bars, watchlist biases graded like the call, `internal/stats` computing the
whole report and the ten gate checks, the null-trader simulation and the expectancy bootstrap in
`significance.go`, the `playbook_versions` window that stops the gate grading a fitted rule on the
record that fitted it, and `internal/retro` with its second prompt, its diff constraints, and an
apply that stages, snapshots and commits in one piece. Twenty-four commands. 886 tests, none of
which touch the network: venues, calendars and model endpoints are `httptest` servers asserting
request shape, the briefing, the slate and the review run against an in-memory feed and a fake
model, trading runs against an in-memory fake broker, and the journal uses a temp-file SQLite. The
schema is at version 6.

Not done, and this is the important part: **tape has never been run against a real Alpaca account, a
real calendar API, or a real model.** Every adapter is tested against a fake HTTP server and nothing
more, and no briefing, proposal or review in this repository was written by an actual model — the
ones further up are test fixtures, and they say so. What a real model does with a real morning's
headlines, whether its ideas are worth taking, whether your vetoes help or hurt, and whether any of
it beats a coin over three months, is exactly the thing nobody here knows yet. Every mechanism for
finding out now exists; none of the answers do.

I'm open-sourcing it at this stage because the interesting part is not the trading. It is that the
honest version of this tool — cost-modeled fills, a ledger that ignores the broker, limits in
compiled code, and a gate whose criteria were fixed before any results existed — is a specific and
mostly unglamorous set of engineering decisions, most of them Claude's, and they are all readable
in one afternoon.

## License

MIT — see [LICENSE](LICENSE).
