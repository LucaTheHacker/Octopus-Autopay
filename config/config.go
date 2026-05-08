package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type Config struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// AutoPay, when true, makes the CLI ask interactively whether to pay any
	// outstanding bills at the end of each run. Set during first-run setup;
	// can be toggled later by hand-editing octopus-autopay.json.
	AutoPay bool         `json:"auto_pay,omitempty"`
	Card    *CardDetails `json:"card,omitempty"`
}

// CardDetails are saved alongside credentials when the user opts into autopay.
// All fields are validated on entry and on load — a hand-edited config that
// breaks the rules will be reported and the bad fields ignored at autopay time
// (the rest of the CLI keeps working).
type CardDetails struct {
	Number     string `json:"number"`    // digits only; 13–19 typical, 15 for AMEX
	ExpMonth   string `json:"exp_month"` // "MM"
	ExpYear    string `json:"exp_year"`  // "YY"
	CVC        string `json:"cvc"`       // 3 digits, or 4 for AMEX
	HolderName string `json:"holder_name"`
	// CutoffDay is the credit-card statement cutoff day-of-month (1–31).
	// When set, the .ics calendar export adds a "pay the day after the cutoff"
	// event before each Octopus due date, so payments land at the start of a
	// new credit-card cycle (maximum float). 0 means "not configured".
	CutoffDay int `json:"cutoff_day,omitempty"`
}

// IsAMEX reports whether the card's IIN identifies it as American Express.
func (c CardDetails) IsAMEX() bool {
	return strings.HasPrefix(c.Number, "34") || strings.HasPrefix(c.Number, "37")
}

// Validate enforces the format rules so a malformed card never reaches the
// browser flow.
func (c CardDetails) Validate() error {
	if !allDigits(c.Number) || len(c.Number) < 13 || len(c.Number) > 19 {
		return fmt.Errorf("card number must be 13–19 digits")
	}
	wantCVC := 3
	if c.IsAMEX() {
		wantCVC = 4
	}
	if !allDigits(c.CVC) || len(c.CVC) != wantCVC {
		return fmt.Errorf("CVC must be %d digits for this card", wantCVC)
	}
	if !allDigits(c.ExpMonth) || len(c.ExpMonth) != 2 {
		return fmt.Errorf("exp month must be 2 digits")
	}
	m, _ := strconv.Atoi(c.ExpMonth)
	if m < 1 || m > 12 {
		return fmt.Errorf("exp month %q out of range", c.ExpMonth)
	}
	if !allDigits(c.ExpYear) || len(c.ExpYear) != 2 {
		return fmt.Errorf("exp year must be 2 digits")
	}
	y, _ := strconv.Atoi(c.ExpYear)
	curr := time.Now().Year() % 100
	if y < curr-2 {
		return fmt.Errorf("exp year %q looks too old", c.ExpYear)
	}
	if strings.TrimSpace(c.HolderName) == "" {
		return fmt.Errorf("cardholder name is required")
	}
	if c.CutoffDay < 0 || c.CutoffDay > 31 {
		return fmt.Errorf("cutoff day must be between 1 and 31 (got %d)", c.CutoffDay)
	}
	return nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// BaseDir returns the directory where octopus-autopay should keep its files
// (config and downloaded invoices). It defaults to the directory of the running
// executable; when invoked via `go run` (transient build dir that's deleted
// between runs) it falls back to the current working directory so artefacts
// persist.
func BaseDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	if isTransientBuildDir(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("locate cwd: %w", err)
		}
		dir = cwd
	}
	return dir, nil
}

// Path returns the absolute path to octopus-autopay.json under BaseDir.
func Path() (string, error) {
	dir, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "octopus-autopay.json"), nil
}

// isTransientBuildDir reports whether a directory looks like a `go run` /
// `go test` temporary build location whose contents are deleted on exit.
func isTransientBuildDir(dir string) bool {
	return strings.Contains(dir, string(filepath.Separator)+"go-build")
}

