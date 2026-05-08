// Package pipeline runs the shared "login → fetch → download PDFs → render
// CSV + ICS → build report" sequence used by both the run-once binary and the
// recurring binary. It does not render the report to stdout and does not
// drive the payment flow — those are caller-specific concerns.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
	"octopus-autopay/api"
	"octopus-autopay/billpdf"
	"octopus-autopay/client"
	"octopus-autopay/config"
	"octopus-autopay/report"
)

// Result is the output of a successful pipeline run.
type Result struct {
	Report     report.Report
	InvoiceDir string
}

// Run logs in, fetches account/ledgers/bills, downloads + parses every PDF,
// writes the invoice index (CSV) and the calendar (.ics, if any outstanding
// bills) under invoice-download/, and returns the built Report.
//
// Progress is reported on stderr so the run-once binary's existing UX is
// preserved; the recurring binary suppresses stderr via launchd anyway.
func Run(ctx context.Context, cfg config.Config) (Result, error) {
	cli, err := client.New()
	if err != nil {
		return Result{}, err
	}

	fmt.Fprintln(os.Stderr, "→ Login...")
	buildID, err := cli.Login(ctx, cfg.Email, cfg.Password)
	if err != nil {
		return Result{}, err
	}

	fmt.Fprintln(os.Stderr, "→ Account info...")
	viewer, err := api.FetchViewer(ctx, cli)
	if err != nil {
		return Result{}, err
	}
	accountNumber := viewer.Accounts[0].Number

	fmt.Fprintln(os.Stderr, "→ Account properties...")
	account, err := api.FetchAccount(ctx, cli, accountNumber)
	if err != nil {
		return Result{}, err
	}

	wanted := []string{api.LedgerElectricity, api.LedgerGas}
	type ledgerData struct {
		ledgerType string
		ledger     api.Ledger
	}
	var ledgers []ledgerData
	for _, lt := range wanted {
		if l, ok := account.LedgerByType(lt); ok {
			ledgers = append(ledgers, ledgerData{lt, l})
		}
	}

	fmt.Fprintln(os.Stderr, "→ Transactions and bills...")
	paymentsByLedger := map[string][]api.Payment{}
	billsByLedger := map[string][]api.Statement{}
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, ld := range ledgers {
		ld := ld
		g.Go(func() error {
			ps, err := api.FetchTransactions(gctx, cli, accountNumber, ld.ledger.Number)
			if err != nil {
				return err
			}
			mu.Lock()
			paymentsByLedger[ld.ledgerType] = ps
			mu.Unlock()
			return nil
		})
	}
	g.Go(func() error {
		bls, err := api.FetchBills(gctx, cli, buildID, accountNumber)
		if err != nil {
			return err
		}
		mu.Lock()
		for _, b := range bls {
			if b.LedgerType == api.LedgerElectricity || b.LedgerType == api.LedgerGas {
				stmts := make([]api.Statement, 0, len(b.Statements.Edges))
				for _, e := range b.Statements.Edges {
					stmts = append(stmts, e.Node)
				}
				billsByLedger[b.LedgerType] = stmts
			}
		}
		mu.Unlock()
		return nil
	})
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	baseDir, err := config.BaseDir()
	if err != nil {
		return Result{}, err
	}
	invoiceDir := filepath.Join(baseDir, "invoice-download")

	fmt.Fprintf(os.Stderr, "→ Downloading and parsing PDFs (saving to %s)...\n", invoiceDir)
	enrichedByLedger := map[string][]report.EnrichedBill{}
	for _, lt := range wanted {
		stmts := billsByLedger[lt]
		if len(stmts) == 0 {
			continue
		}
		enriched := make([]report.EnrichedBill, len(stmts))
		dlg, dctx := errgroup.WithContext(ctx)
		dlg.SetLimit(4)
		for i := range stmts {
			i := i
			dlg.Go(func() error {
				data, err := billpdf.Download(dctx, cli, stmts[i].PdfURL)
				if err != nil {
					return fmt.Errorf("download statement %d: %w", stmts[i].ID, err)
				}
				if _, err := billpdf.Save(invoiceDir, report.PDFFilename(lt, stmts[i]), data); err != nil {
					return fmt.Errorf("save statement %d: %w", stmts[i].ID, err)
				}
				txt, err := billpdf.ExtractText(data)
				if err != nil {
					return fmt.Errorf("extract statement %d: %w", stmts[i].ID, err)
				}
				parsed, perr := billpdf.ParseBill(txt)
				if perr != nil {
					fmt.Fprintf(os.Stderr, "  warning: statement %d partial parse: %v\n", stmts[i].ID, perr)
				}
				enriched[i] = report.EnrichedBill{
					LedgerType: lt,
					Statement:  stmts[i],
					Parsed:     parsed,
				}
				return nil
			})
		}
		if err := dlg.Wait(); err != nil {
			return Result{}, err
		}
		enrichedByLedger[lt] = report.MatchPayments(lt, enriched, paymentsByLedger[lt])
	}

	if err := writeInvoicesCSV(invoiceDir, account, enrichedByLedger); err != nil {
		return Result{}, err
	}

	rpt := report.Build(accountNumber, account, enrichedByLedger)

	cutoff := 0
	if cfg.Card != nil {
		cutoff = cfg.Card.CutoffDay
	}
	if err := writeInvoicesICS(invoiceDir, rpt, cutoff); err != nil {
		return Result{}, err
	}

	return Result{Report: rpt, InvoiceDir: invoiceDir}, nil
}

func writeInvoicesCSV(invoiceDir string, account api.AccountDetails, enriched map[string][]report.EnrichedBill) error {
	csvPath := filepath.Join(invoiceDir, "invoices.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", csvPath, err)
	}
	defer f.Close()
	if err := report.WriteCSV(f, account, enriched); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	fmt.Fprintf(os.Stderr, "→ Saved invoice index to %s\n", csvPath)
	return nil
}

func writeInvoicesICS(invoiceDir string, rpt report.Report, cutoffDay int) error {
	if len(rpt.CurrentDues.OutstandingBills) == 0 {
		return nil
	}
	icsPath := filepath.Join(invoiceDir, "payments.ics")
	f, err := os.Create(icsPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", icsPath, err)
	}
	defer f.Close()
	if err := report.WriteICS(f, rpt, cutoffDay); err != nil {
		return fmt.Errorf("write ICS: %w", err)
	}
	if cutoffDay > 0 {
		fmt.Fprintf(os.Stderr, "→ Saved calendar with due-date + day-after-cutoff(%d) reminders to %s\n", cutoffDay, icsPath)
	} else {
		fmt.Fprintf(os.Stderr, "→ Saved calendar with due-date reminders to %s\n", icsPath)
	}
	return nil
}
