# Design

This is the reasoning behind tape: what the evidence says about day trading, what that evidence
forced into the design, and what I chose not to build. The README covers what the tool does. This
document covers why it is shaped the way it is.

The bet behind the repo: **a trading assistant that never predicts, only applies written rules and
grades itself, is worth more than one that claims to know where the market is going.** The second
kind has no evidence behind it. The first kind produces the one thing a beginner cannot get any
other way: an honest record.

---

## The premise, tested

I came to this the way most people do. Trading movies, Gary Stevenson's _The Trading Game_, the
feeling that the whole thing is about information and that an AI might be the best information
collector I could get. Before writing code I went looking for what the research actually says, and
most of the original premise did not survive.

**Retail day traders lose, and the studies are unusually complete.** Barber, Lee, Liu and Odean had
every trade on the Taiwan Stock Exchange from 1992 to 2006. Under one percent of day traders were
predictably profitable after fees. Chague, De-Losso and Giovannetti followed 19,646 new Brazilian
futures day traders. Of the ones who stuck with it for 300 or more sessions, 97% lost money and
1.1% earned more than minimum wage. Persistence made outcomes worse, not better.

**An LLM picking trades does not change that number.** The flashy results (a multi-agent paper
claiming a 6 to 25 percent edge) come from three-month backtests on a handful of tech stocks where
the model had been pretrained on the very news it was "predicting". FINSABER re-ran LLM timing
strategies across two decades and over a hundred symbols with survivorship and data-snooping
controls, and the advantage evaporated: too conservative in bull markets, too aggressive in bear
markets. When six frontier models were each handed real money on crypto perpetuals in late 2025,
most of them drew down hard from over-leveraging, and the dispersion between them looked like coin
flips with leverage, not skill.

**Speed is not available to a person at a laptop.** Firms hold co-located servers, sub-millisecond
round trips, and, through payment for order flow, wholesalers see retail orders before the market
does. "React to news faster" is not a feature a retail tool can honestly offer.

What does have evidence behind it is duller: position sizing, forced journaling, pre-trade rule
checks, and vetoing impulsive trades. That is loss reduction, not alpha. It is also exactly the
category that trade-journaling products already charge for, which told me the journal is the
product, not the AI.

The Stevenson reframe, for what it's worth: his edge was a macro thesis nobody else believed, held
with patience and sized with conviction. The transferable lesson is not "get information faster".
It is "know exactly why you're in a trade and what would make you wrong". That is a journaling
discipline, and tape enforces it.

## What the evidence forced

| Evidence | What it forced |
| --- | --- |
| 97% of persistent day traders lose; under 1% reliably profitable | Paper money first. Quantitative gates before any real dollar. Success is defined so that "you have no edge" still counts as the project working. |
| LLM trade-picking edge disappears in de-biased long-horizon tests | The model never _predicts_. It applies written playbook rules to today's data and cites which rule fired. Its calls are still scored, so if predictive value exists it shows up in numbers rather than vibes. |
| Unsupervised LLM agents drift into oversized, concentrated bets | Co-pilot, not autopilot. A human confirms every order. Risk limits live in Go code the model cannot override or argue with. |
| Retail cannot win on speed | No news-reaction features. The only edges attempted are preparation, selectivity, sizing discipline, and an honest record. |
| Journaling and analytics is a category people pay for | The journal and stats engine is the load-bearing feature. Everything else is built on top of it. |
| OpenBB, the best open-source research terminal, nearly died maintaining 500 data-vendor wrappers | Few providers, every one behind a Go interface. Replacing a dead vendor is one adapter file. |

## The two loops

There is a morning loop and a learning loop, and the second one is the reason this is more than a
chat wrapper.

**Morning.** Market data comes in (quotes, movers, news, the economic and earnings calendars). The
model produces a briefing: market state, regime, what the calendar is about to do, and a falsifiable
call of the day. It proposes zero to three trades, each tied to a named rule in the playbook and
each carrying its stop, target, and size before it carries anything else. You take or pass. Taken
orders go to the broker. Everything, including the passes and the reason for each pass, goes into
the journal.

**Learning.** Nightly, the journal is scored against what actually happened: fills and P&L for
taken trades, counterfactual outcomes for passed proposals, right-or-wrong for the briefing's call
of the day. Weekly, the model reads its own scored record and proposes diffs to `playbook.md`. You
review those diffs like a pull request. The playbook constrains the next morning's briefing.

