# Choosing a model for Tape

Tape talks to one model, once a morning. That single call is the whole product surface, so the
choice matters more here than in a chat app where a bad answer costs you a retry.

Researched 2026-08-30. Prices move; the [Sources](#sources) at the bottom are all primary and
worth re-checking before you trust a number in here.

---

## The workload

One request per trading day:

| | |
|---|---|
| Input | headlines, quotes, calendars, and a written strategy playbook, capped at 60,000 characters |
| Output | at most 4,096 tokens, a **strict JSON object** — the briefing and one falsifiable call today, plus 0–3 proposals with numeric fields from Phase 2 |
| Frequency | ~252 requests a year |
| Deadline | under ~2 minutes, before the open |

**The costs in this document are a ceiling, not a measurement.** Every per-briefing figure below is
worked at 60,000 input tokens and 3,000 output, which is what the workload was sized for before it
was built. What Phase 1 sends is bounded by `brief.MaxPromptChars`, 60,000 *characters* — nearer
15k tokens, and today a good deal less than that: the system prompt is about 1,800 characters and
the seeded playbook about 4,100, with headlines and movers trimmed by a ladder if the rest of the
feed threatens the cap. A briefing today therefore costs something like a quarter of the table.
Budget at the ceiling anyway; the playbook and the news feed are what grow into it, and the ranking
does not move.

Five things decide the ranking, in this order:

1. **Grounded reasoning over messy text.** The model must reason from the context it was handed,
   not from what it remembers about a ticker.
2. **Not inventing numbers.** A fabricated price level in a proposal is worse than no proposal.
3. **Reliable JSON.** `Request.JSONSchema` is part of `internal/llm`'s contract. A provider that
   enforces the schema by constrained decoding is categorically better than one that is merely
   asked nicely and then validated.
4. **Long context that actually works at 60k**, not a 1M number on a spec sheet.
5. **Cost and latency**, last — because at one call a day, *everything* on this list is cheap.

That last point is the one people get wrong. The spread between the best and the cheapest sane
model here is about **$93 a year**. Optimise for the briefing being right, not for the bill.

---

## Ranked shortlist

Cost per briefing = 60,000 input + 3,000 output tokens at the provider's own list price, no
caching, no batch. Annual = ×252 trading days.

| # | Model | OpenRouter slug | In / Out $ per 1M | Context | JSON schema | Per briefing | Per year |
|---|---|---|---|---|---|---|---|
| 1 | **Claude Opus 5** | `anthropic/claude-opus-5` | 5.00 / 25.00 | 1M | Constrained decoding | **$0.375** | $95 |
| 2 | **Gemini 3.7 Flash** | `google/gemini-3.7-flash` | 0.75 / 3.75 | 1M | Native `responseSchema` | **$0.056** | $14 |
| 3 | **GPT-5.6 Sol** | `openai/gpt-5.6-sol` | 4.00 / 20.00 | 1.05M | Strict Structured Outputs | **$0.300** | $76 |
| 4 | **Kimi K3** | `moonshotai/kimi-k3` | 3.00 / 15.00 | 1.05M | `structured_outputs` | **$0.225** | $57 |
| 5 | **Claude Sonnet 5** | `anthropic/claude-sonnet-5` | 2.00 / 10.00 | 1M | Constrained decoding | **$0.150** | $38 |
| 6 | **GLM-5.3** | `z-ai/glm-5.3` | 1.40 / 4.40 | 1.31M | `structured_outputs` | **$0.097** | $25 |
| 7 | **GPT-5.6 Terra** | `openai/gpt-5.6-terra` | 2.00 / 12.00 | 1.05M | Strict Structured Outputs | **$0.156** | $39 |
| 8 | **Gemini 3.1 Pro** | `google/gemini-3.1-pro-preview` | 2.00 / 12.00 | 1M | Native `responseSchema` | **$0.156** | $39 |
| 9 | **GLM-5.3-Flash** | `z-ai/glm-5.3-flash` | 0.075 / 0.25 | 1.31M | `structured_outputs` | **$0.005** | $1.32 |
| 10 | **Qwen3.8 Max** | `qwen/qwen3.8-max` | 2.00 / 6.00 | 1M | `structured_outputs` | **$0.138** | $35 |
| 11 | **Grok 4.6** | `x-ai/grok-4.6` | 2.00 / 6.00 | 500k | `structured_outputs` | **$0.138** | $35 |
| 12 | **DeepSeek V4 Pro** | `deepseek/deepseek-v4-pro-0813` | 1.32 / 3.96 peak | 1M | `structured_outputs` | **$0.091** | $23 |
| 13 | **Claude Haiku 4.5** | `anthropic/claude-haiku-4.5` | 1.00 / 5.00 | 200k | Constrained decoding | **$0.075** | $19 |
| 14 | **MiniMax M3** | `minimax/minimax-m3` | 0.30 / 1.20 | 1M | `structured_outputs` | **$0.022** | $5.44 |
| 15 | **Qwen3.8-27B** | `qwen/qwen3.8-27b` | 0.425 / 2.55 | 1M | `structured_outputs` | **$0.033** | $8.35 |
| 16 | **DeepSeek V4 Flash** | `deepseek/deepseek-v4-flash-0731` | 0.44 / 1.32 peak | 1.31M | `structured_outputs` | **$0.030** | $7.66 |
| 17 | **GPT-5.6 Luna** | `openai/gpt-5.6-luna` | 0.20 / 1.20 | 1.05M | Strict Structured Outputs | **$0.016** | $3.93 |
| 18 | **Mistral Large 2512** | `mistralai/mistral-large-2512` | 0.50 / 1.50 | 262k | `structured_outputs` | **$0.035** | $8.69 |
| 19 | **Llama 4 Maverick** | `meta-llama/llama-4-maverick` | 0.20 / 0.80 | 1M | `structured_outputs` | **$0.014** | $3.63 |
| 20 | **gpt-oss-120b** | `openai/gpt-oss-120b` | 0.037 / 0.17 | 131k | `structured_outputs` | **$0.003** | $0.69 |

Also available and deliberately not ranked: **Claude Fable 5** (`anthropic/claude-fable-5`,
$10/$50, $0.75 a briefing) is Anthropic's most capable model and tops AA-Omniscience, but it is
twice the price of Opus 5 for a job Opus 5 already leads on. **GPT-5.6 Cyber** ($12.50/$75) is a
security-specialist model. **Grok 4.20** (`x-ai/grok-4.20`, $1.25/$2.50, 2M context) is the
cheapest genuinely-huge-context option if you ever want to stop summarising the news feed.

### Notes per row

- **Opus 5** — leads Artificial Analysis's Finance & Accounting index (57, joint first) and is
  second on the AA-Omniscience Index (37, behind only Fable 5's 43). Mid-pack on the one benchmark
  closest to this workload, AA-LCR (75.7%). See [the caveat about its tokenizer](#the-anthropic-tokenizer-caveat) — the real bill is nearer $0.49.
- **Gemini 3.7 Flash** — the value outlier. 5th on AA-LCR (80.0%), ahead of every GPT-5.6 variant
  and every Claude, at 1/7th of Opus 5's price and ~6× the output speed. The $0.75/$3.75 rate is
  promotional through 2026-12-31; it doubles to $1.50/$7.50 after that (still $0.11 a briefing).
- **GPT-5.6 Sol** — top of the GPT line, AA-LCR 77.7%, and the strictest structured-output
  implementation of the three big labs. Note OpenRouter lists this at $2/$10 while OpenAI's own
  pricing page says $4/$20 — verify before budgeting on the lower number.
- **Kimi K3** — **1st on AA-LCR at 82.7%**, the highest score of any model you can rent. If
  long-context grounding were the only criterion this would be #1. Held back by a 40–53%
  AA-Omniscience hallucination rate and a much thinner track record on financial text.
- **Sonnet 5** — 77.0% AA-LCR, *above* Opus 5, at 40% of the price, with the same constrained
  decoding. The best Anthropic-family value.
- **GLM-5.3 / GLM-5.3-Flash** — Flash scores 78.0% on AA-LCR, higher than Opus 5, for $0.005 a
  briefing. GLM-5.3 has one of the lowest hallucination rates on the board (29.6%). The catch is
  the JurisTech result below.
- **DeepSeek V4** — 1M context, extremely cheap, but the worst hallucination rates on
  AA-Omniscience of anything here (V4 Pro 94.1%). Prices halve off-peak; peak is 01:00–04:00 and
  06:00–10:00 UTC Mon–Fri, so **a US-morning briefing runs off-peak** and pays half the numbers in
  the table.
- **Haiku 4.5** — the only model in the top half with a **200k context**. An 80k-token briefing
  fits, but there is no headroom for a growing playbook.
- **gpt-oss-120b** — listed because it is the same weights you would run locally, at a price that
  makes running it locally hard to justify unless you want offline operation.

---

## Endpoints

Every one of these is OpenAI-compatible except Anthropic, which `internal/llm` already handles
natively.

| Provider | Base URL | Model id to send | Key env |
|---|---|---|---|
| Anthropic (native) | `https://api.anthropic.com` | `claude-opus-5`, `claude-sonnet-5`, `claude-haiku-4-5` | `ANTHROPIC_API_KEY` |
| OpenAI | `https://api.openai.com/v1` | `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna` | `OPENAI_API_KEY` |
| Google | `https://generativelanguage.googleapis.com/v1beta/openai/` | `gemini-3.7-flash`, `gemini-3.1-pro` | `GEMINI_API_KEY` |
| OpenRouter | `https://openrouter.ai/api/v1` | any slug from the table | `OPENROUTER_API_KEY` |
| Z.ai (GLM) | `https://api.z.ai/api/paas/v4/` | `glm-5.3`, `glm-5.3-flash` | `ZAI_API_KEY` |
| DeepSeek | `https://api.deepseek.com` | `deepseek-v4-pro`, `deepseek-v4-flash` | `DEEPSEEK_API_KEY` |
| Moonshot (Kimi) | `https://api.moonshot.ai/v1` | `kimi-k3` | `MOONSHOT_API_KEY` |
| Alibaba (Qwen) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `qwen3.8-max` | `DASHSCOPE_API_KEY` |
| xAI | `https://api.x.ai/v1` | `grok-4.6` | `XAI_API_KEY` |
| Mistral | `https://api.mistral.ai/v1` | `mistral-large-2512` | `MISTRAL_API_KEY` |
| Groq | `https://api.groq.com/openai/v1` | open-weight models, very fast | `GROQ_API_KEY` |
| Ollama | `http://localhost:11434/v1/` | local tag | any string (ignored) |

---

## What the benchmarks actually say

Four measurements matter for this job, and they disagree with each other in a useful way.

**AA-LCR — long-context reasoning.** The closest public proxy for Tape's workload: 100 questions
requiring the model to read ~100k tokens across several documents (academic papers, company
financials, government consultations, legal documents) and integrate facts from different places.
Snapshot 2026-08-30:

| Kimi K3 | Gemini 3.7 Flash | GPT-5.6 Terra | Gemini 3.1 Pro | GLM-5.3-Flash | GPT-5.6 Sol | Claude Sonnet 5 | GLM-5.3 | Claude Opus 5 | Grok 4.6 | DeepSeek V4 Pro |
|---|---|---|---|---|---|---|---|---|---|---|
| 82.7% | 80.0% | 79.7% | 79.0% | 78.0% | 77.7% | 77.0% | 76.3% | 75.7% | 75.0% | 75.3% |

The spread across that whole row is seven points. **Long-context grounding is not where the
frontier models are separating from the cheap ones** — which is the single most useful fact for
choosing a model for Tape.

**AA Finance & Accounting index.** Claude Opus 5 is joint first at 57. Only 28 of 177 models are
scored, so absence isn't evidence. The index is 30% business knowledge, 30% agentic knowledge work,
20% reasoning, 10% agentic customer interaction, 5% long-context, 5% non-hallucination.

**AA-Omniscience.** Two numbers, easily confused. The *Index* (−100..100) rewards correct answers,
penalises hallucinations and does not penalise abstention: Fable 5 leads at 43, Opus 5 second at
37. The *Hallucination Rate* is `incorrect / (incorrect + partial + not attempted)` — how often a
model bluffs instead of declining. Opus 5 scores 60.8% there, worse than Opus 4.8 (39.3%) and much
worse than GLM-5.3 (29.6%) or Grok 4.3 (25.0%). **This is a parametric-recall test, not a grounding
test.** A model that attempts more obscure questions scores worse on rate and better on index. For
Tape, where everything the model needs is in the prompt, the index is the more relevant number and
the rate is a warning about what happens when the prompt is *missing* something.

**JurisTech's financial-gap benchmark (April 2026)** is the one that maps directly onto the failure
mode Tape should fear. Real quarterly financials with P&L sections deliberately removed, degraded
scans, blurred images; models asked for ratios they could not compute. Pass = say the data is
missing. Fail = produce a number anyway.

1. **GPT-5.4** — found Revenue in the notes, disclosed the substitution, reported the rest as unavailable.
2. **Claude Opus 4.6** — flagged the removed sections; extrapolated some ratios but labelled them estimates.
3. **Kimi K2.5** — good structural understanding, extrapolated without enough grounding.
4. **Gemini 3.1 Pro** — *hallucinated* Revenue, Net Sales and Operating Income, confidently and unqualified.
5. **Qwen 3.6-Plus** — fabricated statistics and invented names of executives not in the document.
6. **GLM-5.1** — mixed quarterly and full-year figures; every derived ratio was fabricated.

Their conclusion is worth pinning above the desk: *"The formatting quality of an AI output is
almost entirely uncorrelated with its factual accuracy."* A schema-valid JSON proposal with a
made-up stop-loss looks exactly like a good one.

Two things follow for Tape. Prefer the OpenAI and Anthropic families where a headline may be
truncated or a quote may be missing — they degrade by hedging, the others by inventing. And make
the schema able to express *"I could not determine this"*, because a required numeric field with no
nullable alternative is an instruction to fabricate. The briefing schema does: `threshold_pct` is
typed `["number", "null"]`, and a null takes the desk's configured default rather than a number the
model made up.

### Long-context degradation, one line each

Public measurements (Q1–Q2 2026) put usable recall at 90%+ around 100k tokens for the frontier
models, falling to 60–76% at 1M. At Tape's prompt size, **every model in the table is comfortably
inside its good band**; the AA-LCR row above is the operative ranking. The 60,000-character cap is
what keeps it there as the playbook and the news feed grow, and raising that constant is the change
to think twice about.

### The Anthropic tokenizer caveat

Claude 4.7 and later use a tokenizer that produces **~30% more tokens for the same text**. A prompt
you measured at 60k tokens elsewhere is ~78k tokens on Opus 5 or Sonnet 5, so their real cost per
briefing is closer to **$0.49** and **$0.19**. It does not change the ranking; it does mean you
should size the context budget with `count_tokens` rather than a character heuristic.

---

## Latency

Measured at a short prompt, so treat these as the floor, not the estimate:

| Model | Output tok/s | Latency to first token |
|---|---|---|
| Gemini 3.7 Flash (high) | 330 | 9.1s |
| Gemini 3.7 Flash (medium) | 332 | 4.7s |
| GPT-5.6 Sol (xhigh) | 81 | 52.4s |
| GLM-5.3 (max) | 67 | 1.6s |
| Grok 4.6 (high) | 59 | 40.3s |
| Claude Opus 5 (high) | 52 | 18.5s |
| Claude Opus 5 (medium) | 50 | 4.0s |
| Kimi K3 (max) | 38 | 4.0s |
| Claude Sonnet 5 (max) | 84 | 199.9s |

Add prefill. Published measurements put a 100k-token prompt at 5–15s of prefill, so a 60k prompt
costs roughly 3–9s on top.

Worked example, Opus 5 at `high` effort: ~5s prefill + ~19s to first token + 3,000 tokens at 52
tok/s ≈ 58s of generation ≈ **80 seconds**. Inside the budget, with less headroom than you would
like. Dropping to `medium` effort takes it to ~65s. Gemini 3.7 Flash finishes the same job in
under 25 seconds.

Two traps:

- **Sonnet 5 at `max` effort has a 200-second time-to-first-token.** It will blow the deadline. Run
  Sonnet at `high` or below.
- **Prompt caching does not help Tape.** Anthropic's cache TTL is 5 minutes or 1 hour; Tape runs
  once every 24 hours, so every request is a cache miss. Batch pricing (50% off) is likewise
  useless: batches are asynchronous with a 24-hour window and you need the answer before the open.
  Budget at full list price.

---

## Structured output, by family

`internal/llm.Request.JSONSchema` documents a fallback — "providers without native structured
output fall back to instructing the model and validating the parsed JSON." Here is who needs it.

**Anthropic (native client).** Structured outputs are GA and enforced by *constrained decoding*:
the response is guaranteed to parse and to match the schema, or the request 400s at submit time.
This is the strongest guarantee available anywhere.

```go
OutputConfig: anthropic.OutputConfigParam{
    Format: anthropic.JSONOutputFormatParam{Schema: schema},
},
```

`github.com/invopop/jsonschema` — already in `go.mod` — is the reflector Anthropic's own Go example
uses, so a Go struct is the schema. Schema limits worth knowing before designing the briefing type:
`additionalProperties` must be `false`; **no recursion**; **no numeric constraints** (`minimum`,
`maximum`, `multipleOf`) and no string length constraints; `enum`, `const`, `anyOf`, `$ref` and the
usual string formats are fine; array `minItems` may only be 0 or 1. So range-check proposal
numbers in Go, not in the schema. First request with a new schema pays a grammar-compilation
latency; it is cached for 24 hours after that — which, for a once-daily job, means **you pay it
every single morning**. Budget a couple of seconds.

**OpenAI.** `response_format: {type: "json_schema", json_schema: {name, strict: true, schema}}`.
Same strictness guarantee, same broad restrictions.

**Google.** Native `responseSchema`/`responseMimeType`, and `response_format` with a JSON schema
works through the OpenAI-compatible endpoint.

**Everything else** (GLM, DeepSeek, Qwen, Kimi, Grok, Mistral, MiniMax, Llama, gpt-oss) advertises
`structured_outputs` on OpenRouter, but enforcement varies — OpenRouter's own docs say some
providers "guarantee schema-conforming output, while others translate your schema into their own
structured-output format." Treat these as best-effort: keep the fallback path, validate after
parsing, and retry once on a parse failure.

---

## OpenRouter specifics

One key, every model in the table, one base URL. That is the whole argument for it, and for a
project that wants to be provider-agnostic it is a good one.

**Endpoint and auth.**

```
POST https://openrouter.ai/api/v1/chat/completions
Authorization: Bearer $OPENROUTER_API_KEY
HTTP-Referer: https://github.com/trapp01/tape     # optional
X-OpenRouter-Title: tape                          # optional
```

Both optional headers are attribution only — they put the app on OpenRouter's public leaderboards.
Note the title header is now `X-OpenRouter-Title`; the old `X-Title` name appears in a lot of
tutorials.

**Structured output.** Pass the OpenAI shape:

```json
{"response_format": {"type": "json_schema",
  "json_schema": {"name": "briefing", "strict": true, "schema": { }}}}
```

Support is per *provider endpoint*, not per model — the same model served by two providers may
differ. If a routed provider doesn't support it the request fails with an error rather than
silently dropping the constraint, which is the right behaviour but means **you must pin routing**.
The fix is one field:

```json
{"provider": {"require_parameters": true}}
```

That restricts routing to endpoints supporting every parameter you sent. Without it, a fallback to
a provider that lacks structured outputs turns a good morning into a parse error. Filter the model
list with `?supported_parameters=structured_outputs` on `GET /api/v1/models` to see which
combinations qualify.

**Provider routing.** The `provider` object takes `order` (provider slugs to try in sequence),
`only` / `ignore`, `allow_fallbacks` (default true), `require_parameters`, `data_collection`
(`allow` | `deny`), `zdr` (zero-data-retention endpoints only), `quantizations`, `sort`
(`price` | `throughput` | `latency`), `preferred_min_throughput`, `preferred_max_latency`, and
`max_price`. Slug suffixes: **`:nitro`** sorts by throughput and requests the priority tier,
**`:floor`** sorts by price and uses the flex tier. `:batch` and `:free` variants exist as separate
slugs (e.g. `anthropic/claude-opus-5:batch`, `z-ai/glm-5.2:free`). The `:online`, `:exacto`
suffixes that circulate in older write-ups are not in the current provider-routing docs.

For Tape, a sane default:

```json
{"provider": {"require_parameters": true, "data_collection": "deny", "sort": "throughput"}}
```

`data_collection: "deny"` matters more than usual here — the prompt contains a hand-written
strategy playbook, which is the most private thing in the repo.

**Fees.** OpenRouter does **not** mark up inference: "we pass through the pricing of the underlying
providers." You pay on credit purchase instead — **5.5% ($0.80 minimum) by card, 5% in USDC**. On
$95/year of Opus 5 that is about $5. BYOK is free up to $25,000/month of equivalent usage, then 5%.

**Rate limits.** Free models are capped at 50 requests/day, raised to 1,000/day once you have ever
held $10 in credits. Irrelevant at one request a morning.

**Gotchas.**

- Prices in the table came from `GET https://openrouter.ai/api/v1/models`, which is the
  authoritative machine-readable source and worth polling if you ever want to show cost in the CLI.
- OpenRouter's price for a model can be *below* the first-party price when third parties host open
  weights — `deepseek/deepseek-v4-flash` shows $0.079/$0.157 against DeepSeek's own $0.44/$1.32
  peak. Cheaper, and a different machine with a different quantization and a different privacy
  posture. Pin with `only` if that matters.
- Conversely, OpenRouter lists `openai/gpt-5.6-sol` at $2/$10 while OpenAI's pricing page says
  $4/$20. Verify before relying on either.
- Mid-stream fallback to a different provider is possible unless you set `allow_fallbacks: false`,
  and different providers tokenize differently, so token counts and cost per call will not be
  perfectly reproducible.
- Anthropic prompt caching (`cache_control`) passthrough is provider-dependent. Moot for Tape.

---

## Direct APIs

### Z.ai (GLM)

The user's specific question: yes, GLM is worth hitting directly, and it is the least friction of
any provider here.

```
Base URL   https://api.z.ai/api/paas/v4/
Endpoint   POST https://api.z.ai/api/paas/v4/chat/completions
Auth       Authorization: Bearer $ZAI_API_KEY
Models     glm-5.3   glm-5.3-flash
```

Pricing, USD per 1M tokens, from Z.ai's own docs:

| Model | Input | Cached input | Output | One briefing |
|---|---|---|---|---|
| GLM-5.3 | $1.40 | $0.26 | $4.40 | $0.097 |
| GLM-5.3-Flash | $0.075 | $0.015 | $0.25 | **$0.005** |
| GLM-4.7-Flash | free | — | free | $0.00 |

Context is 1.31M tokens (per OpenRouter's metadata for the same models; Z.ai's pricing page does
not list it). Both support `response_format` and advertise `structured_outputs`. GLM-5.3-Flash at
half a cent a briefing with an AA-LCR score above Claude Opus 5 is the most striking number in this
document.

Two cautions. The GLM Coding Plan subscription uses a *different, dedicated endpoint* and is scoped
to coding tools — do not assume a coding-plan key works against `api.z.ai/api/paas/v4`. And GLM-5.1
was the worst performer in the JurisTech gap test, fabricating every derived ratio; GLM-5.3 is two
generations on and has a much better hallucination rate, but this is not the family to point at a
half-broken data feed.

### DeepSeek

```
Base URL   https://api.deepseek.com
Auth       Authorization: Bearer $DEEPSEEK_API_KEY
Models     deepseek-v4-pro   deepseek-v4-flash
Context    1M tokens, 384k max output
```

Pricing, USD per 1M tokens:

| | v4-flash off-peak | v4-flash peak | v4-pro off-peak | v4-pro peak |
|---|---|---|---|---|
| Input, cache hit | $0.007 | $0.014 | $0.022 | $0.044 |
| Input, cache miss | $0.22 | $0.44 | $0.66 | $1.32 |
| Output | $0.66 | $1.32 | $1.98 | $3.96 |

Off-peak is half price. **Peak is 01:00–04:00 and 06:00–10:00 UTC, Monday–Friday** — a US-morning
run at 13:00 UTC lands off-peak, so a V4 Pro briefing costs $0.046 rather than $0.091.

JSON output is supported (there is a dedicated JSON-mode guide) and OpenRouter reports
`structured_outputs` on the V4 endpoints. The reason DeepSeek is ranked 12th rather than 2nd is
AA-Omniscience: V4 Pro's hallucination rate is 94.1%, the worst of any model considered. Cheap and
long-context, but the least willing to say "I don't know" — the exact failure mode that hurts most
here.

---

## One data point: Alpha Arena

Nof1.ai ran **Alpha Arena**, a live-money competition in which six LLMs each traded $10,000 of real
capital in perpetual futures on Hyperliquid. Season 1 ran 18 October – 3 November 2025. **Qwen3
Max won at +22.3%**, finishing around $12,232; DeepSeek placed strongly; GPT-5, Gemini 2.5 and
Claude Sonnet 4.5 all took heavy drawdowns, attributed in the write-ups to over-leveraging and weak
risk control rather than bad directional calls. A later leaderboard snapshot showed DeepSeek Chat
V3.1 up 46% and GPT-5 down 75% before the models were stopped.

**Do not update much on this.** It is one run, over sixteen days, six models, one asset class, high
leverage, and a prompt design nobody outside Nof1 has audited. Sixteen days of crypto perps
separates luck from skill for approximately nobody. The one transferable lesson is the one Tape
already encodes: the models differed far more in *position sizing and risk discipline* than in
direction, which is an argument for the human confirming size and for the cost model pricing the
friction — not an argument for any particular model on this page.

---

## Recommendations

### Best quality — Claude Opus 5

`provider = "anthropic"`, `model = "claude-opus-5"`. Joint first on the finance and accounting
index, second on the omniscience index, constrained-decoding structured outputs that cannot return
malformed JSON, 1M context, and it degrades by hedging rather than by inventing. $0.49 a briefing
after the tokenizer premium, about **$120 a year**. The native SDK and `invopop/jsonschema` are
already in `go.mod`; this is the shortest path from here to a working `tape brief`.

Run it at `effort: "high"` with adaptive thinking. If the morning run ever feels slow, drop to
`medium` before you drop the model.

If quality is genuinely the only axis, **Claude Fable 5** is one step further up (AA-Omniscience
Index 43) at $0.75 a briefing. It is not worth double for this job.

### Best value — Gemini 3.7 Flash

`gemini-3.7-flash` via `https://generativelanguage.googleapis.com/v1beta/openai/`, or
`google/gemini-3.7-flash` on OpenRouter. **$0.056 a briefing, $14 a year** — a seventh of Opus 5 —
and it scores *higher* on the long-context reasoning benchmark that most resembles Tape's job
(80.0% vs 75.7%). 330 output tokens/sec and ~9s to first token means the whole briefing lands in
under 25 seconds. Native structured outputs.

The reservation is honest: Gemini 3.1 Pro was one of the models that fabricated revenue figures in
the JurisTech gap test, and Flash was not tested. So make the schema nullable where a number may be
unknowable, and if you can afford $130 a year, run both for a fortnight and diff the briefings.
That comparison is a better use of the money than either model alone.

### Cheapest sane — GLM-5.3-Flash

`glm-5.3-flash` direct at `https://api.z.ai/api/paas/v4/`. **$0.005 a briefing — $1.32 a year.**
1.31M context, AA-LCR 78.0% (above Opus 5, above Sonnet 5), `structured_outputs` supported, and the
GLM family has among the lowest hallucination rates on AA-Omniscience. There is no cheaper model
that is this credible on the metrics that matter here.

Runner-up: `deepseek-v4-flash` at $0.015 off-peak, with the large caveat about DeepSeek's
willingness to bluff. If you would rather spend nothing at all, `glm-4.7-flash` is free.

### Best local via Ollama — and be honest about it

Realistic picks by unified memory:

| RAM | Model | `ollama run` | Notes |
|---|---|---|---|
| 16 GB | Qwen3 8B / Mistral Small | `qwen3:8b` | 60k context will not fit alongside the weights |
| 32 GB | Qwen3 30B-A3B (MoE) | `qwen3:30b-a3b` | ~18 GB at Q4 plus KV cache; workable at 32k, tight at 64k |
| 64 GB | gpt-oss-120b (MoE) | `gpt-oss:120b` | ~60 GB at native MXFP4; the best local quality available |
| 128 GB+ | gpt-oss-120b at longer context | `gpt-oss:120b` | Room for a 64k KV cache without thrashing |

Endpoint is `http://localhost:11434/v1/` and the API key is required-but-ignored, so any string
works. `internal/llm` already has the `ollama` preset.

Three things will bite you, in order of severity:

1. **Ollama silently truncates prompts that exceed `num_ctx`.** It trims from the front, with no
   error and no warning. Documentation gives conflicting defaults (4096 in the FAQ, 2048 in the
   Modelfile reference, VRAM-derived in the current context-length page) and there are open reports
   that `OLLAMA_CONTEXT_LENGTH` is **not honoured on the OpenAI-compatible `/v1/chat/completions`
   path**. For Tape this is a correctness bug, not a performance one: a briefing built from the
   back half of the news feed will look completely normal. Set `num_ctx` explicitly, then verify
   the `CONTEXT` column of `ollama ps` before trusting a single output.
2. **Prefill blows the deadline.** llama.cpp's Apple Silicon table puts an M4 Max (40 GPU cores) at
   ~886 tok/s prompt processing for a 7B Q4 model at a 512-token batch. Throughput falls as the KV
   cache grows and as parameters grow; extrapolating to a 30B-class model at 60k tokens gives
   roughly **90 seconds to several minutes of prefill before the first output token**, and a
   120B-class model is worse. The 2-minute budget is borderline on an M4 Max and gone on anything
   smaller.
3. **Schema adherence is best-effort.** Ollama's OpenAI-compat layer documents "JSON mode"; the
   native `format` field takes a schema. Neither is constrained decoding of the kind Anthropic and
   OpenAI provide. Keep the validate-and-retry path.

**Verdict: run local for development, offline replay, and prompt iteration — not for the live
morning briefing.** The strongest argument against local here isn't quality, it's that
`glm-5.3-flash` costs $1.32 a year. There is no meaningful money to save, and the failure mode
you're trading it for is a silently truncated prompt.

### The claude-code provider

`provider = "claude-code"` is not an API endpoint. It shells out to the Claude Code CLI installed on
your machine and runs the briefing under **your own Claude Code login and Max or Pro allowance**, so
the call comes out of a subscription you already pay for rather than API credits. Nothing leaves the
machine except the request Claude Code itself makes. There is no base URL and no key env var, which
is why the row in `tape llm providers` shows dashes for both; `$TAPE_CLAUDE_BIN` points at the
binary when it is not on `$PATH`.

**This is for personal use on your own machine only.** Anthropic's Agent SDK overview states:
*"Unless previously approved, Anthropic does not allow third party developers to offer claude.ai
login or rate limits for their products, including agents built on the Claude Agent SDK. Use the API
key authentication methods described in the Quickstart instead."* So a hosted tape, or a tape
distributed to other people as a product, has to run on API keys — every other provider in this
document. Running it yourself, against your own login, is what this preset is for.
[Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview) ·
[headless mode](https://code.claude.com/docs/en/headless.md)

The invocation is `-p --output-format json --model <model> --max-turns 1 --tools ""`, plus
`--system-prompt` when the request carries one and `--json-schema` when it carries a schema. The
empty `--tools` value turns off every built-in tool, so the model reads the prompt and answers;
`--max-turns 1` keeps it to a single turn. With a schema, Claude Code returns the object in
`structured_output` and tape uses that; without one it uses `result`.

Two things to know before you trust its numbers. `total_cost_usd` in the JSON envelope — surfaced as
`Response.CostUSD` — is [a client-side estimate](https://code.claude.com/docs/en/agent-sdk/cost-tracking)
that can differ from your actual bill, and under a subscription it is not a bill at all; treat it as
a relative signal for comparing runs, not an accounting figure. And a subscription allowance is a
rate limit, not a queue: if the morning briefing lands while you are out of allowance the call fails
rather than waits, which is a good reason to keep a cheap API-key provider configured as the
fallback for a routine that runs at a fixed time.

Model ids here are Claude Code's own names — `opus`, `sonnet`, `haiku` — not the API ids in the
table above. The preset defaults to `opus`.

---

## Sources

Anthropic —
[pricing](https://platform.claude.com/docs/en/about-claude/pricing) ·
[models overview](https://platform.claude.com/docs/en/about-claude/models/overview.md) ·
[structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs.md) ·
[prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching.md)

OpenAI —
[pricing](https://developers.openai.com/api/docs/pricing)

Google —
[Gemini pricing](https://ai.google.dev/gemini-api/docs/pricing) ·
[OpenAI compatibility](https://ai.google.dev/gemini-api/docs/openai)

OpenRouter —
[quickstart](https://openrouter.ai/docs/quickstart) ·
[structured outputs](https://openrouter.ai/docs/features/structured-outputs) ·
[provider routing](https://openrouter.ai/docs/features/provider-routing) ·
[FAQ (fees, rate limits)](https://openrouter.ai/docs/faq) ·
[models API](https://openrouter.ai/api/v1/models) — slugs, context lengths and prices in the table
were read from this endpoint on 2026-08-30

Z.ai —
[API introduction](https://docs.z.ai/guides/develop/http/introduction) ·
[pricing](https://docs.z.ai/guides/overview/pricing)

DeepSeek —
[API docs](https://api-docs.deepseek.com/) ·
[pricing](https://api-docs.deepseek.com/quick_start/pricing)

Others —
[Moonshot Kimi platform](https://platform.kimi.ai/docs/api/overview) ·
[Alibaba Model Studio OpenAI compatibility](https://www.alibabacloud.com/help/en/model-studio/compatibility-of-openai-with-dashscope) ·
[Ollama OpenAI compatibility](https://docs.ollama.com/openai)

Benchmarks —
[AA-LCR leaderboard](https://benchlm.ai/benchmarks/lcr) ·
[AA-Omniscience](https://artificialanalysis.ai/evaluations/omniscience) ·
[AA-Omniscience hallucination rate](https://benchlm.ai/benchmarks/omnisciencehallucinationrate) ·
[AA Finance & Accounting](https://artificialanalysis.ai/models/capabilities/finance-and-accounting) ·
[AA model leaderboard (speed, latency)](https://artificialanalysis.ai/leaderboards/models) ·
[JurisTech hallucination benchmark](https://juristech.net/best-llm-tools-for-financial-analysis-2026/) ·
[llama.cpp Apple Silicon benchmarks](https://github.com/ggml-org/llama.cpp/discussions/4167) ·
[long-context latency and recall measurements](https://tokenmix.ai/blog/1m-token-context-reality-check-2026)

Alpha Arena —
[Season 1 results](https://www.iweaver.ai/blog/alpha-arena-ai-trading-season-1-results/) ·
[Nof1 explainer](https://www.datawallet.com/crypto/alpha-arena-nof1-ai-explained) ·
[leaderboard](https://nof1.live/)

Third-party aggregators (Artificial Analysis mirrors, JurisTech, tokenmix, iWeaver) are secondary
sources. Provider pricing pages and the OpenRouter models API are primary and should win any
disagreement.
