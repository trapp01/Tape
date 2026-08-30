package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// claudeCodeBinEnv overrides which binary is executed, mainly for a non-PATH install.
const claudeCodeBinEnv = "TAPE_CLAUDE_BIN"

// execCommand is swapped in tests to stand in for the claude binary.
var execCommand = exec.CommandContext

// claudeCodeProvider shells out to the locally installed Claude Code CLI in headless
// mode, so the machine owner's own subscription pays instead of API credits. This is
// personal use on your own machine only; offering this login to third parties is
// prohibited. See https://code.claude.com/docs/en/headless.md.
type claudeCodeProvider struct {
	name  string
	model string
}

func newClaudeCode(p Preset, model string) *claudeCodeProvider {
	return &claudeCodeProvider{name: p.Name, model: model}
}

func (c *claudeCodeProvider) Name() string  { return c.name }
func (c *claudeCodeProvider) Model() string { return c.model }

// claudeCodeResult is the envelope `claude -p --output-format json` writes to stdout.
type claudeCodeResult struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	StopReason       string          `json:"stop_reason"`
	APIErrorStatus   any             `json:"api_error_status"`
	TotalCostUSD     *float64        `json:"total_cost_usd"`
	Usage            struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *claudeCodeProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("%s: request has no messages", c.name)
	}
	bin := os.Getenv(claudeCodeBinEnv)
	if bin == "" {
		bin = "claude"
	}

	// "--tools" with an empty value disables every built-in tool.
	args := []string{"-p", "--output-format", "json", "--model", c.model, "--max-turns", "1", "--tools", ""}
	if req.System != "" {
		args = append(args, "--system-prompt", req.System)
	}
	if len(req.JSONSchema) > 0 {
		args = append(args, "--json-schema", string(req.JSONSchema))
	}

	cmd := execCommand(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(transcript(req.Messages))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrNotFound) {
		return Response{}, fmt.Errorf("%s: claude code is not installed (looked for %q, set $%s to override): see https://code.claude.com/docs/en/quickstart",
			c.name, bin, claudeCodeBinEnv)
	}
	if ctx.Err() != nil {
		return Response{}, fmt.Errorf("%s: running %s: %w", c.name, bin, ctx.Err())
	}

	// A failed run still prints the failure as a result envelope, so parse stdout
	// before trusting the exit code.
	var res claudeCodeResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &res); err != nil {
		if runErr != nil {
			return Response{}, fmt.Errorf("%s: %s failed: %w (stderr: %s)", c.name, bin, runErr, excerpt(stderr.Bytes()))
		}
		return Response{}, fmt.Errorf("%s: decoding %s output: %w (stdout: %s)", c.name, bin, err, excerpt(stdout.Bytes()))
	}

	out := Response{
		Model:        c.model,
		InputTokens:  res.Usage.InputTokens,
		OutputTokens: res.Usage.OutputTokens,
		StopReason:   res.StopReason,
		CostUSD:      res.TotalCostUSD,
	}
	if out.StopReason == "" {
		out.StopReason = res.Subtype
	}
	if res.IsError {
		return out, fmt.Errorf("%s: %s reported %q: %s", c.name, bin, res.Subtype, claudeCodeErrText(res, stderr.Bytes()))
	}

	if hasStructuredOutput(res.StructuredOutput) {
		compact, err := compactJSON(res.StructuredOutput)
		if err != nil {
			return out, fmt.Errorf("%s: compacting structured output: %w", c.name, err)
		}
		out.Text = compact
	} else {
		out.Text = res.Result
	}
	if out.Text == "" {
		return out, fmt.Errorf("%s: %s returned no text (subtype %q)", c.name, bin, res.Subtype)
	}
	return out, nil
}

// transcript flattens the conversation for stdin, which carries a single prompt.
func transcript(messages []Message) string {
	if len(messages) == 1 {
		return messages[0].Content
	}
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if m.Role == RoleAssistant {
			b.WriteString("Assistant: ")
		} else {
			b.WriteString("User: ")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

func hasStructuredOutput(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func compactJSON(raw json.RawMessage) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// claudeCodeErrText picks the most specific failure text the run produced.
func claudeCodeErrText(res claudeCodeResult, stderr []byte) string {
	if res.Result != "" {
		return res.Result
	}
	if res.APIErrorStatus != nil {
		return fmt.Sprintf("api error status %v", res.APIErrorStatus)
	}
	if text := excerpt(stderr); text != "" {
		return text
	}
	return "no error detail reported"
}