func LoadOrPrompt() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	cfg, err := read(path)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg, err = prompt()
	if err != nil {
		return Config{}, err
	}
	if err := write(path, cfg); err != nil {
		return Config{}, fmt.Errorf("write config %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "Saved credentials to %s\n", path)
	return cfg, nil
}

func read(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Email == "" || cfg.Password == "" {
		return Config{}, fmt.Errorf("config %s is missing email or password", path)
	}
	return cfg, nil
}

func write(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func prompt() (Config, error) {
	path, _ := Path()
	fmt.Fprintf(os.Stderr, "First-time setup — credentials will be saved to %s (mode 0600).\n", path)
	reader := bufio.NewReader(os.Stdin)

	email, err := promptLine(reader, "Octopus Energy email: ")
	if err != nil {
		return Config{}, err
	}
	if email == "" {
		return Config{}, errors.New("email is required")
	}

	fmt.Fprint(os.Stderr, "Octopus Energy password: ")
	pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return Config{}, fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimSpace(string(pwBytes))
	if password == "" {
		return Config{}, errors.New("password is required")
	}

	cfg := Config{Email: email, Password: password}

	enable, err := promptYesNo(reader, "Abilitare l'autopay (chiede a fine report se pagare le bollette aperte; richiede salvare i dati della carta nel config)? [y/N]: ")
	if err != nil {
		return Config{}, err
	}
	if enable {
		card, err := promptCard(reader)
		if err != nil {
			return Config{}, err
		}
		cfg.Card = &card
		cfg.AutoPay = true
	}

	return cfg, nil
}

func promptCard(reader *bufio.Reader) (CardDetails, error) {
	for {
		number, err := promptLine(reader, "Numero carta (solo cifre, spazi e trattini ignorati): ")
		if err != nil {
			return CardDetails{}, err
		}
		number = stripCardNumber(number)
		card := CardDetails{Number: number}
		if !allDigits(number) || len(number) < 13 || len(number) > 19 {
			fmt.Fprintln(os.Stderr, "  numero non valido (13–19 cifre)")
			continue
		}
		if card.IsAMEX() {
			fmt.Fprintln(os.Stderr, "  AMEX rilevata: CVC a 4 cifre")
		}

		exp, err := promptLine(reader, "Scadenza (MM/YY): ")
		if err != nil {
			return CardDetails{}, err
		}
		mm, yy, ok := splitExpiry(exp)
		if !ok {
			fmt.Fprintln(os.Stderr, "  scadenza non valida")
			continue
		}
		card.ExpMonth = mm
		card.ExpYear = yy

		fmt.Fprint(os.Stderr, "CVC: ")
		cvcBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return CardDetails{}, fmt.Errorf("read cvc: %w", err)
		}
		card.CVC = strings.TrimSpace(string(cvcBytes))

		holder, err := promptLine(reader, "Intestatario carta: ")
		if err != nil {
			return CardDetails{}, err
		}
		card.HolderName = holder

		cutoffStr, err := promptLine(reader, "Giorno cutoff carta di credito (1-31, vuoto per saltare): ")
		if err != nil {
			return CardDetails{}, err
		}
		if cutoffStr != "" {
			day, err := strconv.Atoi(cutoffStr)
			if err != nil || day < 1 || day > 31 {
				fmt.Fprintln(os.Stderr, "  cutoff non valido (1-31) — riprova")
				continue
			}
			card.CutoffDay = day
		}

		if err := card.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "  carta non valida: %v — riprova\n", err)
			continue
		}
		return card, nil
	}
}

func promptLine(r *bufio.Reader, msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	s, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func promptYesNo(r *bufio.Reader, msg string) (bool, error) {
	s, err := promptLine(r, msg)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(s) {
	case "y", "yes", "s", "si", "sì":
		return true, nil
	}
	return false, nil
}

func stripCardNumber(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitExpiry parses "MM/YY" or "MMYY" into ("MM", "YY", true).
func splitExpiry(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	if i := strings.IndexAny(s, "/-"); i >= 0 {
		mm := s[:i]
		yy := s[i+1:]
		if len(yy) == 4 {
			yy = yy[2:]
		}
		if len(mm) == 2 && len(yy) == 2 && allDigits(mm) && allDigits(yy) {
			return mm, yy, true
		}
		return "", "", false
	}
	if len(s) == 4 && allDigits(s) {
		return s[:2], s[2:], true
	}
	return "", "", false
}
