package brief

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/market"
)

// attachNews makes two calls: one for the watchlist, one market-wide. Zero
// lookback hours turns news off rather than asking for stories since right now.
func attachNews(ctx context.Context, d Deps, in *Input, now time.Time, watch []string, w *warnings) {
	if d.News == nil || d.Cfg.NewsLookbackHours <= 0 {
		return
	}
	since := now.Add(-time.Duration(d.Cfg.NewsLookbackHours) * time.Hour)

	if len(watch) > 0 {
		stories, err := d.News.News(ctx, watch, since, perSymbolHeadlines*len(watch))
		if err != nil {
			w.add("news unavailable: %v", err)
		} else {
			bySymbol := groupBySymbol(stories, watch)
			for i := range in.Watchlist {
				in.Watchlist[i].Headlines = bySymbol[in.Watchlist[i].Symbol]
			}
		}
	}

	stories, err := d.News.News(ctx, nil, since, marketHeadlines)
	if err != nil {
		w.add("market news unavailable: %v", err)
		return
	}
	in.MarketHeadlines = trimStories(stories, marketHeadlines)
}

// groupBySymbol files each story under every watchlist symbol it tags, newest
// first, so one feed call covers the whole list.
func groupBySymbol(stories []market.Headline, watch []string) map[string][]market.Headline {
	wanted := make(map[string]bool, len(watch))
	for _, s := range watch {
		wanted[s] = true
	}
	out := make(map[string][]market.Headline, len(watch))
	for _, story := range sortStories(stories) {
		for _, sym := range story.Symbols {
			sym = strings.ToUpper(sym)
			if wanted[sym] && len(out[sym]) < perSymbolHeadlines {
				out[sym] = append(out[sym], truncateSummary(story))
			}
		}
	}
	return out
}

func trimStories(stories []market.Headline, limit int) []market.Headline {
	sorted := sortStories(stories)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	out := make([]market.Headline, 0, len(sorted))
	for _, s := range sorted {
		out = append(out, truncateSummary(s))
	}
	return out
}

// sortStories puts the newest first and breaks ties on id, so two runs over the
// same feed archive the same briefing.
func sortStories(stories []market.Headline) []market.Headline {
	sorted := slices.Clone(stories)
	slices.SortStableFunc(sorted, func(a, b market.Headline) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return sorted
}

func truncateSummary(h market.Headline) market.Headline {
	h.Summary = clipRunes(h.Summary, summaryRunes)
	return h
}
