package brief

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/llm"
)

func TestSchemaIsValidJSON(t *testing.T) {
	raw := Schema()
	if !json.Valid(raw) {
		t.Fatalf("Schema() is not valid json:\n%s", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshalling the schema: %v", err)
	}
	if doc["type"] != "object" {
		t.Errorf("top-level type = %v, want object", doc["type"])
	}
}

// TestSchemaIsStrictMode walks every object in the schema. Both Anthropic
// structured outputs and OpenAI strict mode need additionalProperties false and
// every property named in required.
func TestSchemaIsStrictMode(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Schema(), &doc); err != nil {
		t.Fatalf("unmarshalling the schema: %v", err)
	}
	walkObjects(t, "", doc)
}

func walkObjects(t *testing.T, path string, node map[string]any) {
	t.Helper()
	props, hasProps := node["properties"].(map[string]any)
	if hasProps {
		if node["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties = %v, want false", pathOrRoot(path), node["additionalProperties"])
		}
		var names []string
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)

		required := stringsOf(node["required"])
		sort.Strings(required)
		if !reflect.DeepEqual(names, required) {
			t.Errorf("%s: required = %v, want every property %v", pathOrRoot(path), required, names)
		}
		for name, child := range props {
			if obj, ok := child.(map[string]any); ok {
				walkObjects(t, path+"/"+name, obj)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		walkObjects(t, path+"/items", items)
	}
}

func pathOrRoot(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func stringsOf(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// TestSchemaNullabilityIsInTheTypeArray keeps threshold_pct nullable the way
// strict mode requires: declared null, never dropped from required.
func TestSchemaNullabilityIsInTheTypeArray(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Schema(), &doc); err != nil {
		t.Fatalf("unmarshalling the schema: %v", err)
	}
	call := doc["properties"].(map[string]any)["call"].(map[string]any)
	threshold := call["properties"].(map[string]any)["threshold_pct"].(map[string]any)

	types := stringsOf(threshold["type"])
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"null", "number"}) {
		t.Errorf("threshold_pct type = %v, want [number null]", threshold["type"])
	}
	if !slices.Contains(stringsOf(call["required"]), "threshold_pct") {
		t.Error("threshold_pct is nullable but missing from the call's required list")
	}
}

func TestSchemaEnumsAndLimits(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Schema(), &doc); err != nil {
		t.Fatalf("unmarshalling the schema: %v", err)
	}
	props := doc["properties"].(map[string]any)
	call := props["call"].(map[string]any)["properties"].(map[string]any)

	direction := stringsOf(call["direction"].(map[string]any)["enum"])
	if !reflect.DeepEqual(direction, []string{"up", "down", "flat"}) {
		t.Errorf("direction enum = %v", direction)
	}

	watch := props["watchlist"].(map[string]any)
	bias := stringsOf(watch["items"].(map[string]any)["properties"].(map[string]any)["bias"].(map[string]any)["enum"])
	if !reflect.DeepEqual(bias, []string{"bullish", "bearish", "neutral"}) {
		t.Errorf("bias enum = %v", bias)
	}
	if watch["maxItems"] != float64(MaxWatchNotes) {
		t.Errorf("watchlist maxItems = %v, want %d", watch["maxItems"], MaxWatchNotes)
	}
	if props["risks"].(map[string]any)["maxItems"] != float64(MaxRisks) {
		t.Errorf("risks maxItems = %v, want %d", props["risks"].(map[string]any)["maxItems"], MaxRisks)
	}
}

func TestSchemaMatchesOutputFields(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Schema(), &doc); err != nil {
		t.Fatalf("unmarshalling the schema: %v", err)
	}
	want := jsonTagsOf(reflect.TypeOf(Output{}))
	got := stringsOf(doc["required"])
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("schema properties = %v, Output fields = %v", got, want)
	}

	callWant := jsonTagsOf(reflect.TypeOf(Call{}))
	callGot := stringsOf(doc["properties"].(map[string]any)["call"].(map[string]any)["required"])
	sort.Strings(callWant)
	sort.Strings(callGot)
	if !reflect.DeepEqual(callGot, callWant) {
		t.Errorf("call properties = %v, Call fields = %v", callGot, callWant)
	}
}

// TestSchemaSurvivesKeywordStripping checks the schema still describes Output
// after the range keywords come out for strict structured outputs. Validate and
// ValidateAgainst are what actually enforce the ranges.
func TestSchemaSurvivesKeywordStripping(t *testing.T) {
	stripped, err := llm.StripUnsupportedKeywords(Schema())
	if err != nil {
		t.Fatalf("StripUnsupportedKeywords: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(stripped, &doc); err != nil {
		t.Fatalf("stripped schema is not json: %v", err)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("stripped schema has no properties:\n%s", stripped)
	}
	for _, name := range jsonTagsOf(reflect.TypeOf(Output{})) {
		if _, ok := props[name]; !ok {
			t.Errorf("property %q was lost:\n%s", name, stripped)
		}
	}

	got := stringsOf(doc["required"])
	want := jsonTagsOf(reflect.TypeOf(Output{}))
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("required = %v, want %v", got, want)
	}
	if doc["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", doc["additionalProperties"])
	}

	for _, kw := range []string{"minLength", "minimum", "maximum", "maxItems"} {
		if strings.Contains(string(stripped), `"`+kw+`"`) {
			t.Errorf("%s survived stripping:\n%s", kw, stripped)
		}
	}
}

func jsonTagsOf(t reflect.Type) []string {
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Tag.Get("json"))
	}
	return out
}

func TestOutputRoundTrips(t *testing.T) {
	threshold := 0.4
	want := Output{
		MarketRead:   "SPY gapped up 0.4% on a quiet overnight tape.",
		RegimeNote:   "Uptrend with low vol, so continuations are live at half the usual watch count.",
		CalendarNote: "CPI lands at 06:30 MT; nothing before it.",
		Call: Call{
			Instrument:   "SPY",
			Direction:    DirUp,
			ThresholdPct: &threshold,
			Rationale:    "M2: yesterday's high held on the retest.",
			Invalidation: "A close back under 512.00 in the first hour.",
		},
		Watchlist: []WatchNote{
			{Symbol: "NVDA", Bias: "bullish", Note: "Above the prior high with volume."},
			{Symbol: "TSLA", Bias: "neutral", Note: "Inside yesterday's range."},
		},
		Risks: []string{"CPI reprices the whole session.", "Volume is thin into a holiday."},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshalling Output: %v", err)
	}
	var got Output
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshalling Output: %v", err)
	}
	if got.Call.ThresholdPct == nil || *got.Call.ThresholdPct != threshold {
		t.Fatalf("threshold_pct = %v, want %v", got.Call.ThresholdPct, threshold)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the output:\ngot  %+v\nwant %+v", got, want)
	}
	if err := Validate(want); err != nil {
		t.Errorf("the sample output does not validate: %v", err)
	}

	var null Output
	if err := json.Unmarshal([]byte(`{"call":{"threshold_pct":null}}`), &null); err != nil {
		t.Fatalf("unmarshalling a null threshold: %v", err)
	}
	if null.Call.ThresholdPct != nil {
		t.Errorf("a null threshold_pct decoded as %v, want nil", null.Call.ThresholdPct)
	}
}
