package payment

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/config"
	"octopus-autopay/report"
)

// Result captures the outcome of a single ledger payment attempt.
type Result struct {
	Ledger string
	Status string // "paid" | "skipped" | "errored"
	Amount decimal.Decimal
	Err    error
}

// Run prompts the user per ledger and dispatches the browser flow for each
// chosen ledger. Outstanding bills are summed per ledger before asking — the
// payment-una-tantum page accepts a single amount per ledger.
func Run(ctx context.Context, cfg config.Config, accountNumber string, outstanding []report.DueLine, screenshotDir string) []Result {
	groups := groupByLedger(outstanding)
	if len(groups) == 0 {
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	var results []Result

	// Deterministic order: Luce first, then Gas, then anything else alphabetically.
	order := append([]string{"Luce", "Gas"}, sortedKeys(groups)...)
	processed := map[string]bool{}

	for _, ledger := range order {
		if processed[ledger] {
			continue
		}
		amount, ok := groups[ledger]
		if !ok {
			continue
		}
		processed[ledger] = true

		if !confirmPayment(reader, ledger, amount, "") {
			results = append(results, Result{Ledger: ledger, Status: "skipped", Amount: amount})
			continue
		}

		shotPath := filepath.Join(screenshotDir, fmt.Sprintf("payment-%s-%s.png", lowerLedger(ledger), time.Now().Format("20060102-150405")))
		if err := payOne(ctx, cfg, accountNumber, ledger, amount, shotPath); err != nil {
			results = append(results, Result{Ledger: ledger, Status: "errored", Amount: amount, Err: err})
			fmt.Fprintf(os.Stderr, "  pagamento %s fallito: %v\n", ledger, err)
			continue
		}
		results = append(results, Result{Ledger: ledger, Status: "paid", Amount: amount})
		fmt.Fprintf(os.Stderr, "  pagamento %s confermato (€%s). Screenshot: %s\n", ledger, amount.StringFixed(2), shotPath)
	}

	printSummary(results)
	return results
}

func groupByLedger(outstanding []report.DueLine) map[string]decimal.Decimal {
	g := map[string]decimal.Decimal{}
	for _, d := range outstanding {
		g[d.Ledger] = g[d.Ledger].Add(d.Amount)
	}
	return g
}

func sortedKeys(m map[string]decimal.Decimal) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func lowerLedger(s string) string {
	switch s {
	case "Luce":
		return "luce"
	case "Gas":
		return "gas"
	default:
		return s
	}
}

// RunTestPayment drives a fixed €1 charge against the gas ledger via the real
// browser automation, regardless of any outstanding balance. Used by the
// --test-payment CLI flag to verify the end-to-end flow with a small live
// charge. Requires a configured card and a TTY (the Stripe iframe and any 3DS
// challenge need a visible Firefox window).
func RunTestPayment(ctx context.Context, cfg config.Config, accountNumber, screenshotDir string) error {
	if cfg.Card == nil {
		return fmt.Errorf("test payment richiede una carta configurata (riesegui il setup)")
	}
	amount := decimal.NewFromInt(1)
	ledger := "Gas"
	reader := bufio.NewReader(os.Stdin)
	if !confirmPayment(reader, ledger, amount, "TEST — denaro reale") {
		fmt.Fprintln(os.Stderr, "  test payment annullato.")
		return nil
	}
	shotPath := filepath.Join(screenshotDir, fmt.Sprintf("payment-test-%s-%s.png", lowerLedger(ledger), time.Now().Format("20060102-150405")))
	fmt.Fprintf(os.Stderr, "  avvio pagamento di €%s su %s...\n", amount.StringFixed(2), ledger)
	if err := payOne(ctx, cfg, accountNumber, ledger, amount, shotPath); err != nil {
		fmt.Fprintf(os.Stderr, "  test payment fallito: %v\n", err)
		return err
	}
	fmt.Fprintf(os.Stderr, "  test payment confermato. Screenshot: %s\n", shotPath)
	return nil
}

// confirmPayment displays a clear consent box and requires explicit "y" before
// returning true. Used by every browser-driven payment so the user always
// sees the exact ledger and amount being charged before a Firefox window
// opens. The optional `mode` tag (e.g. "TEST") is shown in the box header.
func confirmPayment(reader *bufio.Reader, ledger string, amount decimal.Decimal, mode string) bool {
	header := "Conferma pagamento"
	if mode != "" {
		header = "Conferma pagamento (" + mode + ")"
	}
	fmt.Fprintf(os.Stderr, "\n┌─ %s\n", header)
	fmt.Fprintf(os.Stderr, "│ Bolletta: %s\n", ledger)
	fmt.Fprintf(os.Stderr, "│ Importo:  €%s\n", amount.StringFixed(2))
	fmt.Fprintln(os.Stderr, "└──")
	return Confirm(reader, "Procedere col pagamento? [y/N]: ")
}

func printSummary(results []Result) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "\n— Riepilogo pagamenti —")
	for _, r := range results {
		switch r.Status {
		case "paid":
			fmt.Fprintf(os.Stderr, "  ✓ %-4s €%s pagato\n", r.Ledger, r.Amount.StringFixed(2))
		case "skipped":
			fmt.Fprintf(os.Stderr, "  · %-4s €%s saltato\n", r.Ledger, r.Amount.StringFixed(2))
		case "errored":
			fmt.Fprintf(os.Stderr, "  ✗ %-4s €%s errore: %v\n", r.Ledger, r.Amount.StringFixed(2), r.Err)
		}
	}
}