That last edge, playbook to briefing, is the only way strategy changes. The model does not
improvise. It applies rules, and the rules evolve only through scored history that you approved.

The detail most journals miss is on the pass side. If you veto a proposal and it would have made
money, that is recorded. If the model proposes something you decline and it would have lost, that is
recorded too. Over months this answers a question no amount of reading can: are your vetoes helping
or hurting?

## Co-pilot rules

These are enforced in code, not in prompts, because a prompt is a suggestion and a type system is
a wall.

- **No order without confirmation.** The model can propose. Only `tape take` transmits.
- **Risk limits are Go.** Per-trade risk capped at a small fraction of the account, a cap on open
  positions, no averaging down, flat by close, and a daily halt once two positions have closed the
  day at a gross loss. The model has no code path around any of them.
- **Every proposal carries its exit before its entry.** Entry, stop and target are all required by
  the schema, and a proposal missing one is rejected before a human sees it. Size is not on that
  list: the model never supplies it, Go computes it.
- **Every briefing makes falsifiable calls.** Direction for the day, a thesis per setup, a condition
  that would invalidate it. Nothing so vague it cannot be wrong.

## Architecture

```
cmd/tape             the binary
internal/cli         cobra commands: thin, one function call each
internal/config      ~/.tape/config.toml, env overrides for secrets
internal/broker      Broker + MarketData interfaces; alpaca/ is the paper adapter
internal/journal     SQLite: orders, fills, trades, ledger, day recap, briefings, calls,
                     proposals and their decisions, and every guardrail refusal
internal/costs       slippage, commission, and regulatory-fee model applied to every fill
internal/llm         Provider interface; native Anthropic plus one OpenAI-compatible client
internal/risk        position sizing and the limits every entry is checked against
internal/trading     orchestration: submit, sync fills, cancel, flatten, and the guardrails
internal/market      read-only data contracts; alpacadata/ reads snapshots, bars, sessions, news
internal/calendar    scheduled-event contracts; fomc/ fred/ finnhub/ are the sources
internal/regime      the market label, computed from daily bars by fixed rules
internal/playbook    reads the strategy file, and writes the seed once
internal/brief       assembles the briefing, builds the prompt, validates and scores the call
```

**Go for the tool, Python for research.** Go gives a single static binary, a maintained official
Alpaca SDK, and the concurrency a quote stream wants. What Go does not have is a research stack:
there is no pandas, no vectorbt or backtrader-class portfolio backtester with walk-forward and
realistic fills, and every piece of prior art in this space is Python. So the tool you touch is
Go, and when a backtest is ten times easier in Python, a sidecar script is permitted. The rule is
that you never have to know it is there.

**One SQLite file.** Briefings, proposals, decisions, fills, and scores all live in `~/.tape/tape.db`.
Trivial to back up, trivial to query, trivial to hand to the Python sidecar. Stats are computed from
this file and never from the broker, because the broker's numbers do not include the cost model or
the ledger size you actually care about.

**The broker is an interface.** `broker.Broker` covers account, positions, orders, and flattening.
`broker.MarketData` covers quotes and streams. The paper adapter is Alpaca. A live adapter is a new
package behind the same interface, and the CLI does not change. This seam is the whole reason
"paper first, real money later" is a real path rather than a rewrite.

**The strategy is a markdown file in git.** `playbook.md` is human-readable, diffable, and versioned.
Every change to it is a commit, which means the strategy has a history you can line up against the
stats. When the model proposes a rule change, the diff is the proposal.

## Any model, one interface

The LLM layer is provider-agnostic on purpose. `llm.Provider` is a single `Complete` call that takes
a system prompt, messages, and an optional JSON schema, and returns text plus token counts. Three
implementations exist:

- **Anthropic, native**, through the official Go SDK.
- **OpenAI-compatible**, one client with a configurable base URL and named presets: OpenRouter,
  Z.ai (GLM) direct, DeepSeek, OpenAI, Groq, and a local Ollama. A custom base URL covers anything
  else that speaks the chat-completions shape.
