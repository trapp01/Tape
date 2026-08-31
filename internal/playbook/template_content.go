package playbook

const defaultTemplate = `# Playbook

Tape reads this file and nothing else for strategy. The morning briefing applies
these rules and cites them by id. You edit it; tape never rewrites it on its own.

## Posture by regime

The regime label is computed from daily bars, not chosen by the model. Read it
first and let it set the day's ceiling.

**Uptrend, low vol.** Momentum continuations are live. Normal size. Prefer the
cleanest setup of the morning over the most exciting one.

**Uptrend, high vol.** Half size. Put the stop outside the noise instead of under
the last candle, and move the target out to match so the ratio holds. Pass on
anything that has already gapped more than 4%.

**Sideways.** Stand aside by default. The only trade is R1 at a range edge the
tape has already respected twice. No continuations in chop.

**Downtrend.** No new longs. Shorting is not enabled, so the session is
observation only: write the call of the day, journal the trades you would have
taken, and place nothing.

## Setups

Every proposal names one id from this list. A proposal with no id is not a setup
and gets rejected.

### M1 gap-and-go continuation

- When: the open gaps more than 1% above the prior close on heavier than average
  volume, and the first five minutes hold above the opening range high.
- Entry: the break of the opening range high, after it has held once.
- Invalidation: any trade back below the opening range low.
- Target: the prior swing high, or 2R, whichever comes first.
- Size: the per-trade risk in Risk rules, and no more.

### M2 momentum continuation above prior high

- When: the regime is an uptrend, price is above the 20-day average, and the
  symbol takes out yesterday's high in the first two hours.
- Entry: the retest of yesterday's high once it has turned into support.
- Invalidation: a close back under yesterday's high on the entry timeframe.
- Target: the measured move from the base that produced the breakout, or 2R.
- Size: the per-trade risk in Risk rules, and no more.

### R1 range-edge mean reversion

- When: the regime is sideways, the range is at least 1% wide, and the edge being
  traded has held twice already today.
- Entry: rejection at the edge, taken back toward the middle of the range.
- Invalidation: a trade through the edge by more than a quarter of the range.
- Target: the midpoint of the range. Take it; do not hold for the far edge.
- Size: half the per-trade risk in Risk rules. Range trades fail in clusters.

### N1 no-trade conditions

Any one of these ends the discussion for that symbol today.

- Earnings for the symbol today, or tomorrow before the open.
- A high-impact economic release inside the next 30 minutes.
- Quoted spread wider than 15 basis points of price.
- The setup only appears if you squint. If it needs an argument, it is not one.

## Risk rules

Tape enforces these in code, from config. Restating them here is what lets the
briefing plan inside them; changing a number in this file does not move the wall.

- Risk 0.5% of ledger equity per trade. Size comes from the stop distance, never
  from conviction.
- At most 3 open positions at once.
- No averaging down. A position that is losing is never added to.
- Flat by the close. Nothing is held overnight.
- After two stopped-out losses in one day, the day is over. No revenge trade.

## Call of the day

The briefing makes one call every morning and it gets graded after the close,
whether or not a single trade was taken. This is the record that says whether the
reads are worth anything.

- One instrument. Name it, uppercase, and pick the one the day actually hinges
  on rather than the one that is easiest to be right about.
- One direction: up, down, or flat, measured from today's open to today's close.
- A threshold in percent, or null to take the default from config. Set it higher
  than the default only when the thesis genuinely needs a big move.
- A rationale that cites the setup id or the regime posture it rests on.
- The invalidation: the specific thing that, if seen before the close, means the
  call was wrong. Not a hedge, not a range. One observation.
`
