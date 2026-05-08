package report

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// WriteICS emits an RFC 5545 VCALENDAR with one all-day VEVENT per outstanding
// bill (on the bill's due date), plus — when cardCutoffDay > 0 — an extra
// "pay early" VEVENT placed on the day after the most recent credit-card
// statement cutoff that still falls before the due date. The .ics is safe to
// re-import: UIDs are deterministic per statement.
func WriteICS(w io.Writer, r Report, cardCutoffDay int) error {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//octopus-autopay//Octopus Energy Italia//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")

	stamp := r.GeneratedAt.UTC().Format("20060102T150405Z")
	for _, d := range r.CurrentDues.OutstandingBills {
		writeDueEvent(&b, d, stamp)
		if cardCutoffDay > 0 {
			payDay := optimalPayDate(d.DueDate, cardCutoffDay)
			writePayEarlyEvent(&b, d, payDay, cardCutoffDay, stamp)
		}
	}

	b.WriteString("END:VCALENDAR\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeDueEvent(b *strings.Builder, d DueLine, stamp string) {
	uid := fmt.Sprintf("due-%d@octopus-autopay", d.StatementID)
	summary := fmt.Sprintf("Octopus %s — €%s (scadenza)", d.Ledger, d.Amount.StringFixed(2))
	desc := fmt.Sprintf(
		"Bolletta Octopus %s\\nPeriodo %s → %s\\nImporto: €%s\\nScadenza: %s",
		d.Ledger,
		d.PeriodStart.Format("2006-01-02"),
		d.PeriodEnd.Format("2006-01-02"),
		d.Amount.StringFixed(2),
		d.DueDate.Format("2006-01-02"),
	)
	writeAllDayEvent(b, uid, stamp, d.DueDate, summary, desc, "-P1D")
}

func writePayEarlyEvent(b *strings.Builder, d DueLine, payDay time.Time, cutoff int, stamp string) {
	uid := fmt.Sprintf("pay-%d-cutoff%d@octopus-autopay", d.StatementID, cutoff)
	summary := fmt.Sprintf("Pagare Octopus %s — €%s (giorno dopo cutoff)", d.Ledger, d.Amount.StringFixed(2))
	desc := fmt.Sprintf(
		"Pagare la bolletta Octopus %s ora per sfruttare il ciclo carta di credito.\\nImporto: €%s\\nScadenza Octopus: %s\\nCutoff carta: giorno %d",
		d.Ledger,
		d.Amount.StringFixed(2),
		d.DueDate.Format("2006-01-02"),
		cutoff,
	)
	writeAllDayEvent(b, uid, stamp, payDay, summary, desc, "-PT9H")
}

func writeAllDayEvent(b *strings.Builder, uid, stamp string, day time.Time, summary, description, alarm string) {
	dt := day.Format("20060102")
	dtNext := day.AddDate(0, 0, 1).Format("20060102")
	b.WriteString("BEGIN:VEVENT\r\n")
	writeICSLine(b, "UID:"+uid)
	writeICSLine(b, "DTSTAMP:"+stamp)
	writeICSLine(b, "DTSTART;VALUE=DATE:"+dt)
	writeICSLine(b, "DTEND;VALUE=DATE:"+dtNext)
	writeICSLine(b, "SUMMARY:"+escapeICSText(summary))
	writeICSLine(b, "DESCRIPTION:"+escapeICSText(description))
	writeICSLine(b, "STATUS:CONFIRMED")
	writeICSLine(b, "TRANSP:TRANSPARENT")
	b.WriteString("BEGIN:VALARM\r\n")
	writeICSLine(b, "ACTION:DISPLAY")
	writeICSLine(b, "DESCRIPTION:"+escapeICSText(summary))
	writeICSLine(b, "TRIGGER:"+alarm)
	b.WriteString("END:VALARM\r\n")
	b.WriteString("END:VEVENT\r\n")
}

// optimalPayDate returns the day after the most recent occurrence of
// cutoffDay-of-month that still falls strictly before dueDate. Cutoff days
// past month-end (e.g. 31 in February) are clamped to the month's last day.
func optimalPayDate(dueDate time.Time, cutoffDay int) time.Time {
	loc := dueDate.Location()
	year, month := dueDate.Year(), dueDate.Month()
	for offset := 0; offset < 13; offset++ {
		y := year
		m := int(month) - offset
		for m < 1 {
			m += 12
			y--
		}
		cutoff := dayInMonth(y, time.Month(m), cutoffDay, loc)
		pay := cutoff.AddDate(0, 0, 1)
		if !pay.After(dueDate) {
			return pay
		}
	}
	// Fallback: due date itself (shouldn't happen with cutoffDay in 1..31).
	return dueDate
}

func dayInMonth(year int, month time.Month, day int, loc *time.Location) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// escapeICSText escapes commas, semicolons, backslashes, and newlines per RFC 5545.
func escapeICSText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// writeICSLine writes a content line, folding at 75 octets per RFC 5545
// section 3.1. Continuation lines start with a single space.
func writeICSLine(b *strings.Builder, line string) {
	const max = 75
	if len(line) <= max {
		b.WriteString(line)
		b.WriteString("\r\n")
		return
	}
	b.WriteString(line[:max])
	b.WriteString("\r\n")
	rest := line[max:]
	for len(rest) > 0 {
		n := max - 1 // continuation line uses 1 byte for leading space
		if len(rest) < n {
			n = len(rest)
		}
		b.WriteString(" ")
		b.WriteString(rest[:n])
		b.WriteString("\r\n")
		rest = rest[n:]
	}
}
