package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
	"octopus-autopay/api"
	"octopus-autopay/billpdf"
	"octopus-autopay/client"
	"octopus-autopay/config"
	"octopus-autopay/payment"
	"octopus-autopay/report"
)

func main() {
	asJSON := flag.Bool("json", false, "emit the report as JSON instead of plain text")
	timeout := flag.Duration("timeout", 90*time.Second, "overall pipeline timeout")
	testPayment := flag.Bool("test-payment", false, "TEST MODE: drive a €1 payment for the gas ledger via the real browser flow regardless of any outstanding balance (real money, requires card)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, cancel2 := context.WithTimeout(ctx, *timeout)
	defer cancel2()

	if err := run(ctx, *asJSON, *testPayment); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, asJSON, testPayment bool) error {
	cfg, err := config.LoadOrPrompt()
	if err != nil {
		return err
	}
	cli, err := client.New()
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "→ Login...")
	buildID, err := cli.Login(ctx, cfg.Email, cfg.Password)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "→ Account info...")
	viewer, err := api.FetchViewer(ctx, cli)
	if err != nil {
		return err
	}
	accountNumber := viewer.Accounts[0].Number

	fmt.Fprintln(os.Stderr, "→ Account properties...")
	account, err := api.FetchAccount(ctx, cli, accountNumber)
	if err != nil {
		return err
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
		return err
	}

	baseDir, err := config.BaseDir()
	if err != nil {
		return err
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
			return err
		}
		enrichedByLedger[lt] = report.MatchPayments(lt, enriched, paymentsByLedger[lt])
	}

	if err := writeInvoicesCSV(invoiceDir, account, enrichedByLedger); err != nil {
		return err
	}

	rpt := report.Build(accountNumber, account, enrichedByLedger)

	// ICS is written before the report is rendered (and before any payment
	// prompt) so the .ics file is always available to the user on disk
	// regardless of whether the rendering succeeds or whether they choose to
	// pay. Skipped entirely when there are no outstanding bills — UIDs are
	// stable per statement ID, so re-imports of the same .ics dedupe in any
	// calendar app.
	cutoff := 0
	if cfg.Card != nil {
		cutoff = cfg.Card.CutoffDay
	}
	if err := writeInvoicesICS(invoiceDir, rpt, cutoff); err != nil {
		return err
	}

	if asJSON {
		if err := report.RenderJSON(os.Stdout, rpt); err != nil {
			return err
		}
	} else {
		if err := report.RenderText(os.Stdout, rpt); err != nil {
			return err
		}
	}

	maybePay(ctx, cfg, accountNumber, rpt, invoiceDir)

	if testPayment {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("--test-payment richiede stdin interattivo (Stripe iframe / 3DS hanno bisogno di una finestra Firefox visibile)")
		}
		if err := payment.RunTestPayment(ctx, cfg, accountNumber, invoiceDir); err != nil {
			return err
		}
	}
	return nil
}

// maybePay runs the interactive payment flow when the user has enabled autopay
// in their config. Best-effort: errors are surfaced to stderr but don't fail
// the overall run (the report has already been printed and the .ics has
// already been written).
func maybePay(ctx context.Context, cfg config.Config, accountNumber string, rpt report.Report, invoiceDir string) {
	if !cfg.AutoPay {
		return
	}
	if len(rpt.CurrentDues.OutstandingBills) == 0 {
		return
	}
	if cfg.Card == nil {
		fmt.Fprintln(os.Stderr, "\nAutopay abilitato ma carta non configurata. Riesegui il setup per aggiungerla.")
		return
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "\nAutopay abilitato ma stdin non interattivo; salto il prompt.")
		return
	}
	payment.Run(ctx, cfg, accountNumber, rpt.CurrentDues.OutstandingBills, invoiceDir)
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
		// No outstanding bills → no calendar to write. Any prior payments.ics
		// is left in place; once the user imports it, calendars dedupe future
		// re-imports by the deterministic per-statement UID.
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
