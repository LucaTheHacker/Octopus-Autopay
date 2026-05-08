package report

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/api"
	"octopus-autopay/billpdf"
)

func TestPDFFilename(t *testing.T) {
	endAt, _ := time.Parse(time.RFC3339, "2026-03-31T22:00:00Z")
	s := api.Statement{ID: 10200594, EndAt: endAt}
	got := PDFFilename(api.LedgerElectricity, s)
	if got != "luce_2026-03_10200594.pdf" {
		t.Errorf("PDFFilename = %q", got)
	}
	got = PDFFilename(api.LedgerGas, s)
	if got != "gas_2026-03_10200594.pdf" {
		t.Errorf("PDFFilename gas = %q", got)
	}
}

func TestWriteCSVRoundTrip(t *testing.T) {
	endAt, _ := time.Parse(time.RFC3339, "2026-03-31T22:00:00Z")
	pmt, _ := time.Parse(time.RFC3339, "2026-04-29T07:00:00Z")
	enriched := map[string][]EnrichedBill{
		api.LedgerElectricity: {
			{
				Statement: api.Statement{ID: 1, EndAt: endAt},
				Parsed: billpdf.ParsedBill{
					TotalDaPagare: decimal.RequireFromString("135.98"),
					FixedCosts:    decimal.RequireFromString("25.91"),
					Consumption:   decimal.RequireFromString("537"),
					Unit:          "kWh",
					OfferName:     "Octopus Fissa 12M",
					OfferCode:     "OCTOFIX",
				},
				Paid: &api.Payment{ID: "p1", CreatedAt: pmt, Title: "Credit card collection", Amounts: api.Amounts{Gross: 13598}},
			},
			{
				Statement: api.Statement{ID: 2, EndAt: endAt},
				Parsed: billpdf.ParsedBill{
					TotalDaPagare: decimal.RequireFromString("99.99"),
					Consumption:   decimal.RequireFromString("400"),
					Unit:          "kWh",
				},
				// no Paid → outstanding
			},
		},
	}
	account := api.AccountDetails{
		Ledgers: []api.Ledger{{LedgerType: api.LedgerElectricity, Number: "L-AAA"}},
		Properties: []api.Property{{
			ElectricitySupplyPoints: []api.ElectricitySupplyPt{{POD: "IT001E1234"}},
		}},
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, account, enriched); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	rdr := csv.NewReader(strings.NewReader(buf.String()))
	rows, err := rdr.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2)", len(rows))
	}
	if rows[0][0] != "ledger" || rows[0][1] != "status" {
		t.Errorf("header[0..1] = %v", rows[0][:2])
	}
	// paid row
	paid := rows[1]
	want := map[int]string{
		0:  "Luce",
		1:  "paid",
		2:  "1",
		9:  "135.98",
		10: "25.91",
		15: "L-AAA",
		16: "IT001E1234",
		17: "POD",
		20: "luce_2026-03_1.pdf",
	}
	for col, exp := range want {
		if paid[col] != exp {
			t.Errorf("paid[%d] = %q, want %q", col, paid[col], exp)
		}
	}
	// outstanding row
	out := rows[2]
	if out[1] != "outstanding" {
		t.Errorf("outstanding row status = %q", out[1])
	}
	if out[18] != "" || out[19] != "" {
		t.Errorf("outstanding row should have empty payment fields, got %q %q", out[18], out[19])
	}
}
