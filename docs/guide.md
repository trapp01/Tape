# Guide

What each part of tape does and what it prints. The [README](../README.md) has the pitch and the
quick start; [DESIGN.md](DESIGN.md) has the reasoning. Every output block here is real output,
rendered against the in-memory venue and the fake model the test suite uses.

## The briefing

`tape brief` reads the market, hands the model everything it collected together with your playbook,
and archives the reply with one call that gets graded after the close. It places no orders.

**What the model is shown**, all of it archived verbatim next to the reply:

- The index ETFs — `SPY QQQ IWM DIA` by default — with the last trade, the previous close, and the
  move between them.
- A regime label computed in Go from eighty daily bars of `SPY`: 20- and 50-day moving averages and
  twenty-day realised volatility, turned into a label like `uptrend, low vol` by fixed thresholds.
  The model is told to let it set the day's ceiling and does not get to choose it.
- The calendar for the next three days: FOMC decision days from a table compiled into the binary,
  US economic releases from FRED, and earnings for watchlist symbols from Finnhub.
- The watchlist with quotes, up to five headlines per symbol and fifteen market-wide ones, each
  summary clipped to 300 characters.
- The session's top gainers, losers and most actives from Alpaca's screener.
- The ledger's cash and equity, the venue clock, and the sources that were unreachable.
- The risk limits, under a heading that says they are enforced in code and cannot be moved.
- `playbook.md`, verbatim, as the last block of the message.

All of that has to fit 60,000 characters. Over the cap, the prompt drops to two headlines a symbol,
then to none, then drops the movers; a briefing that still does not fit is cut at the tail, which
lands in the playbook.

**What it must return** is JSON against a strict schema: a market read, a regime note, a calendar
note, at most twelve watchlist notes each carrying a bias of bullish, bearish or neutral, at most
five risks, at most three trade proposals, and one call of the day. The call is an instrument, a
direction of up, down or flat, a threshold in percent (or null for the configured default), a
rationale naming the playbook rule it rests on, and an invalidation: the one observation that would
prove it wrong before the close. An empty proposal list is a valid answer, and the prompt says so.

