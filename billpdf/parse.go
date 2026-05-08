package billpdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/shopspring/decimal"
	"octopus-autopay/client"
)

// ParsedBill is what we extract from a single bill PDF.
type ParsedBill struct {
	TotalDaPagare decimal.Decimal // matches Payment.gross/100 when paid
	TotalBolletta decimal.Decimal // bill body without canone RAI
	FixedCosts    decimal.Decimal // sum of "Quota fissa" + "Quota potenza" + canone RAI
	DueDate       time.Time
	IssueDate     time.Time
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Consumption   decimal.Decimal
	Unit          string // "kWh" or "Smc"
	OfferName     string
	OfferCode     string
	OfferExpiry   time.Time
}

// AvgUnitPrice returns total_da_pagare / consumption (€/kWh or €/Smc).
// This is the all-in average — includes taxes, network, fixed quotas, and RAI.
func (p ParsedBill) AvgUnitPrice() decimal.Decimal {
	if p.Consumption.IsZero() {
		return decimal.Zero
	}
	return p.TotalDaPagare.DivRound(p.Consumption, 4)
}

// AvgUnitPriceExclFixed returns (total - fixed) / consumption — the per-unit
// rate the user effectively paid on consumption-driven items only (energy,
// network charges, taxes, system charges).
func (p ParsedBill) AvgUnitPriceExclFixed() decimal.Decimal {
	if p.Consumption.IsZero() {
		return decimal.Zero
	}
	return p.TotalDaPagare.Sub(p.FixedCosts).DivRound(p.Consumption, 4)
}