- **Claude Code**, which shells out to the locally installed CLI in headless mode so development
  runs on your own subscription instead of API credits; personal use on your own machine only, since
  Anthropic does not permit offering a claude.ai login to third parties (see `docs/models.md`).

The reason is the learning loop. If every proposal and every briefing call is scored, then swapping
the model behind the briefing and comparing the scores is a real experiment instead of an opinion.
The journal records which model produced each proposal so those comparisons are possible later.

## Paper fills lie

A paper venue fills you at the quote with no commission, no slippage, and no queue. A small account
trading through a real broker does not get any of that, and the difference is large enough to flip a
strategy from positive to negative expectancy.

So tape applies a cost model to every fill before it enters the journal: slippage against you in
basis points, a per-share commission with a minimum and a percentage cap that mirrors IBKR Pro
pricing, and the SEC and FINRA fees a real sell incurs. The fill the broker reported is kept as the
raw price; the modeled price is what the stats use. This is the honesty layer, and it went in
first.

The ledger has the same problem. Alpaca hands out a hundred thousand dollars of paper money, which
tells you nothing about how you would behave with an account you could actually fund. Tape's ledger
starts at the equity you configure and computes cash, P&L, and position sizing from its own fills.
The broker's balance is ignored.

## Briefing design

`tape brief` is one model call a morning, and its entire value is that the call it makes can be
graded later without argument. Five decisions carry that, and three of them are there because the
obvious implementation gets them wrong in a way that only shows up in the scored record.

**Session dates are Eastern.** One function, `market.SessionDate`, answers "which trading day is
this" for every briefing, call, and score, in `America/New_York`. Alpaca stamps a daily bar at
midnight Eastern; read from Mountain time that timestamp is 22:00 the previous day, so a briefing
dated in the trader's own zone files calls under the wrong session and then grades them against the
wrong bars. The configured `account.timezone` is for display and day recaps, and for nothing that
decides which session something belongs to.

**The call of the day locks at the open.** A session carries exactly one call, held by a unique
index on the briefing and a first-wins check on the day. Before 09:30 Eastern, `tape brief --force`
replaces it; after the bell, the first call stands and the second briefing is labelled a second
read. A run in the evening is a call on the next session, keyed to that day, and the morning says
when it was written. Without the lock, a re-run at 11am would be a prediction about a session that
has already given away half its answer.

**Grade only complete sessions.** A call is scored from the first and last regular one-minute bars
of its own session, and only after 16:30 Eastern: the 16:00 bell plus the free REST feed's
fifteen-minute delay. A daily bar is refused as a source because the venue may still be building
it, and grading a half-built session writes a permanent verdict for a day that had not finished
happening. `journal.ScoreCall` refuses a second score on a row that already carries one, so a wrong
grade is a bug to fix rather than something to quietly re-run.

**`ValidateAgainst` checks the reply against the briefing's own data.** The JSON schema is what the
model was asked for; this is what the journal will accept. It rejects a call or a watch note naming
a symbol that was not in the indexes or the watchlist, a lowercase ticker, a missing rationale, a
missing invalidation, and a threshold at or below zero or above 5%. A call on a symbol the briefing
never showed cannot be graded against a session it was never about, and a zero threshold makes an
unchanged close count as both up and down and never flat — a call that cannot be wrong.

**A reply that fails validation is archived and refused, never reused.** The raw text is written to
the journal, the command exits non-zero naming the briefing id, and no call is filed. Re-reading
that briefing runs the same validation and fails the same way. Keeping the rejected reply and
rendering it anyway is the tempting shortcut, and it is wrong: a reply that could not be trusted
once does not become trustworthy by being read a second time.

### Co-pilot design

The briefing's proposals are where a model's output stops being commentary and starts being an
order. Nine decisions carry that seam, and most of them exist because the obvious implementation
loses something the record later needs.

**Size is Go's, never the model's.** The model returns prices and prose; `risk.SizeWithin` turns
ledger equity, the per-trade percent and the stop distance into a share count. A schema-valid JSON
object with a fabricated share count looks exactly like a good one, which is the failure mode
`docs/models.md` is written around.

**Cash is a second ceiling.** Risk sizes first, then the position is trimmed to what free cash — the
ledger's cash less what open orders already claim, and less 2% of headroom for slippage and the
commission — can actually buy. A $5,000 ledger cannot buy $8,000 of stock, and a slate that prints a
size the account cannot pay for is a slate the overspend rule refuses at the till.

