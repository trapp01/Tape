package brief

import "encoding/json"

func schema() json.RawMessage {
	return json.RawMessage(outputSchema)
}

// outputSchema is Output in strict structured-output form: every property is
// listed in required, no object takes extra properties, and a nullable field
// declares null in its type array rather than being left out of required.
const outputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["market_read", "regime_note", "calendar_note", "call", "watchlist", "risks"],
  "properties": {
    "market_read": {
      "type": "string",
      "minLength": 1,
      "description": "What the tape is doing this morning, in plain sentences."
    },
    "regime_note": {
      "type": "string",
      "minLength": 1,
      "description": "What the classified regime allows and forbids today, citing the playbook posture."
    },
    "calendar_note": {
      "type": "string",
      "description": "What the scheduled events do to the session. Empty when nothing is scheduled."
    },
    "call": {
      "type": "object",
      "additionalProperties": false,
      "required": ["instrument", "direction", "threshold_pct", "rationale", "invalidation"],
      "properties": {
        "instrument": {
          "type": "string",
          "minLength": 1,
          "description": "One ticker, uppercase."
        },
        "direction": {
          "type": "string",
          "enum": ["up", "down", "flat"],
          "description": "Where the instrument closes against its open."
        },
        "threshold_pct": {
          "type": ["number", "null"],
          "minimum": 0,
          "maximum": 5,
          "description": "Percent move that decides the call. Null takes the configured default."
        },
        "rationale": {
          "type": "string",
          "minLength": 1,
          "description": "Why, naming the playbook setup id it rests on."
        },
        "invalidation": {
          "type": "string",
          "minLength": 1,
          "description": "The observation that would prove the call wrong before the close."
        }
      }
    },
    "watchlist": {
      "type": "array",
      "maxItems": 12,
      "description": "One note per symbol worth watching, at most twelve.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["symbol", "bias", "note"],
        "properties": {
          "symbol": {
            "type": "string",
            "minLength": 1,
            "description": "Ticker, uppercase."
          },
          "bias": {
            "type": "string",
            "enum": ["bullish", "bearish", "neutral"]
          },
          "note": {
            "type": "string",
            "minLength": 1,
            "description": "What to watch on this symbol and the level that matters."
          }
        }
      }
    },
    "risks": {
      "type": "array",
      "maxItems": 5,
      "description": "What would make today a bad day to trade this plan, at most five.",
      "items": {
        "type": "string",
        "minLength": 1
      }
    }
  }
}`
