package report

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/api"
	"octopus-autopay/billpdf"
)

// EnrichedBill pairs a Statement (and its parsed PDF) with the matching Payment, if any.
type EnrichedBill struct {
	LedgerType string
	Statement  api.Statement
	Parsed     billpdf.ParsedBill
	Paid       *api.Payment // nil → outstanding
}

// MatchPayments cross-references statements against payments per ledger.
// A bill is "paid" if a Payment exists with the same gross euro amount (within 0.005)
// and createdAt within [firstIssuedAt - 1d, firstIssuedAt + 60d]. The 1-day grace
// covers minor clock skew between PDF generation and payment ingest.
func MatchPayments(ledgerType string, bills []EnrichedBill, payments []api.Payment) []EnrichedBill {
	used := make(map[int]bool, len(payments))
	tolerance := decimal.NewFromFloat(0.005)
	maxDelay := 60 * 24 * time.Hour
	earlyGrace := 24 * time.Hour

	// Newest-first so the most recent statement claims the most recent payment when amounts collide.
	sort.SliceStable(bills, func(i, j int) bool {
		return bills[i].Statement.FirstIssuedAt.After(bills[j].Statement.FirstIssuedAt)
	})

	for i := range bills {
		bills[i].LedgerType = ledgerType
		bestIdx := -1
		var bestDelta time.Duration
		target := bills[i].Parsed.TotalDaPagare
		issued := bills[i].Statement.FirstIssuedAt
		for j, p := range payments {
			if used[j] {
				continue
			}
			diff := p.Amounts.Euros().Sub(target).Abs()
			if diff.GreaterThan(tolerance) {
				continue
			}
			delta := p.CreatedAt.Sub(issued)
			if delta < -earlyGrace || delta > maxDelay {
				continue
			}
			if bestIdx == -1 || abs(delta) < abs(bestDelta) {
				bestIdx = j
				bestDelta = delta
			}
		}
		if bestIdx >= 0 {
			pp := payments[bestIdx]
			bills[i].Paid = &pp
			used[bestIdx] = true
		}
	}

	// Restore chronological ordering for downstream display.
	sort.SliceStable(bills, func(i, j int) bool {
		return bills[i].Statement.FirstIssuedAt.Before(bills[j].Statement.FirstIssuedAt)
	})
	return bills
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
