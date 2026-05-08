package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"octopus-autopay/api"
)

// PDFFilename produces the stable, sortable name used to save each invoice on
// disk: "luce_2026-03_10200594.pdf" — ledger label, billing month from EndAt,
// statement id. Exposed so main and the CSV exporter agree on filenames.
func PDFFilename(ledgerType string, s api.Statement) string {
	prefix := strings.ToLower(LedgerLabel(ledgerType))
	month := s.EndAt.UTC().Format("2006-01")
	return fmt.Sprintf("%s_%s_%d.pdf", prefix, month, s.ID)
}

// csvHeader is the canonical column order. One row per bill; both paid and
// outstanding invoices are emitted with a status column to distinguish them.
var csvHeader = []string{
	"ledger",
	"status",
	"statement_id",
	"period_start",
	"period_end",
	"issue_date",
	"due_date",
	"consumption",
	"unit",
	"total",
	"fixed_costs",
	"avg_unit_price",
	"avg_unit_price_excl_fixed",
	"offer_name",
	"offer_code",
	"ledger_number",
	"supply_code",
	"supply_code_type",
	"payment_date",
	"payment_method",
	"pdf_filename",
}

// WriteCSV emits one denormalized row per bill, drawing from the per-ledger
// enriched bills (so outstanding invoices are included alongside paid ones).
func WriteCSV(w io.Writer, account api.AccountDetails, perLedger map[string][]EnrichedBill) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	ledgerOrder := []string{api.LedgerElectricity, api.LedgerGas}
	for _, lt := range ledgerOrder {
		bills := perLedger[lt]
		sp := supplyPointFor(lt, account)
		for _, b := range bills {
			row := buildCSVRow(lt, b, sp)
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func buildCSVRow(ledgerType string, b EnrichedBill, sp *SupplyPoint) []string {
	status := "outstanding"
	paymentDate := ""
	paymentMethod := ""
	if b.Paid != nil {
		status = "paid"
		paymentDate = b.Paid.CreatedAt.Format("2006-01-02")
		paymentMethod = b.Paid.Title
	}
	supplyCode, supplyCodeType, ledgerNumber := "", "", ""
	if sp != nil {
		supplyCode = sp.Code
		supplyCodeType = sp.CodeType
		ledgerNumber = sp.LedgerNumber
	}
	return []string{
		LedgerLabel(ledgerType),
		status,
		fmt.Sprintf("%d", b.Statement.ID),
		fmtDate(b.Parsed.PeriodStart),
		fmtDate(b.Parsed.PeriodEnd),
		fmtDate(b.Parsed.IssueDate),
		fmtDate(b.Parsed.DueDate),
		b.Parsed.Consumption.String(),
		b.Parsed.Unit,
		b.Parsed.TotalDaPagare.StringFixed(2),
		b.Parsed.FixedCosts.StringFixed(2),
		b.Parsed.AvgUnitPrice().StringFixed(4),
		b.Parsed.AvgUnitPriceExclFixed().StringFixed(4),
		b.Parsed.OfferName,
		b.Parsed.OfferCode,
		ledgerNumber,
		supplyCode,
		supplyCodeType,
		paymentDate,
		paymentMethod,
		PDFFilename(ledgerType, b.Statement),
	}
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