**Only facts about the idea reject it.** A refusal on the idea's own numbers — no stop, a target
under the entry, a malformed order, a short — marks the proposal `rejected`, because those numbers
will not change. A refusal about the moment — the risk cap, the position limit, the clock, the
daily halt, the cash, a stale level — leaves it `proposed` and lives only in `refusals`, because a
cap that lifts in an hour is not a verdict on the trade. A rule the map does not recognise counts as
situational, so a guardrail added later cannot silently decide an idea forever.

**`take` claims before it submits.** The row moves `proposed → submitting` before the order leaves
the machine, so a crash between the venue accepting and the decision landing leaves a claim rather
than a takeable proposal. The claim is resolved from the order's client id — which carries the
proposal number — by `Sync` and by `tape proposals --reconcile`, and a claimed idea can never be
taken twice.

**A stale level is re-checked at take time.** The entry is measured against the live quote when the
order is placed, not only when it was proposed: a level written before the bell is a number and not
a plan by 11am. An entry the tape has already traded through its own stop is refused outright, since
filling it would open a position the stop leg exits immediately.

**An exit is always allowed.** Sells run none of the entry guardrails, and a manual sell blocked only
by the resting bracket legs standing over the same shares cancels those legs first and says so. A
rule that traps the trader in a position is worse than the one it enforces.

**The halt counts positions, not exits.** A day is over once `max_daily_losses` *symbols* — two by
default — have closed at a gross loss. Scaling out of one winner in three clips is not three losses,
and a position that nets negative only because the commission floor landed twice was not a losing
read.

**The slate day is the session day.** `take`, `pass`, `why`, `proposals` and `eod` resolve the number
the trader typed against the session the briefing keyed itself to — the next open while the market is
shut — never against the reader's calendar date. An evening briefing is a call and a slate on
tomorrow, and it has to be takeable that same evening or the ideas expire unacted.

**N-rules cannot be cited.** `ValidateAgainst` accepts only setup ids the playbook defines whose id
does not start with `N`. An N-rule is a no-trade condition, so a proposal citing one is arguing for
the trade its own rule forbids.

## Phases and the gate

0. **Plumbing — complete.** Config, broker adapter, journal, cost model, LLM layer, manual paper
   orders end to end.
1. **The briefing — complete.** Data collectors behind provider interfaces, `tape brief` with a
   scored call of the day, before any trade proposals exist.
2. **Co-pilot — complete.** Schema-validated proposals citing the playbook's setup ids, sized in Go,
   `take` and `pass` with reasons, and the rest of the Go-enforced guardrails.
3. **The mirror — next.** Nightly scoring including counterfactuals, `tape stats`, weekly `tape
   retro` with playbook diffs. Then the actual experiment: three or more months of real mornings on
   paper.

Phase 1 contains: a read-only market client over Alpaca's snapshots, daily bars, one-minute
sessions, screener and news; three calendars (FOMC from a compiled-in table, US economic releases
from FRED, watchlist earnings from Finnhub), each optional and each degrading to a named warning;
the rule-based regime classifier; `playbook.md`, seeded once by `tape init` and never rewritten;
`tape brief` with its prompt assembly, strict output schema, Go-side validation and verbatim
archive; and `tape briefs`, `tape score`, `tape watchlist` and `tape playbook` around it. Schema
version 2 adds the `briefings` and `calls` tables.

Phase 2 contains: the proposal half of the briefing schema, with the same Go-side validation the
call gets — long only, every price present and on its own side of the entry, one idea per symbol, a
symbol the briefing actually showed, and an entry setup id the playbook defines; `internal/risk`,
which sizes each idea from equity, the per-trade percent and the stop distance and caps it at what
free cash can buy; the slate lifecycle in the journal — `proposed → submitting → taken | passed |
rejected | expired | unfilled` — with `tape proposals`, `tape take`, `tape pass`, `tape why` and
`tape cancel` around it; the eight entry guardrails in `internal/trading` (no entry without a stop,
target above entry, stale entry, risk cap, max positions, no averaging down, flat by close, daily
halt), each of which writes its rule, symbol and numbers to `refusals`; the exit path that clears a
bracket's resting legs so a sell is never trapped by the order that opened the position; and `eod`
expiring the ideas nobody decided and marking a take whose order never traded a share `unfilled`.
Schema version 3 adds the `proposals` and `refusals` tables and `orders.proposal_id`; version 4 adds
the quantity and risk a `--qty` take actually sent, and `orders.parent_order_id`, so a
one-cancels-other bracket pair counts as one claim on the shares instead of two.

