package report

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/api"
	"octopus-autopay/billpdf"
)

func issued(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}

func TestMatchPaymentsHappyPath(t *testing.T) {
	bills := []EnrichedBill{
		{
			Statement: api.Statement{ID: 1, FirstIssuedAt: issued(t, "2026-04-09T12:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("135.98")},
		},
		{
			Statement: api.Statement{ID: 2, FirstIssuedAt: issued(t, "2026-03-07T19:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("112.42")},
		},
	}
	payments := []api.Payment{
		{ID: "p1", CreatedAt: issued(t, "2026-04-29T07:00:00Z"), Amounts: api.Amounts{Gross: 13598}, Title: "Credit card collection"},
		{ID: "p2", CreatedAt: issued(t, "2026-03-26T11:00:00Z"), Amounts: api.Amounts{Gross: 11242}, Title: "Credit card collection"},
	}
	out := MatchPayments(api.LedgerElectricity, bills, payments)

	for _, b := range out {
		if b.Paid == nil {
			t.Errorf("statement %d not matched", b.Statement.ID)
		}
	}
}

func TestMatchPaymentsAmbiguousAmount(t *testing.T) {
	bills := []EnrichedBill{
		{
			Statement: api.Statement{ID: 1, FirstIssuedAt: issued(t, "2026-01-09T00:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("100.00")},
		},
		{
			Statement: api.Statement{ID: 2, FirstIssuedAt: issued(t, "2026-02-09T00:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("100.00")},
		},
	}
	payments := []api.Payment{
		{ID: "older", CreatedAt: issued(t, "2026-01-29T00:00:00Z"), Amounts: api.Amounts{Gross: 10000}},
		{ID: "newer", CreatedAt: issued(t, "2026-02-28T00:00:00Z"), Amounts: api.Amounts{Gross: 10000}},
	}
	out := MatchPayments(api.LedgerElectricity, bills, payments)
	for _, b := range out {
		if b.Paid == nil {
			t.Fatalf("statement %d unmatched", b.Statement.ID)
		}
		switch b.Statement.ID {
		case 1:
			if b.Paid.ID != "older" {
				t.Errorf("statement 1 matched to %q, want older", b.Paid.ID)
			}
		case 2:
			if b.Paid.ID != "newer" {
				t.Errorf("statement 2 matched to %q, want newer", b.Paid.ID)
			}
		}
	}
}

func TestMatchPaymentsOutstandingBill(t *testing.T) {
	bills := []EnrichedBill{
		{
			Statement: api.Statement{ID: 1, FirstIssuedAt: issued(t, "2026-04-09T12:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("99.99")},
		},
	}
	payments := []api.Payment{
		{ID: "p1", CreatedAt: issued(t, "2026-04-29T00:00:00Z"), Amounts: api.Amounts{Gross: 13598}},
	}
	out := MatchPayments(api.LedgerElectricity, bills, payments)
	if out[0].Paid != nil {
		t.Errorf("expected outstanding bill, got matched to %s", out[0].Paid.ID)
	}
}

func TestMatchPaymentsRejectsTooEarlyOrTooLate(t *testing.T) {
	bills := []EnrichedBill{
		{
			Statement: api.Statement{ID: 1, FirstIssuedAt: issued(t, "2026-04-09T00:00:00Z")},
			Parsed:    billpdf.ParsedBill{TotalDaPagare: decimal.RequireFromString("100.00")},
		},
	}
	tooEarly := []api.Payment{{ID: "early", CreatedAt: issued(t, "2026-04-04T00:00:00Z"), Amounts: api.Amounts{Gross: 10000}}}
	if out := MatchPayments(api.LedgerElectricity, append([]EnrichedBill(nil), bills...), tooEarly); out[0].Paid != nil {
		t.Errorf("matched too-early payment")
	}
	tooLate := []api.Payment{{ID: "late", CreatedAt: issued(t, "2026-06-19T00:00:00Z"), Amounts: api.Amounts{Gross: 10000}}}
	if out := MatchPayments(api.LedgerElectricity, append([]EnrichedBill(nil), bills...), tooLate); out[0].Paid != nil {
		t.Errorf("matched too-late payment")
	}
}
