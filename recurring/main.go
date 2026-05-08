// Command octopus-autopay-recurring registers itself as a per-user OS login
// trigger (launchd LaunchAgent / systemd-user timer / Task Scheduler) and,
// when fired by the OS, runs the autopay pipeline unattended. Notifications
// fire on scan failure, new outstanding bills, payment success, payment
// failure — silent otherwise.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	"octopus-autopay/config"
	"octopus-autopay/payment"
	"octopus-autopay/pipeline"
	"octopus-autopay/system"
)

const (
	notifyTitle      = "Octopus Autopay"
	stateFileName    = "state.json"
	scheduledTimeout = 8 * time.Minute
)

type state struct {
	NotifiedBillIDs []int64 `json:"notified_bill_ids"`
}

func (s state) has(id int64) bool {
	for _, x := range s.NotifiedBillIDs {
		if x == id {
			return true
		}
	}
	return false
}

func loadState() state {
	dir, err := config.BaseDir()
	if err != nil {
		return state{}
	}
	b, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		return state{}
	}
	var s state
	_ = json.Unmarshal(b, &s)
	return s
}

func saveState(s state) error {
	dir, err := config.BaseDir()
	if err != nil {
		return err
	}
	sort.Slice(s.NotifiedBillIDs, func(i, j int) bool { return s.NotifiedBillIDs[i] < s.NotifiedBillIDs[j] })
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFileName), b, 0o644)
}

func main() {
	uninstall := flag.Bool("uninstall", false, "rimuovi lo scheduling OS-native e esci")
	flag.Parse()

	if *uninstall {
		if err := system.UninstallSchedule(); err != nil {
			fmt.Fprintln(os.Stderr, "errore disinstallazione:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Scheduling rimosso.")
		return
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		if err := runInteractiveSetup(); err != nil {
			fmt.Fprintln(os.Stderr, "errore:", err)
			os.Exit(1)
		}
		return
	}

	runScheduled()
}

func runInteractiveSetup() error {
	cfg, err := config.LoadOrPrompt()
	if err != nil {
		return err
	}

	if !cfg.AutoPay || cfg.Card == nil {
		fmt.Fprintln(os.Stderr, "L'esecuzione automatica richiede l'autopay con una carta configurata.")
		fmt.Fprintln(os.Stderr, "Riesegui octopus-autopay e completa il setup con la carta, poi rilancia questo binario.")
		return nil
	}

	enable, err := config.PromptSchedule()
	if err != nil {
		return err
	}
	if !enable {
		_ = system.UninstallSchedule()
		cfg.Schedule = &config.ScheduleConfig{Enabled: false}
		_ = config.Save(cfg)
		fmt.Fprintln(os.Stderr, "Esecuzione automatica non abilitata.")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return fmt.Errorf("non posso installare lo scheduling quando lanciato via `go run` (path effimero %s). Builda l'eseguibile e rilancialo da quel path", exe)
	}
	if err := system.InstallSchedule(exe); err != nil {
		return fmt.Errorf("install schedule: %w", err)
	}
	cfg.Schedule = &config.ScheduleConfig{Enabled: true}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "warning: salvataggio config fallito:", err)
	}
	fmt.Fprintf(os.Stderr, "Esecuzione automatica abilitata. Parte ad ogni login e ogni ~6h mentre sei loggato.\n")
	fmt.Fprintf(os.Stderr, "Per disattivare: %s -uninstall\n", filepath.Base(exe))
	return nil
}

func runScheduled() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, cancel2 := context.WithTimeout(ctx, scheduledTimeout)
	defer cancel2()

	cfg, err := config.LoadOrPrompt()
	if err != nil {
		// No TTY → can't recover via prompt. Notify and exit.
		system.Notify(notifyTitle, "Configurazione mancante: lancia il binario dal terminale.")
		return
	}

	res, err := pipeline.Run(ctx, cfg)
	if err != nil {
		system.Notify(notifyTitle, "Scansione fallita: "+truncate(err.Error(), 200))
		return
	}

	st := loadState()
	currentIDs := make([]int64, 0, len(res.Report.CurrentDues.OutstandingBills))
	for _, d := range res.Report.CurrentDues.OutstandingBills {
		currentIDs = append(currentIDs, d.StatementID)
	}

	var newCount int
	for _, id := range currentIDs {
		if !st.has(id) {
			newCount++
		}
	}
	if newCount > 0 {
		system.Notify(notifyTitle, fmt.Sprintf("%d nuove bollette: €%s", newCount, res.Report.CurrentDues.TotalDue.StringFixed(2)))
	}
	st.NotifiedBillIDs = currentIDs
	_ = saveState(st)

	if len(res.Report.CurrentDues.OutstandingBills) == 0 {
		return
	}
	if !cfg.AutoPay || cfg.Card == nil {
		// User opted out of autopay (or removed the card). The new-bill
		// notification above is already enough.
		return
	}
	ok, _ := system.IsScreenAvailable()
	if !ok {
		return // refire later — silent on purpose
	}

	results := payment.Run(ctx, cfg, res.Report.Account, res.Report.CurrentDues.OutstandingBills, res.InvoiceDir, true)
	for _, r := range results {
		switch r.Status {
		case "paid":
			system.Notify(notifyTitle, fmt.Sprintf("Pagato €%s su %s", r.Amount.StringFixed(2), r.Ledger))
		case "errored":
			body := fmt.Sprintf("Pagamento %s fallito", r.Ledger)
			if r.Err != nil {
				body += ": " + truncate(r.Err.Error(), 150)
			}
			system.Notify(notifyTitle, body)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
