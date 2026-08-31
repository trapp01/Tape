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
  "required": ["market_read", "regime_note", "calendar_note", "call", "proposals", "watchlist", "risks"],
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
    "proposals": {
      "type": "array",
      "maxItems": 3,
      "description": "Zero to three trade ideas, each one citing a playbook setup id. An empty list is a valid morning.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["symbol", "side", "setup_id", "entry", "stop", "target", "thesis", "invalidation", "confidence"],
        "properties": {
          "symbol": {
            "type": "string",
            "minLength": 1,
            "description": "Ticker, uppercase, from the INDEXES or WATCHLIST blocks."
          },
          "side": {
            "type": "string",
            "enum": ["long"],
            "description": "Long only. Shorting is not enabled."
          },
          "setup_id": {
            "type": "string",
            "minLength": 1,
            "description": "The playbook setup this rests on, e.g. M2."
          },
          "entry": {
            "type": "number",
            "description": "The price the trade is entered at."
          },
          "stop": {
            "type": "number",
            "description": "The exit if the idea is wrong. Below the entry."
          },
          "target": {
            "type": "number",
            "description": "The exit if the idea is right. Above the entry."
          },
          "thesis": {
            "type": "string",
            "minLength": 1,
            "description": "Why this trade, in one or two sentences, naming what the setup asks for."
          },
          "invalidation": {
            "type": "string",
            "minLength": 1,
            "description": "The observation that kills the idea, separate from the stop price."
          },
          "confidence": {
            "type": "string",
            "enum": ["low", "medium", "high"],
            "description": "Your own read. It never changes the size; Go computes that from the stop."
          }
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
