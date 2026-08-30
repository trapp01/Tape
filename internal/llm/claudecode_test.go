package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// realResultJSON is the exact envelope observed from Claude Code 2.1.251 running
// `claude -p --output-format json --model opus --max-turns 1 --tools ""`, trimmed to
// the fields the parser reads.
const realResultJSON = `{"duration_api_ms":1955,"stop_reason":"end_turn","session_id":"d7ccb426-26d3-406c-9202-dd5228363ecc","total_cost_usd":0.069419,"usage":{"input_tokens":2,"cache_creation_input_tokens":6835,"cache_read_input_tokens":0,"output_tokens":4,"service_tier":"standard"},"permission_denials":[],"is_error":false,"num_turns":1,"subtype":"success","api_error_status":null,"result":"pong","type":"result","duration_ms":1243,"uuid":"e7214105-32f0-4b2f-b3c2-953e7e253271"}`

// helperRun is what the fake claude binary recorded about its invocation.
type helperRun struct {
	Argv  []string `json:"argv"`
	Stdin string   `json:"stdin"`
}

// fakeClaude points execCommand at TestHelperProcess for the rest of the test.
// The returned func reads back what the fake was invoked with.
func fakeClaude(t *testing.T, env map[string]string) func() helperRun {
	t.Helper()
	recordPath := filepath.Join(t.TempDir(), "run.json")

	original := execCommand
	t.Cleanup(func() { execCommand = original })
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], argv...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HELPER_RECORD="+recordPath)
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		return cmd
	}

	return func() helperRun {
		t.Helper()
		raw, err := os.ReadFile(recordPath)
		if err != nil {
			t.Fatalf("fake claude recorded nothing: %v", err)
		}
		var run helperRun
		if err := json.Unmarshal(raw, &run); err != nil {
			t.Fatalf("decoding fake claude record: %v", err)
		}
		return run
	}
}

// TestHelperProcess is not a real test; it is the fake claude binary.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	if i := slices.Index(args, "--"); i >= 0 {
		args = args[i+1:]
	}
	stdin, _ := io.ReadAll(os.Stdin)
	if path := os.Getenv("HELPER_RECORD"); path != "" {
		raw, _ := json.Marshal(helperRun{Argv: args, Stdin: string(stdin)})
		os.WriteFile(path, raw, 0o600)
	}
	if ms := os.Getenv("HELPER_SLEEP_MS"); ms != "" {
		d, _ := strconv.Atoi(ms)
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	fmt.Fprint(os.Stdout, os.Getenv("HELPER_STDOUT"))
	code := 0
	if c := os.Getenv("HELPER_EXIT"); c != "" {
		code, _ = strconv.Atoi(c)
	}
	os.Exit(code)
}

func newTestClaudeCode(t *testing.T) *claudeCodeProvider {
	t.Helper()
	preset, ok := FindPreset(ProviderClaudeCode)
	if !ok {
		t.Fatal("claude-code preset missing")
	}
	return newClaudeCode(preset, "opus")
}

// argValue returns the argument following flag.
func argValue(argv []string, flag string) (string, bool) {
	i := slices.Index(argv, flag)
	if i < 0 || i+1 >= len(argv) {
		return "", false
	}
	return argv[i+1], true
}

