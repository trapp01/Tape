package cli

import (
	"fmt"
	"math"
	"strings"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/playbook"
)

// renderProposals prints the session's slate: what to trade, at what size, and
// what became of each idea. An empty slate is a real answer, so it says so and
// gives the model's reason for it.
func renderProposals(a *app, res brief.Result) {
	slate := res.Proposals
	if len(slate) == 0 {
		label(a, "PROPOSALS", "none today")
		continuation(a, res.Output.RegimeNote)
		return
	}

	titles := playbook.SetupTitles(res.Input.Playbook)
	label(a, "PROPOSALS", fmt.Sprintf("(%d)", len(slate)))
	for _, p := range slate {
		fmt.Fprintf(a.out, "  #%d  %s %s — %s%s\n", p.Index, strings.ToUpper(p.Side), p.Symbol,
			setupTitle(titles, p.SetupID), statusSuffix(a, p))
		if p.Status != journal.ProposalProposed && p.Status != journal.ProposalTaken {
			continue
		}
		fmt.Fprintf(a.out, "      %s\n", planLine(p, res.Input))
		fmt.Fprintf(a.out, "      thesis: %s\n", oneLine(p.Thesis))
		fmt.Fprintf(a.out, "      invalid if: %s   confidence: %s\n", oneLine(p.Invalidation), dash(p.Confidence))
	}
	if n, ok := firstOpen(slate); ok {
		fmt.Fprintf(a.out, "act: tape take %d · tape pass %d --reason \"…\" · tape why %d\n", n, n, n)
	}
}

// planLine is the numbers Go computed, never the model's: share count, notional,
// what the stop costs, and the reward the target pays for it.
func planLine(p journal.Proposal, in brief.Input) string {
	size := fmt.Sprintf("size %d sh (~%s · risks %s%s)",
		p.Qty, money0(float64(p.Qty)*p.Entry), money0(p.RiskUSD), riskShare(p.RiskUSD, in.Equity))
	if cashCapped(p, in) {
		size += " (cash-capped)"
	}
	parts := []string{
		fmt.Sprintf("entry %.2f  stop %.2f  target %.2f", p.Entry, p.Stop, p.Target),
		size,
	}
	if rr := rewardRisk(p); rr > 0 {
		parts = append(parts, fmt.Sprintf("%.1fR", rr))
	}
	return strings.Join(parts, "  ")
}

// cashCapped reports whether the free cash, not the risk budget, set the size:
// the budget alone would have bought more shares than the account can pay for.
func cashCapped(p journal.Proposal, in brief.Input) bool {
	return p.Qty > 0 && p.Qty < riskSizedQty(p, in)
}

// riskSizedQty is what the per-trade budget alone would have bought, or zero
// when the archive carries no sizing basis.
func riskSizedQty(p journal.Proposal, in brief.Input) int {
	if in.Equity <= 0 || in.Limits.PerTradePct <= 0 || p.Entry <= p.Stop {
		return 0
	}
	budget := in.Equity * in.Limits.PerTradePct / 100
	return int(math.Floor(budget / (p.Entry - p.Stop)))
}

// riskShare is what the stop costs as a share of the account it was sized
// against. An archive written before the equity was recorded shows nothing.
func riskShare(riskUSD, equity float64) string {
	if equity <= 0 || riskUSD <= 0 {
		return ""
	}
	return fmt.Sprintf(" = %.1f%%", riskUSD*100/equity)
}

func rewardRisk(p journal.Proposal) float64 {
	if p.Target <= 0 || p.Entry <= p.Stop {
		return 0
	}
	return (p.Target - p.Entry) / (p.Entry - p.Stop)
}

// statusSuffix says what became of the idea. A proposal still open says nothing:
// the act line below the slate is the invitation.
func statusSuffix(a *app, p journal.Proposal) string {
	switch p.Status {
	case journal.ProposalTaken:
		return "   ✓ taken" + orderLink(p)
	case journal.ProposalSubmitting:
		return "   … submitting" + orderLink(p)
	case journal.ProposalUnfilled:
		return "   " + a.style.dim("never filled")
	case journal.ProposalPassed:
		return "   " + a.style.dim("— passed: "+oneLine(p.Reason))
	case journal.ProposalRejected:
		return "   ✗ rejected: " + oneLine(p.Reason)
	case journal.ProposalExpired:
		return "   " + a.style.dim("expired")
	default:
		return ""
	}
}

func orderLink(p journal.Proposal) string {
	if p.OrderID == nil {
		return ""
	}
	return fmt.Sprintf(" → order #%d", *p.OrderID)
}

// firstOpen is the index the act line offers, which is the first idea still
// waiting on a decision.
func firstOpen(slate []journal.Proposal) (int, bool) {
	for _, p := range slate {
		if p.Status == journal.ProposalProposed {
			return p.Index, true
		}
	}
	return 0, false
}

func setupTitle(titles map[string]string, id string) string {
	if title := titles[id]; title != "" {
		return title
	}
	return id
}

// statusNote is what a decided proposal carries into the table's last column:
// the order it became, or the reason it did not.
func statusNote(p journal.Proposal) string {
	if p.OrderID != nil {
		return fmt.Sprintf("order #%d", *p.OrderID)
	}
	return dash(oneLine(p.Reason))
}
