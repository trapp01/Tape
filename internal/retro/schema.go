package retro

import "encoding/json"

const (
	// MaxFindings and MaxDiffs bound the reply. A week that needs more than this
	// is being over-read.
	MaxFindings = 8
	MaxDiffs    = 5
)

func schema() json.RawMessage { return json.RawMessage(outputSchema) }

// outputSchema is Output in strict structured-output form: every property listed
// in required, and no object taking extra properties.
const outputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "findings", "diffs"],
  "properties": {
    "summary": {
      "type": "string",
      "minLength": 1,
      "description": "What this stretch of the record shows, in plain sentences. Say so when the sample is too small to conclude anything."
    },
    "findings": {
      "type": "array",
      "maxItems": 8,
      "description": "At most eight things the numbers show. An empty list is valid when the record shows nothing yet.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title", "evidence", "confidence"],
        "properties": {
          "title": {
            "type": "string",
            "minLength": 1,
            "description": "The finding in one line."
          },
          "evidence": {
            "type": "string",
            "minLength": 1,
            "description": "The numbers from the record that say it, quoted."
          },
          "confidence": {
            "type": "string",
            "enum": ["low", "medium", "high"],
            "description": "How much the sample size supports this. A handful of trades is low."
          }
        }
      }
    },
    "diffs": {
      "type": "array",
      "maxItems": 5,
      "description": "At most five exact playbook edits. An empty list is a valid answer and the correct one when the record cannot carry a change.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["section", "change", "rationale", "before", "after"],
        "properties": {
          "section": {
            "type": "string",
            "minLength": 1,
            "description": "A heading that already exists in the playbook, copied verbatim, e.g. \"### M2 momentum continuation above prior high\". Never \"## Risk rules\"."
          },
          "change": {
            "type": "string",
            "enum": ["add", "edit", "remove"],
            "description": "add appends after under the section; edit replaces before with after; remove deletes before."
          },
          "rationale": {
            "type": "string",
            "minLength": 1,
            "description": "Which finding this edit follows from."
          },
          "before": {
            "type": "string",
            "description": "For edit and remove: text appearing exactly once under the section, copied character for character. Empty for add."
          },
          "after": {
            "type": "string",
            "description": "The replacement or the new text. Empty for remove."
          }
        }
      }
    }
  }
}`