// Save writes the downloaded PDF to dir/filename, creating dir (and parents)
// if needed. Existing files are overwritten — statement IDs are stable, so
// re-running the tool is idempotent.
func Save(dir, filename string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func Download(ctx context.Context, c *client.Client, pdfURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/pdf,*/*")
	req.Header.Set("Referer", c.BaseURL+"/area-personale")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download PDF: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("download PDF %s -> %d: %s", pdfURL, resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}

func ExtractText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	var buf bytes.Buffer
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", i, err)
		}
		buf.WriteString(text)
		buf.WriteByte('\n')
	}
	return cleanText(buf.String()), nil
}

// cleanText drops control characters that ledongthuc/pdf occasionally emits
// as token separators in real Octopus invoices — most notably NUL (`\x00`).
// Go's `\s` does not match NUL, so leaving these in place breaks every regex
// that expects whitespace between fields. Tabs, newlines and CRs are kept.
func cleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	rxDueDate     = regexp.MustCompile(`Entro il\s+(\d{2}/\d{2}/\d{4})`)
	rxTotalPagare = regexp.MustCompile(`TOTALE DA PAGARE[\s\S]{0,80}?([\d\.]+,\d{2})\s*€`)
	rxTotalBoll   = regexp.MustCompile(`TOTALE BOLLETTA[\s\S]{0,80}?([\d\.]+,\d{2})\s*€`)
	rxDataFatt    = regexp.MustCompile(`DATA FATTURA[:\s]+(\d{2}/\d{2}/\d{4})`)
	rxPeriodo     = regexp.MustCompile(`PERIODO DI RIFERIMENTO[:\s]+dal\s+(\d{2}/\d{2}/\d{4})\s+al\s+(\d{2}/\d{2}/\d{4})`)
	rxConsumo     = regexp.MustCompile(`CONSUMO FATTURATO[:\s]+([\d\.,]+)\s*(kWh|Smc)`)
	rxOfferName   = regexp.MustCompile(`NOME OFFERTA(?:\s+COMMERCIALE)?[:\s]+([^\r\n]+)`)
	rxOfferCode   = regexp.MustCompile(`CODICE OFFERTA[:\s]+([A-Za-z0-9\s\r\n]+?)(?:PENALI|TIPOLOGIA|DATA|TOTALE|$)`)
	rxOfferExpiry = regexp.MustCompile(`DATA SCADENZA OFFERTA[:\s]+(\d{2}/\d{2}/\d{4})`)

	// Each fixed-quota section in the "Scontrino dell'energia" table is laid out
	// as: "Quota fissa[ (...)]" or "Quota potenza", then "<qty> (mese|gg|kW) x",
	// then "<unit-price> €/(mese|anno|kW)", then "<importo> €". We capture the
	// importo of every parent row and skip the "di cui" sub-rows (which don't
	// follow a "Quota..." header).
	rxFixedQuotaRow = regexp.MustCompile(`(?:Quota fissa(?:\s*\([^)]*\))?|Quota potenza)\s+[\d\.,]+\s*(?:mese|gg|kW)\s+x\s+[\d\.,]+\s*€/(?:mese|anno|kW)\s+([\d\.,]+)\s*€`)
	// RAI canone (electricity bills only) — fixed, independent of consumption.
	rxCanoneRAI = regexp.MustCompile(`Canone di abbonamento alla televisione[^\d]*?([\d\.,]+)\s*€`)
)

const dateLayout = "02/01/2006"

func ParseBill(text string) (ParsedBill, error) {
	var p ParsedBill
	var firstErr error
	setErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	if m := rxDueDate.FindStringSubmatch(text); m != nil {
		t, err := time.Parse(dateLayout, m[1])
		if err != nil {
			setErr(fmt.Errorf("due date: %w", err))
		}
		p.DueDate = t
	} else {
		setErr(fmt.Errorf("due date not found"))
	}

	if m := rxTotalPagare.FindStringSubmatch(text); m != nil {
		v, err := parseDecimalIT(m[1])
		if err != nil {
			setErr(fmt.Errorf("totale da pagare: %w", err))
		}
		p.TotalDaPagare = v
	} else {
		setErr(fmt.Errorf("totale da pagare not found"))
	}

	if m := rxTotalBoll.FindStringSubmatch(text); m != nil {
		v, err := parseDecimalIT(m[1])
		if err == nil {
			p.TotalBolletta = v
		}
	}
	if p.TotalDaPagare.IsZero() && !p.TotalBolletta.IsZero() {
		p.TotalDaPagare = p.TotalBolletta
	}

	if m := rxDataFatt.FindStringSubmatch(text); m != nil {
		t, _ := time.Parse(dateLayout, m[1])
		p.IssueDate = t
	}

	if m := rxPeriodo.FindStringSubmatch(text); m != nil {
		ps, _ := time.Parse(dateLayout, m[1])
		pe, _ := time.Parse(dateLayout, m[2])
		p.PeriodStart = ps
		p.PeriodEnd = pe
	}

	if m := rxConsumo.FindStringSubmatch(text); m != nil {
		v, err := parseDecimalIT(m[1])
		if err != nil {
			setErr(fmt.Errorf("consumo: %w", err))
		}
		p.Consumption = v
		p.Unit = m[2]
	} else {
		setErr(fmt.Errorf("consumo not found"))
	}

	if m := rxOfferName.FindStringSubmatch(text); m != nil {
		p.OfferName = strings.TrimSpace(m[1])
	}

	if m := rxOfferCode.FindStringSubmatch(text); m != nil {
		p.OfferCode = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, m[1])
	}

	if m := rxOfferExpiry.FindStringSubmatch(text); m != nil {
		t, _ := time.Parse(dateLayout, m[1])
		p.OfferExpiry = t
	}

	p.FixedCosts = sumFixedCosts(text)

	return p, firstErr
}

func sumFixedCosts(text string) decimal.Decimal {
	total := decimal.Zero
	for _, m := range rxFixedQuotaRow.FindAllStringSubmatch(text, -1) {
		v, err := parseDecimalIT(m[1])
		if err == nil {
			total = total.Add(v)
		}
	}
	if m := rxCanoneRAI.FindStringSubmatch(text); m != nil {
		if v, err := parseDecimalIT(m[1]); err == nil {
			total = total.Add(v)
		}
	}
	return total
}

// parseDecimalIT parses an Italian-formatted number ("1.234,56") into a Decimal.
func parseDecimalIT(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	return decimal.NewFromString(s)
}
