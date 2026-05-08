package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
	"octopus-autopay/config"
	"octopus-autopay/payment"
	"octopus-autopay/pipeline"
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

	res, err := pipeline.Run(ctx, cfg)
	if err != nil {
		return err
	}

	if asJSON {
		if err := report.RenderJSON(os.Stdout, res.Report); err != nil {
			return err
		}
	} else {
		if err := report.RenderText(os.Stdout, res.Report); err != nil {
			return err
		}
	}

	maybePay(ctx, cfg, res.Report, res.InvoiceDir)

	if testPayment {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("--test-payment richiede stdin interattivo (Stripe iframe / 3DS hanno bisogno di una finestra Firefox visibile)")
		}
		if err := payment.RunTestPayment(ctx, cfg, res.Report.Account, res.InvoiceDir); err != nil {
			return err
		}
	}
	return nil
}

// maybePay runs the interactive payment flow when the user has enabled autopay
// in their config. Best-effort: errors are surfaced to stderr but don't fail
// the overall run (the report has already been printed and the .ics has
// already been written).
func maybePay(ctx context.Context, cfg config.Config, rpt report.Report, invoiceDir string) {
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
	payment.Run(ctx, cfg, rpt.Account, rpt.CurrentDues.OutstandingBills, invoiceDir, false)
}