What none of it has done is meet reality. Every adapter is tested against a fake HTTP server, the
briefing and the slate against an in-memory feed, an in-memory venue and a fake model. Nothing here
has run against a real Alpaca account, a real calendar API, or a real model, so nothing in the
repository is evidence about how good the reads are — which is the whole reason the call is scored
rather than shown off.

Between three and four sits the gate. No real money moves until every box ticks:

- Three or more months and fifty or more sessions through the full system
- Positive expectancy after modeled slippage and commissions
- Profit factor of 1.3 or better, maximum drawdown of 10% or less
- Zero guardrail breaches in the final month
- The stats identify _which_ playbook rules carry the edge. Funding a mystery is gambling.

4. **Real money**, only if the gate opens. A live adapter behind the existing interface, a small
   account, the same guardrails and the same journal. Live results will be worse than paper; the
   gate's margins exist partly to absorb that, and the gate can close again.

If the gate never opens, phase four never runs, and the project still did its job.

## What I cut

**Full autonomy.** Deferred, not rejected. The evidence on unsupervised LLM trading agents is
consistent: they concentrate, over-leverage, and blow up. Autonomy is something the record can earn
one guardrail at a time, not something the first version assumes.

**"The AI tells me where the market is going" as the product.** Kept only as a scored experiment,
the call of the day. If it turns out to have predictive value, the stats will say so. Building on
that assumption would mean building on the one claim the evidence rejects.

**Options, crypto, and shorting in the first version.** Options are where beginners lose fastest
and they multiply every mistake. Crypto has thin qualitative information and never closes, which
fights a morning routine. Shorting is one more way to be wrong while learning. All three fit behind
the same interfaces later.

**An MCP server as the delivery vehicle.** A standalone CLI is the right shape for a ritual that
happens at the same time every morning. Exposing the journal over MCP so other tools can query the
record is a nice later addition, not the foundation.

## Non-goals

- Beating the market. The tool's job is to find out, cheaply and honestly, whether the person using
  it has an edge. "No" is a valid and useful answer.
- Financial advice. Nothing here is a recommendation. It is an engineering plan for measuring
  something.
- High-frequency anything. If a strategy needs the first hundred milliseconds after a print, it
  needs different hardware and a different life.
- Supporting every broker and every data vendor. Few adapters, well-tested, replaceable.

If you are in Canada: not in a TFSA (a trading business inside one is taxed on the whole thing),
frequent trading gains are business income, and IBKR is the practical broker with real API order
entry. If you are in the US, the pattern-day-trader rule was retired in 2026 and replaced by
risk-based intraday margin; check your broker's current policy.

## Sources

- Barber, Lee, Liu and Odean, day trading on the Taiwan Stock Exchange; Chague, De-Losso and
  Giovannetti, Brazilian day traders. Summary with figures:
  https://www.currentmarketvaluation.com/posts/the-data-on-day-trading.php
- FINSABER, LLM timing strategies over two decades: https://arxiv.org/abs/2505.07078
- TradingAgents (the multi-agent paper and its limits): https://arxiv.org/abs/2412.20138
- TradeTrap, stress-testing LLM trading agents: https://arxiv.org/abs/2512.02261
- Alpha Arena, frontier models with real money: https://nof1.ai/
- Payment for order flow: https://en.wikipedia.org/wiki/Payment_for_order_flow
- OpenBB's terminal sunset and open-sourcing:
  https://openbb.co/blog/sunsetting-openbb-terminal-why-how-and-what-now/ and
  https://openbb.co/blog/openbb-belongs-to-everyone/
- FINRA on the retired pattern-day-trader rule:
  https://www.finra.org/rules-guidance/notices/26-10
- Alpaca paper trading: https://docs.alpaca.markets/docs/paper-trading
- Alpaca Go SDK: https://github.com/alpacahq/alpaca-trade-api-go
