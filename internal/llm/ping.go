package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PingResult is what a reachability check learned about a provider.
type PingResult struct {
	Model   string
	Latency time.Duration
	Reply   string
}

// Ping checks credentials and reachability with the smallest useful request.
// MaxTokens is left at the provider default so a thinking model is not truncated
// before it answers.
func Ping(ctx context.Context, p Provider) (PingResult, error) {
	start := time.Now()
	resp, err := p.Complete(ctx, Request{
		System:   "You are a connectivity check. Reply with the single word pong and nothing else.",
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
	})
	if err != nil {
		return PingResult{}, fmt.Errorf("pinging %s (%s): %w", p.Name(), p.Model(), err)
	}
	model := resp.Model
	if model == "" {
		model = p.Model()
	}
	return PingResult{
		Model:   model,
		Latency: time.Since(start),
		Reply:   strings.TrimSpace(resp.Text),
	}, nil
}
