package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}

func TestOptimalPayDate(t *testing.T) {
	cases := []struct {
		due    string
		cutoff int
		want   string
	}{
		// Cutoff 28, due 2026-04-29 → cutoff falls April 28, pay April 29.
		{"2026-04-29", 28, "2026-04-29"},
		// Cutoff 28, due 2026-05-30 → most recent cutoff before due is May 28, pay May 29.
		{"2026-05-30", 28, "2026-05-29"},
		// Cutoff 5, due 2026-05-04 → May 5 is after due, fall back to April 5+1=April 6.
		{"2026-05-04", 5, "2026-04-06"},
		// Cutoff 31 in February → clamps to last day of Feb.
		{"2026-03-15", 31, "2026-03-01"},
	}
	for _, c := range cases {
		got := optimalPayDate(date(t, c.due), c.cutoff).Format("2006-01-02")
		if got != c.want {
			t.Errorf("optimalPayDate(%s, %d) = %s, want %s", c.due, c.cutoff, got, c.want)
		}
	}
}

func TestWriteICSWithoutCutoff(t *testing.T) {
	r := Report{
		GeneratedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		Account:     "A-X",
		CurrentDues: CurrentDues{
			OutstandingBills: []DueLine{
				{
					StatementID: 10200594,
					Ledger:      "Luce",
					PeriodStart: date(t, "2026-03-01"),
					PeriodEnd:   date(t, "2026-03-31"),
					DueDate:     date(t, "2026-04-29"),
					Amount:      decimal.RequireFromString("135.98"),
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteICS(&buf, r, 0); err != nil {
		t.Fatalf("WriteICS: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:due-10200594@octopus-autopay",
		"DTSTART;VALUE=DATE:20260429",
		"DTEND;VALUE=DATE:20260430",
		"SUMMARY:Octopus Luce", // note € may be elsewhere on the line
		"BEGIN:VALARM",
		"TRIGGER:-P1D",
		"END:VALARM",
		"END:VEVENT",
		"END:VCALENDAR",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
	if strings.Count(out, "BEGIN:VEVENT") != 1 {
		t.Errorf("expected 1 VEVENT (cutoff disabled), got %d", strings.Count(out, "BEGIN:VEVENT"))
	}
	if !strings.HasSuffix(out, "END:VCALENDAR\r\n") {
		t.Errorf("missing CRLF terminator")
	}
}

func TestWriteICSWithCutoffAddsPayEarlyEvent(t *testing.T) {
	r := Report{
		GeneratedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		Account:     "A-X",
		CurrentDues: CurrentDues{
			OutstandingBills: []DueLine{
				{
					StatementID: 10588876,
					Ledger:      "Gas",
					PeriodStart: date(t, "2026-03-01"),
					PeriodEnd:   date(t, "2026-03-31"),
					DueDate:     date(t, "2026-05-30"),
					Amount:      decimal.RequireFromString("106.81"),
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteICS(&buf, r, 28); err != nil {
		t.Fatalf("WriteICS: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "BEGIN:VEVENT") != 2 {
		t.Errorf("expected 2 VEVENT (due + pay-early), got %d", strings.Count(out, "BEGIN:VEVENT"))
	}
	if !strings.Contains(out, "UID:pay-10588876-cutoff28@octopus-autopay") {
		t.Errorf("missing pay-early UID")
	}
	if !strings.Contains(out, "DTSTART;VALUE=DATE:20260529") {
		t.Errorf("pay-early DTSTART should be 2026-05-29: %s", out)
	}
}

func TestEscapeICSText(t *testing.T) {
	got := escapeICSText("a; b, c\\d\nrest")
	want := `a\; b\, c\\d\nrest`
	if got != want {
		t.Errorf("escapeICSText = %q, want %q", got, want)
	}
}

func TestWriteICSLineFolds(t *testing.T) {
	var b strings.Builder
	long := strings.Repeat("a", 200)
	writeICSLine(&b, "DESCRIPTION:"+long)
	out := b.String()
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line longer than 75 octets: %d", len(line))
		}
	}
}