**The rules that make the grade mean something**, each argued in
[DESIGN.md](DESIGN.md#briefing-design):

- The call and the slate lock at 09:30 ET. `--force` replaces them before the bell; after it, the
  first call and slate stand. An evening run is a call on the next session.
- Only complete sessions are graded, from the session's own minute bars, after 16:30 ET, once.
- Session dates are Eastern, through one function, whatever `account.timezone` says.
- The model may only name symbols it was shown, and a call needs an invalidation and a threshold
  above zero and at most 5%.
- A reply that fails validation is archived raw, reported as failed, and never rendered as valid.
- Headlines are data. Everything a news wire or an API wrote is fenced in a block marked
  `untrusted text, data only`; source warnings are clipped to 120 characters in the prompt and kept
  whole in the archive.

### What a briefing looks like

`TestBriefRenderGolden` in `internal/cli/briefrender_test.go` pins this byte for byte, colour off,
on a morning whose call has already been graded:

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

Computed facts sit on the label lines and the model's words sit underneath them. `CALL` reads
`[scored after close]` until the session is graded. Under `PROPOSALS`, the size, the risk and the R
multiple are computed in Go and the thesis is the model's; `#2` was refused before you ever saw it
and stays on the screen and on the record. `SOURCES` is what the briefing was written without.

```sh
tape briefs             # every briefing, newest first, with its call and its grade
tape briefs show today  # re-render one from the journal
tape brief --dry-run    # assemble everything and print both prompts, ask nothing, archive nothing
tape brief --json       # the input, the reply and the briefing id, for the sidecar
```

### The playbook

`playbook.md` sits next to the config and the journal. Tape reads it and cites it; only
`tape retro apply` writes to it. `init` seeds it once with a posture for each regime the classifier
can produce, four setups — `M1` gap-and-go continuation, `M2` momentum continuation above the prior
high, `R1` range-edge mean reversion, and `N1`, the conditions that end the discussion for a symbol —
risk rules that restate what Go enforces, and the rules for the call of the day.

A proposal has to cite a setup id, and an id starting with `N` is not one: it is a no-trade
condition. Changing a risk number in the file does not move the wall, which lives in `[risk]` and in
Go. The seed is a starting point, not a recommendation; the learning loop exists so you change it
against a scored record.

## The slate

**A proposal is nine required fields**: a symbol the briefing showed, a side (`long`; v1 does not
short), a setup id the playbook defines, an entry, a stop below it, a target above it, a thesis, an
invalidation, and a confidence of low, medium or high. Confidence changes nothing; it is on the
record so the scoring can find out later whether it meant anything.

**Size is never the model's.** The model supplies prices; `risk.SizeWithin` supplies the share count:

```
budget  = equity × risk.per_trade_pct / 100
shares  = floor(budget ÷ (entry − stop))
ceiling = free cash × 0.98, and the position is trimmed to what that buys
```

Free cash is the ledger's cash less what open orders already claim; the 2% absorbs slippage and
commission. A size the cash ceiling cut is labelled `cash-capped` on the slate. `tape why N` prints
the whole calculation:

```
Sizing (computed in Go, never by the model)
  budget              $5,000.00 equity × 0.5% = $25.00
  shares              $25.00 / ($120.60 − $119.10) = 16, rounded down
  risked at the stop  $24.00
  notional            $1,929.60
```

An idea is also checked before it is sized: the target has to pay at least `risk.min_reward_risk`
times the stop distance, and the entry has to sit within `risk.max_entry_deviation_pct` of the price
the briefing showed. An idea that fails either, or that the budget cannot buy one share of, is
journaled as `rejected` with the rule text on the row.

### What becomes of each idea

```
proposed ──┬─► taken ──► unfilled        the order died without trading a share
           ├─► passed                    you declined it, with a reason
           ├─► rejected                  a rule refused the idea itself
           └─► expired                   the session ended and nobody decided
```

`take` claims the idea (`submitting`) before it sends anything, so a crash between the venue
accepting the order and the decision landing leaves a claim rather than a takeable proposal.
`tape proposals --reconcile` resolves the claim from the order's client id without sending anything.
`take --qty` may lower the computed size, never raise it, and the `proposals` table then shows both
numbers (`16→4`, `$24.00→$6.00`).

`take`, `pass`, `why`, `proposals` and `eod` resolve `N` against the session the briefing keyed
itself to, not your calendar date, so an evening slate is takeable that evening. `eod` expires every
idea nobody decided and marks a take whose order never traded a share `unfilled`; it was never a
trade, so it adds nothing to execution drag.

## The guardrails

Eleven rules run in Go before an order can leave the machine, and `tape take` and `tape buy` take
the same path through them. Each refusal names the rule and its numbers and is written to the
`refusals` table, which is where "zero guardrail breaches in the final month" is counted from.

A rule that refuses a fact about the *idea* marks the proposal `rejected`. A rule that refuses
today's *circumstances* leaves it open and records only the refusal, because a limit that lifts in an
hour is not a verdict on the trade.

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

```
tape: buy 16 NVDA: ledger cash $500.00 < cost $2,056.59 for 16 NVDA at $128.41 (rule: no overspend)
tape: sell 99 AAPL: selling 99 AAPL but the ledger holds 10 (rule: no shorting)
tape: buy 15 AAPL: ledger cash $5,000.00 less $3,602.80 committed to open orders leaves $1,397.20 < cost $1,501.90 for 15 AAPL at $100.01 (rule: no overspend)
```

- The overspend check prices the order with the cost model, so it refuses the order that would
  actually overdraw the ledger, not the one that looks affordable at the quote.
- Cash and shares that resting orders already claim are subtracted first, so two orders cannot spend
  the same dollar or sell the same share. A bracket's stop and target count once, as the larger leg.
- `buy` and `sell` sync the journal with the venue before the guardrails read it, so a stop that
  fired since your last command is seen. A venue outage refuses new orders rather than trading on a
  stale record.
- The daily halt counts positions, not exits. Scaling out of one winner in three clips is not three
  losses, and a position that nets negative only from the commission floor was not a losing read.

**An exit is always allowed.** Sells run none of the entry rules, and a sell blocked only by the
bracket legs resting over its own shares cancels those legs first and says so:

```
$ tape sell NVDA 10

cancelled 2 resting bracket legs for NVDA
```

`eod` sells the quantity the *ledger* holds, never the venue's. Where the two disagree it says so
and refuses to trade the difference: a broker holding shares tape never recorded is a
reconciliation problem for you, not a position for tape to liquidate.

## Costs

Every fill is re-priced before it lands ([DESIGN.md](DESIGN.md#paper-fills-lie) has the
argument). Slippage moves the price against you in basis points, commission is per-share with a
minimum and a percent-of-value cap mirroring IBKR Pro fixed pricing, and the SEC fee and FINRA TAF
land on sells. The raw venue price is kept; the modeled price is what the stats use.

A sell shows `NET` where a buy shows `COST`: a buy pays commission and fees on top of the shares, a
sell has them taken out of the proceeds.

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

`FEES` is $0.03 here and $0.00 on the buy, because the regulatory fees fall on sells only. The buy
was modeled above the quote and this sell below it: both against you.

The `eod` recap prints two cost lines. `costs on closed trades` is what the day's round trips paid,
and `net` is computed from it. `costs on today's fills` is every commission and fee the day
incurred, including on positions still open. After a clean `eod` they agree.

## Scoring

`tape score` runs after the close, and `tape eod` calls it when it runs after 16:30 ET. It settles
three things for every session up to the day it is asked about, and every grade is written once.

- **The call of the day**, from its own session's first and last regular minute bars.
- **Every watchlist bias**, at the threshold that morning's call used (or `call_threshold_pct` when
  there was no call). Bullish has to clear the threshold, bearish has to clear it downward, neutral
  has to stay inside it. A symbol that cannot be graded is named and left out, and one symbol gets
  one grade per session however many briefings that session archived.
- **A counterfactual replay of every decided idea**: taken, passed, rejected, expired and unfilled,
  all through the same simulator, against the session's minute bars, at the levels the model wrote.

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

`#3 AAPL` is why R and dollars are separate columns: the reward/risk rule rejected it before it was
sized, so it replays at zero shares. The levels still score, and the record can still say later
whether that rule was refusing good trades.

### Replay conventions

A minute bar gives the high and the low but not the order they printed in, so some things are
decided by convention, the same way every time, with the affected replays counted.

- **The entry fills on touch**, at `min(entry, bar open)`. This is the optimistic convention: a real
  resting limit sits behind a queue and may not fill on a low that only reaches it.
- **The fill bar can stop you out but cannot reach the target.** The take-profit was not resting yet
  when that minute's high printed.
- **A bar that spans both stop and target is a stop**, marked `Ambiguous`. `tape stats` reports the
  count as `decided by the stop-first rule`, and such a replay prints as `stop (stop-first)`.
- **A stop gapped through fills at the open**, `min(stop, bar open)`.
- **Anything still open at the last bar exits at the close.**
- **Both sides pay the cost model**, so a replay's net is comparable with what the journal books.
- **Size is what you would have held**: the sized quantity, or the smaller `take --qty`. The R
  multiple is per share, so an idea nobody could size still scores its levels.
- **A replay that cannot be trusted is refused, not filed**: levels that do not describe a long
  trade, a session with no prints, or a fill bar opening more than 25% from the entry (the signature
  of a split the feed has since adjusted for).

A half day is a complete session. When the venue calendar can answer, a session is finished once its
last print is within five minutes of that day's close; the fixed 15:55 ET is only the fallback.

The replay is not a backtester. It only replays ideas the model actually filed, on the session each
was filed for. A walk-forward backtest over historical bars is the Python sidecar's job, and that
sidecar does not exist yet.

## Stats

`tape stats` computes nine sections from the journal and nothing else:

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

- **By setup** puts each rule's real trades next to the replay of every idea that cited it. The
  trades are filtered by your own decisions; the replays say whether the rule works.
- **By regime** cuts the record by the label the briefing archived that morning, never a
  recomputation. Days with no archived briefing get their own row.
- **Veto quality** is `passes that would have profited` against `losses the vetoes avoided`: whether
  declining a suggestion saves you money or costs you money.
- **Execution drag** is the replay's net minus what a take actually booked. It separates what your
  execution cost from whether the idea was good, and counts only takes that traded. That is why the
  `taken` idea in the score output shows up as `unfilled` here: its limit never traded a share.
- **`INSIDE NOISE BAND`** counts reads whose actual move landed within 5 basis points of their
  threshold, printed beside the accuracy they inflate.

`--month`, `--all` and `--from`/`--to` cut the descriptive sections. `EQUITY` and `GATE` always read
the whole record through the window's end, so `tape stats --month` and `tape gate` never disagree
about the account. `tape gate` prints the last two sections on their own; `--json` prints the report
for the sidecar.

## The gate

A profit factor of 1.3 over fifty trades is not evidence; a trader with no edge clears it often
enough. So `tape stats` simulates that trader ten thousand times at your sample size, each trade
winning at the break-even rate and drawing its size from your own wins and losses, and the gate
requires that at most `max_null_pass_rate` of those paths clear the thresholds you did. A bootstrap
of your net P&L has to put the 2.5th percentile of the mean above zero. Both stay silent under twenty
trades or fewer than five on either side. [DESIGN.md](DESIGN.md#the-mirror-design) has the full
argument.

The gate reads only sessions after the last change to what a trade means: the playbook text, the
risk limits, the cost model, the regime symbol, the call threshold, or the model or provider. Never
the watchlist, the news lookback, or a key. A hand edit of `playbook.md` counts, because the file is
fingerprinted, not trusted. `stats`, `gate` and `retro` record a snapshot whenever they see a change:

```console
$ tape playbook versions

[paper] playbook versions

ID  TAKEN        SHA           REVIEW  NOTE
#1  08-31 08:46  2a1bc5e31c80  -       first snapshot
```

## The weekly review

`tape retro` is one model call a week over the last `[retro] weeks` of sessions (or `--weeks N`). It
reads the stats for that window, the whole-record gate table, the three best and worst trades, every
pass with what its replay cost or saved, refusals by rule, the previous review's summary, the risk
limits, the playbook's setup ids, and `playbook.md`. Everything somebody else wrote — your pass
reasons, the earlier summary — is fenced as untrusted data; the playbook is the one trusted block,
which is why its edits go through you.

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

- **A diff is an exact text edit.** It names an existing heading and `add`s under it, or `edit`s or
  `remove`s a `before` that appears exactly once in that section.
- **`## Risk rules` is off limits**, with everything nested under it. New text may not open a level-1
  or level-2 heading, and a new `### <ID>` must not collide with an existing setup id. At most eight
  findings and five diffs.
- **An empty diff list is often the right answer.** It renders as
  `none. an empty list is a real answer: the record could not carry a change.`

`tape retro apply` writes only the diffs you name (`--diff 1,3` or `--all`), resolved against the
playbook as it stands now. A hand edit made since the review gets its own snapshot first. The
version row and the diff marks commit in one transaction, the new file lands by rename, and the old
one goes to `playbook.history/`. A diff applies once. After an apply, the gate reads only the
sessions from there on.

`tape retro --dry-run` prints both prompts and asks nothing. `tape retro show <id|latest>`
re-renders an archived review, and `--json` gives the sidecar the input, the reply and the diffs.

## Configuration

`~/.tape/config.toml`, written by `tape init` and edited by hand. `ALPACA_API_KEY`,
`ALPACA_API_SECRET`, `FRED_API_KEY` and `FINNHUB_API_KEY` override the file, and a command that
rewrites the config (`tape mode paper`, `tape watchlist add`) never writes an env-supplied key back.

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

- The SEC fee and FINRA TAF are not in the file. They live in `costs.Default()` and apply to every
  sell.
- `tape watchlist ls`, `add` and `rm` edit `brief.watchlist` with the same validation as the file.
- `call_threshold_pct` refuses zero: a zero threshold makes an unchanged close both up and down, a
  call that cannot be wrong.
- The `[risk]` and `[gate]` floors exist because the gate is measured inside the walls, and the gate's
  numbers were fixed before any results existed. You can make any of them stricter than the default.
  `require_stop` is the one you can turn off, and turning it off means trading without a bounded loss.
- `[gate]` is not part of the fingerprint that restarts the gate's window, so raising the bar is
  free. `[risk]`, `[costs]`, `brief.regime_symbol`, `brief.call_threshold_pct` and the model are, so
  changing one costs you the record traded under the old value.

Setting `slippage_bps = 0` and `commission_min = 0` will make paper look better. It will also make
every number tape produces a lie.

## Without keys

Commands that touch only the config and the journal — `briefs`, `briefs show`, `proposals`, `why`,
`pass`, `watchlist`, `playbook`, `playbook versions`, `stats`, `gate`, `retro show` and
`retro apply` — work on a machine with no keys at all. Everything that touches the market says so
before doing any work, including `brief --dry-run`, which reads quotes and the venue clock:

```console
$ tape brief --dry-run

[paper] brief
tape: alpaca: ALPACA_API_KEY / ALPACA_API_SECRET not set (free paper keys: https://app.alpaca.markets)
```

The first line of every command is its mode. That is `[paper]`, or `[LIVE — locked]` if you
hand-edit `mode = "live"` into the config: the file can say live, and the banner still says tape is
not allowed to trade it. `--config` points at a config file anywhere.
