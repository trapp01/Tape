package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/broker"
)

const watchRefresh = 250 * time.Millisecond

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch SYMBOL...",
		Short: "Stream live quotes until Ctrl-C",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbols := upperAll(args)
			a, err := newApp(cmd, "watch "+strings.Join(symbols, " "))
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			b := newBoard(symbols)
			streamErr := make(chan error, 1)
			go func() { streamErr <- a.data.StreamQuotes(ctx, symbols, b.update) }()

			if isTerminal(a.out) {
				return b.runTTY(ctx, a, streamErr)
			}
			return b.runPlain(ctx, a, streamErr)
		},
	}
}

// board holds the newest quote per symbol. update runs on the stream's goroutine,
// so it does nothing but take the lock.
type board struct {
	symbols []string

	mu     sync.Mutex
	latest map[string]broker.Quote
	feed   chan broker.Quote
}

func newBoard(symbols []string) *board {
	return &board{symbols: symbols, latest: map[string]broker.Quote{}, feed: make(chan broker.Quote, 256)}
}

func (b *board) update(q broker.Quote) {
	b.mu.Lock()
	b.latest[q.Symbol] = q
	b.mu.Unlock()
	select {
	case b.feed <- q:
	default:
	}
}

// runTTY redraws the table in place on a timer.
func (b *board) runTTY(ctx context.Context, a *app, streamErr <-chan error) error {
	tick := time.NewTicker(watchRefresh)
	defer tick.Stop()

	drawn := 0
	for {
		select {
		case <-ctx.Done():
			return watchExit(streamErr)
		case err := <-streamErr:
			return watchError(err)
		case <-tick.C:
			drawn = b.draw(a, drawn)
		}
	}
}

// runPlain prints one line per update when stdout is not a terminal.
func (b *board) runPlain(ctx context.Context, a *app, streamErr <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return watchExit(streamErr)
		case err := <-streamErr:
			return watchError(err)
		case q := <-b.feed:
			fmt.Fprintf(a.out, "%s  %s  bid %s  ask %s  mid %s  spread %s\n",
				stamp(q.Timestamp, a.loc), q.Symbol, price(q.Bid), price(q.Ask), price(q.Last), spreadBps(q))
		}
	}
}

// draw rewrites the previous frame using cursor-up, and returns the line count so
// the next frame knows how far to climb.
func (b *board) draw(a *app, previous int) int {
	lines := b.frame(a)
	var buf bytes.Buffer
	if previous > 0 {
		fmt.Fprintf(&buf, "\x1b[%dA", previous)
	}
	for _, line := range lines {
		fmt.Fprintf(&buf, "\x1b[2K%s\n", line)
	}
	io.Copy(a.out, &buf)
	return len(lines)
}

// frame renders the table to lines so the caller can prefix each with a clear code
// without disturbing tabwriter's column widths.
func (b *board) frame(a *app) []string {
	var buf bytes.Buffer
	tw := table(&buf)
	row(tw, "SYMBOL", "BID", "ASK", "MID", "SPREAD", "TIME")

	b.mu.Lock()
	for _, s := range b.symbols {
		q, ok := b.latest[s]
		if !ok {
			row(tw, s, "-", "-", "-", "-", "waiting")
			continue
		}
		row(tw, s, price(q.Bid), price(q.Ask), price(q.Last), spreadBps(q), q.Timestamp.In(a.loc).Format("15:04:05"))
	}
	b.mu.Unlock()
	tw.Flush()

	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func spreadBps(q broker.Quote) string {
	if q.Last <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", (q.Ask-q.Bid)/q.Last*10_000)
}

// watchExit waits briefly for the stream to unwind after Ctrl-C rather than
// leaving the goroutine mid-write.
func watchExit(streamErr <-chan error) error {
	select {
	case err := <-streamErr:
		return watchError(err)
	case <-time.After(2 * time.Second):
		return nil
	}
}

// watchError treats cancellation as a clean exit; anything else is a real fault.
func watchError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func upperAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