func TestClaudeCodeBuildsExpectedArgs(t *testing.T) {
	record := fakeClaude(t, map[string]string{"HELPER_STDOUT": realResultJSON})
	prov := newTestClaudeCode(t)

	_, err := prov.Complete(context.Background(), Request{
		System:     "be terse",
		Messages:   []Message{{Role: RoleUser, Content: "ping"}},
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"side":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	run := record()
	if run.Argv[0] != "claude" {
		t.Errorf("binary = %q, want claude", run.Argv[0])
	}
	if !slices.Contains(run.Argv, "-p") {
		t.Errorf("argv is missing -p: %v", run.Argv)
	}
	for flag, want := range map[string]string{
		"--output-format": "json",
		"--model":         "opus",
		"--max-turns":     "1",
		"--tools":         "",
		"--system-prompt": "be terse",
		"--json-schema":   `{"type":"object","properties":{"side":{"type":"string"}}}`,
	} {
		got, ok := argValue(run.Argv, flag)
		if !ok {
			t.Errorf("argv is missing %s: %v", flag, run.Argv)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
	if run.Stdin != "ping" {
		t.Errorf("stdin = %q, want the prompt", run.Stdin)
	}
}

func TestClaudeCodeOmitsOptionalFlags(t *testing.T) {
	record := fakeClaude(t, map[string]string{"HELPER_STDOUT": realResultJSON})
	prov := newTestClaudeCode(t)

	if _, err := prov.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	run := record()
	if slices.Contains(run.Argv, "--system-prompt") {
		t.Errorf("--system-prompt sent without a system prompt: %v", run.Argv)
	}
	if slices.Contains(run.Argv, "--json-schema") {
		t.Errorf("--json-schema sent without a schema: %v", run.Argv)
	}
}

func TestClaudeCodeParsesRealResultShape(t *testing.T) {
	fakeClaude(t, map[string]string{"HELPER_STDOUT": realResultJSON})
	prov := newTestClaudeCode(t)

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "pong" {
		t.Errorf("Text = %q, want pong", resp.Text)
	}
	if resp.Model != "opus" {
		t.Errorf("Model = %q, want opus", resp.Model)
	}
	if resp.InputTokens != 2 || resp.OutputTokens != 4 {
		t.Errorf("usage = %d/%d, want 2/4", resp.InputTokens, resp.OutputTokens)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	if resp.CostUSD == nil {
		t.Fatal("CostUSD not captured")
	}
	if *resp.CostUSD != 0.069419 {
		t.Errorf("CostUSD = %v, want 0.069419", *resp.CostUSD)
	}
}

func TestClaudeCodeStructuredOutputBecomesCompactText(t *testing.T) {
	const body = `{"type":"result","subtype":"success","is_error":false,"result":"here you go",
	  "structured_output": { "side" : "long",  "size": 3 },
	  "stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":7}}`
	fakeClaude(t, map[string]string{"HELPER_STDOUT": body})
	prov := newTestClaudeCode(t)

	resp, err := prov.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "call it"}},
		JSONSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != `{"side":"long","size":3}` {
		t.Errorf("Text = %q, want the compacted structured output", resp.Text)
	}
}

func TestClaudeCodeFallsBackToResultWhenStructuredOutputIsNull(t *testing.T) {
	const body = `{"type":"result","subtype":"success","is_error":false,"result":"plain text",
	  "structured_output":null,"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	fakeClaude(t, map[string]string{"HELPER_STDOUT": body})
	prov := newTestClaudeCode(t)

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "plain text" {
		t.Errorf("Text = %q, want the result field", resp.Text)
	}
}

func TestClaudeCodeIsErrorReturnsError(t *testing.T) {
	const body = `{"type":"result","subtype":"error_during_execution","is_error":true,
	  "result":"Invalid API key · Please run /login","stop_reason":null,
	  "usage":{"input_tokens":0,"output_tokens":0}}`
	fakeClaude(t, map[string]string{"HELPER_STDOUT": body})
	prov := newTestClaudeCode(t)

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error when is_error is true")
	}
	if !strings.Contains(err.Error(), "error_during_execution") || !strings.Contains(err.Error(), "Please run /login") {
		t.Errorf("error should carry the subtype and result text: %v", err)
	}
	if resp.StopReason != "error_during_execution" {
		t.Errorf("StopReason = %q, want the subtype fallback", resp.StopReason)
	}
}

func TestClaudeCodeNonZeroExitReportsStderr(t *testing.T) {
	fakeClaude(t, map[string]string{
		"HELPER_STDOUT": "",
		"HELPER_STDERR": "error: unknown option '--nope'",
		"HELPER_EXIT":   "1",
	})
	prov := newTestClaudeCode(t)

	_, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error on a non-zero exit")
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Errorf("error should carry the stderr excerpt: %v", err)
	}
}

func TestClaudeCodeFlattensMultiTurnOntoStdin(t *testing.T) {
	record := fakeClaude(t, map[string]string{"HELPER_STDOUT": realResultJSON})
	prov := newTestClaudeCode(t)

	if _, err := prov.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Content: "first"},
			{Role: RoleAssistant, Content: "ack"},
			{Role: RoleUser, Content: "second"},
		},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	want := "User: first\n\nAssistant: ack\n\nUser: second"
	if got := record().Stdin; got != want {
		t.Errorf("stdin = %q, want %q", got, want)
	}
}

func TestClaudeCodeMissingBinary(t *testing.T) {
	t.Setenv(claudeCodeBinEnv, "tape-claude-does-not-exist")
	prov := newTestClaudeCode(t)

	_, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error when the binary is missing")
	}
	if !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), "quickstart") {
		t.Errorf("error should say it is not installed and link the quickstart: %v", err)
	}
	if !strings.Contains(err.Error(), claudeCodeBinEnv) {
		t.Errorf("error should mention the override env var: %v", err)
	}
}

func TestClaudeCodeHonoursBinOverride(t *testing.T) {
	t.Setenv(claudeCodeBinEnv, "/opt/custom/claude")
	record := fakeClaude(t, map[string]string{"HELPER_STDOUT": realResultJSON})
	prov := newTestClaudeCode(t)

	if _, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := record().Argv[0]; got != "/opt/custom/claude" {
		t.Errorf("binary = %q, want the override", got)
	}
}

func TestClaudeCodeContextCancellationKillsProcess(t *testing.T) {
	fakeClaude(t, map[string]string{
		"HELPER_STDOUT":   realResultJSON,
		"HELPER_SLEEP_MS": "10000",
	})
	prov := newTestClaudeCode(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := prov.Complete(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error when the context expires")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("error should name the context failure: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v, the process was not killed promptly", elapsed)
	}
}

func TestClaudeCodeRejectsEmptyMessages(t *testing.T) {
	prov := newTestClaudeCode(t)
	if _, err := prov.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("want an error when there are no messages")
	}
}
