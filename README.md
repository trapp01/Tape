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

The whole loop is built and tested: the briefing, the trade slate, the guardrails, the scoring, the
stats and the weekly review. None of it has run against a real Alpaca account, a real calendar API,
or a real model yet — see [Status](#status). There is no live-broker code path: `tape mode live` is
refused, and no flag, environment variable, or config value changes that.

## Why I built it

I wanted to find out whether I could trade, and I wanted the answer to be a number rather than a
feeling. The obvious way to get there is to point a good model at the market every morning and ask
what to buy. That premise did not survive the research Claude did before anything was built. It
stops working the moment you ask what you would do with the answer:

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

**Who decided what.** The split is the one in
[Getting Out of the AI's Way](https://matt-trapp.com/posts/getting-out-of-the-ais-way/). Four
things are mine: the goal above, the playbook, the ledger that starts at $5,000 rather than
Alpaca's $100,000, and the gate — paper only until a bar written down in advance opens. Claude did
the research, wrote [docs/DESIGN.md](docs/DESIGN.md) off what it found and its own
[CLAUDE.md](CLAUDE.md) off that, and made the engineering decisions: the journal as the source of
truth, the cost model, the guardrails in Go, the provider layer, the replay conventions, and the
statistics behind the gate. A supervising agent with parallel subagents built the packages against
frozen contracts. I review every diff, and nothing is committed until I have read it. Where these
docs explain why something is built the way it is, that is Claude's reasoning, kept as written,
unless it says otherwise.

**Influences:**

- Gary Stevenson's _The Trading Game_: know why you are in a trade and what would make you wrong.
- The [Taiwan and Brazil day-trader studies](https://www.currentmarketvaluation.com/posts/the-data-on-day-trading.php),
  which set the bar.
- [FINSABER](https://arxiv.org/abs/2505.07078), [TradingAgents](https://arxiv.org/abs/2412.20138),
  [TradeTrap](https://arxiv.org/abs/2512.02261) and [Alpha Arena](https://nof1.ai/): the LLM edge
  evaporates under de-biased tests.
- [Tradervue](https://www.tradervue.com/) and [TraderSync](https://tradersync.com/): the category
  that already works, and it predicts nothing.
- [OpenBB](https://openbb.co/blog/sunsetting-openbb-terminal-why-how-and-what-now/): the warning
  about vendor wrappers.
- Shadow, then score, then gate, which is how I have shipped a forecaster before. Paper is the
  shadow, the nightly scoring is the scoring, and the gate is the decision.

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

The detail most journals miss is the pass side. Every proposal is journaled whether or not it is
taken, with the reason it was declined, and the ones you declined get counterfactually scored. Over
months that answers a question no amount of reading will: are your vetoes helping or hurting?

The model is held to four rules, and none of them are stylistic:

- **It never places an order.** `internal/llm` imports neither `internal/broker` nor
  `internal/trading`, and there is no auto-take flag. The only thing that transmits is a command you
  typed.
- **It never does the arithmetic that matters.** Share counts, risk dollars, R multiples and every
  grade are computed in Go from the proposal's prices and the session's bars, never read out of the
  reply.
- **It never argues with a guardrail.** Eleven limits run in `internal/trading` before an order
  leaves the machine. If a prompt in this repo ever contains the words "you may override", it is a
  bug.
- **It never sees a secret.** Keys resolve from the environment and go into no prompt, no log, and
  no journal row.

The details — what the briefing is shown, how a proposal is sized, every guardrail, the replay
conventions, the stats and the gate — are in [docs/guide.md](docs/guide.md). The reasoning behind
them is in [docs/DESIGN.md](docs/DESIGN.md).

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

```sh
tape init                                         # config, journal and playbook in ~/.tape
export ALPACA_API_KEY=... ALPACA_API_SECRET=...   # free paper keys: https://app.alpaca.markets
export ANTHROPIC_API_KEY=...                      # or whichever provider you configured
export FRED_API_KEY=... FINNHUB_API_KEY=...       # optional, free: economic and earnings calendars
tape status                                       # ledger, broker balance, risk walls, clock
```

The ledger starts at $5,000 whatever Alpaca's paper balance says. `init` writes `playbook.md` once
and never again, because the strategy file is yours. `$TAPE_HOME` moves everything somewhere other
than `~/.tape`. Every config key is annotated in [docs/guide.md](docs/guide.md#configuration).

The ritual is `brief` before the open, one decision on each idea it puts up, and `eod` at the close:

```sh
tape brief                        # the read, one graded call, and 0-3 sized ideas — before the open
tape why 1                        # everything behind idea 1, sizing arithmetic included
tape take 1                       # trade idea 1 as a bracket, at the size Go computed
tape pass 2 --reason "gap already ran"
tape eod                          # flatten, expire what you never decided, recap, score
```

A pass will not go through without a reason, because the pass side is what gets scored later.
`eod` scores the day itself when you run it after 16:30 ET. Once a week, when the sessions have
piled up:

```sh
tape stats --month                # what the record says, and where it stands against the gate
tape retro                        # the review, and the playbook edits it proposes
tape retro apply 3 --diff 1       # write the ones you agree with
```

Manual orders go through `tape buy SPY 1 --stop 511.00` and `tape sell`, past the same guardrails.

## A round trip

Real output, rendered through the in-memory venue the tests use rather than a live Alpaca account.
Every number comes from the same code path a real fill takes.

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

`RAW` is what the venue reported. `MODELED` adds five basis points of slippage against you. The
$1.00 is the commission minimum: at half a cent a share, ten shares earn five cents of commission,
and the floor charges twenty times that. The stock then runs to $110 and `eod` closes it:

```console
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

The paper venue's arithmetic says $100.00. Tape says $96.92 — 3% of the move gone on a winner, and
all of it on a scratch. That difference is the reason `internal/costs` was the first package
written, and the reason no stat in tape ever reads the broker's numbers.

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

Every command prints its mode on the first line. Commands that touch only the config and the
journal (`briefs`, `proposals`, `why`, `pass`, `stats`, `gate`, `retro show` and the like) need no
keys at all.

## Models

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

`claude-code` shells out to `claude -p` on your own subscription: personal use on your own machine
only, since Anthropic does not permit offering a claude.ai login to third parties.
[docs/models.md](docs/models.md) ranks twenty models against this workload; at one call a day,
optimise for the briefing being right, not for the bill.

## Status

Phases 0 through 3 are built and tested: the paper plumbing, the morning briefing, the co-pilot and
the mirror. The test suite never touches the network: venues, calendars and model endpoints are
`httptest` servers, trading runs against an in-memory broker, and the journal uses a temp-file
SQLite.

Not done, and this is the important part: **tape has never been run against a real Alpaca account, a
real calendar API, or a real model.** No briefing, proposal or review in this repository was written
by an actual model; the ones in these docs are test fixtures. Whether a real model's ideas are worth
taking, whether your vetoes help, and whether any of it beats a coin over three months is exactly
what nobody here knows yet. The next phase is not a feature. It is one end-to-end smoke run against a
real paper account and a real provider, then three or more months of real mornings.

**The gate.** No real money moves until every box ticks, and the boxes were written down before any
results existed. `tape gate` prints them:

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

Real money comes only if the gate opens *and* a kill switch is written before the first live order.
If the gate never opens, that phase never runs, and the project still did its job. The phases, the
evidence behind each decision, and what Claude cut are in [docs/DESIGN.md](docs/DESIGN.md).

## Disclaimer

Tape is a research tool, not investment advice. It reads public market data and news, asks a
language model what it makes of them, and writes down the answer. The model is often wrong, the data
is sometimes wrong, and neither of them knows anything about your finances.

Run it in paper mode until you have enough entries in the journal to judge it, and read the source
before you trade on anything it says. The parts that decide whether the numbers mean anything are
small enough to read in a sitting: `internal/costs`, `internal/risk`, the guardrails in
`internal/trading/rules.go` and `internal/trading/entry_rules.go`, the replay in
`internal/counterfactual`, and the null trader and bootstrap in `internal/stats/significance.go`.

## License

MIT — see [LICENSE](LICENSE).
