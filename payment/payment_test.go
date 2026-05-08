package payment

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/report"
)

func TestGroupByLedgerSumsPerLedger(t *testing.T) {
	bills := []report.DueLine{
		{Ledger: "Luce", Amount: decimal.RequireFromString("100.00"), DueDate: time.Now()},
		{Ledger: "Luce", Amount: decimal.RequireFromString("35.98"), DueDate: time.Now()},
		{Ledger: "Gas", Amount: decimal.RequireFromString("106.81"), DueDate: time.Now()},
	}
	got := groupByLedger(bills)
	if v := got["Luce"].StringFixed(2); v != "135.98" {
		t.Errorf("Luce sum = %s, want 135.98", v)
	}
	if v := got["Gas"].StringFixed(2); v != "106.81" {
		t.Errorf("Gas sum = %s, want 106.81", v)
	}
	if len(got) != 2 {
		t.Errorf("groups = %d, want 2", len(got))
	}
}

func TestGroupByLedgerEmpty(t *testing.T) {
	if got := groupByLedger(nil); len(got) != 0 {
		t.Errorf("nil input → %v", got)
	}
}

func TestLowerLedger(t *testing.T) {
	cases := map[string]string{"Luce": "luce", "Gas": "gas", "Other": "Other"}
	for in, want := range cases {
		if got := lowerLedger(in); got != want {
			t.Errorf("lowerLedger(%q) = %q, want %q", in, got, want)
		}
	}
}
