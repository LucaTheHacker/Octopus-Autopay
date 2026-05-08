package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/api"
)

type Report struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Account     string          `json:"account_number"`
	CurrentDues CurrentDues     `json:"current_dues"`
	History     []LedgerHistory `json:"history"`
}

type CurrentDues struct {
	TotalDue         decimal.Decimal      `json:"total_due"`
	NextDueDate      *time.Time           `json:"next_due_date,omitempty"`
	OutstandingBills []DueLine            `json:"outstanding_bills"`
	LastPaidByLedger map[string]time.Time `json:"last_paid_by_ledger,omitempty"`
}

type DueLine struct {
	StatementID int64           `json:"statement_id"`
	Ledger      string          `json:"ledger"`
	PeriodStart time.Time       `json:"period_start"`
	PeriodEnd   time.Time       `json:"period_end"`
	DueDate     time.Time       `json:"due_date"`
	Amount      decimal.Decimal `json:"amount"`
}

type LedgerHistory struct {
	Ledger                   string          `json:"ledger"`
	Unit                     string          `json:"unit"`
	SupplyPoint              *SupplyPoint    `json:"supply_point,omitempty"`
	Bills                    []HistoryBill   `json:"bills"`
	TotalConsumption         decimal.Decimal `json:"total_consumption"`
	TotalPaid                decimal.Decimal `json:"total_paid"`
	TotalFixedCosts          decimal.Decimal `json:"total_fixed_costs"`
	WeightedAvgUnit          decimal.Decimal `json:"weighted_avg_unit_price"`
	WeightedAvgUnitExclFixed decimal.Decimal `json:"weighted_avg_unit_price_excl_fixed"`
	CurrentOffer             *CurrentOffer   `json:"current_offer,omitempty"`
}

// SupplyPoint identifies a single energy supply: the public POD/PDR code
// distributors use, plus the Octopus-internal ledger number ("L-XXXXXXXXXX")
// that ties bills and transactions to this supply.
type SupplyPoint struct {
	LedgerNumber string `json:"ledger_number"` // Octopus internal "L-..." ledger id
	Code         string `json:"code"`          // POD (electricity) or PDR (gas)
	CodeType     string `json:"code_type"`     // "POD" or "PDR"
}

type CurrentOffer struct {
	DisplayName          string     `json:"display_name"`
	ConsumptionCharge    string     `json:"consumption_charge"`
	AnnualStandingCharge string     `json:"annual_standing_charge"`
	ProductType          string     `json:"product_type"`
	ValidTo              *time.Time `json:"valid_to,omitempty"`
}

type HistoryBill struct {
	StatementID           int64           `json:"statement_id"`
	PeriodStart           time.Time       `json:"period_start"`
	PeriodEnd             time.Time       `json:"period_end"`
	IssueDate             time.Time       `json:"issue_date"`
	Consumption           decimal.Decimal `json:"consumption"`
	Unit                  string          `json:"unit"`
	TotalPaid             decimal.Decimal `json:"total_paid"`
	FixedCosts            decimal.Decimal `json:"fixed_costs"`
	AvgUnitPrice          decimal.Decimal `json:"avg_unit_price"`
	AvgUnitPriceExclFixed decimal.Decimal `json:"avg_unit_price_excl_fixed"`
	OfferName             string          `json:"offer_name"`
	OfferCode             string          `json:"offer_code"`
	PaymentDate           *time.Time      `json:"payment_date,omitempty"`
	PaymentMethod         string          `json:"payment_method,omitempty"`
}

func Build(accountNumber string, account api.AccountDetails, perLedger map[string][]EnrichedBill) Report {
	r := Report{
		GeneratedAt: time.Now(),
		Account:     accountNumber,
		CurrentDues: CurrentDues{
			TotalDue:         decimal.Zero,
			LastPaidByLedger: map[string]time.Time{},
		},
	}

	ledgerOrder := []string{api.LedgerElectricity, api.LedgerGas}
	for _, lt := range ledgerOrder {
		bills := perLedger[lt]
		if len(bills) == 0 {
			continue
		}
		for _, b := range bills {
			if b.Paid != nil {
				if t, ok := r.CurrentDues.LastPaidByLedger[lt]; !ok || b.Paid.CreatedAt.After(t) {
					r.CurrentDues.LastPaidByLedger[lt] = b.Paid.CreatedAt
				}
				continue
			}
			r.CurrentDues.OutstandingBills = append(r.CurrentDues.OutstandingBills, DueLine{
				StatementID: b.Statement.ID,
				Ledger:      LedgerLabel(lt),
				PeriodStart: b.Parsed.PeriodStart,
				PeriodEnd:   b.Parsed.PeriodEnd,
				DueDate:     b.Parsed.DueDate,
				Amount:      b.Parsed.TotalDaPagare,
			})
			r.CurrentDues.TotalDue = r.CurrentDues.TotalDue.Add(b.Parsed.TotalDaPagare)
			if r.CurrentDues.NextDueDate == nil || b.Parsed.DueDate.Before(*r.CurrentDues.NextDueDate) {
				dd := b.Parsed.DueDate
				r.CurrentDues.NextDueDate = &dd
			}
		}

		hist := LedgerHistory{
			Ledger:           LedgerLabel(lt),
			TotalConsumption: decimal.Zero,
			TotalPaid:        decimal.Zero,
			TotalFixedCosts:  decimal.Zero,
		}
		// Newest-first for display
		sort.SliceStable(bills, func(i, j int) bool {
			return bills[i].Statement.FirstIssuedAt.After(bills[j].Statement.FirstIssuedAt)
		})
		for _, b := range bills {
			if b.Paid == nil {
				continue
			}
			if hist.Unit == "" {
				hist.Unit = b.Parsed.Unit
			}
			hist.TotalConsumption = hist.TotalConsumption.Add(b.Parsed.Consumption)
			hist.TotalPaid = hist.TotalPaid.Add(b.Parsed.TotalDaPagare)
			hist.TotalFixedCosts = hist.TotalFixedCosts.Add(b.Parsed.FixedCosts)
			pmt := b.Paid.CreatedAt
			hist.Bills = append(hist.Bills, HistoryBill{
				StatementID:           b.Statement.ID,
				PeriodStart:           b.Parsed.PeriodStart,
				PeriodEnd:             b.Parsed.PeriodEnd,
				IssueDate:             b.Parsed.IssueDate,
				Consumption:           b.Parsed.Consumption,
				Unit:                  b.Parsed.Unit,
				TotalPaid:             b.Parsed.TotalDaPagare,
				FixedCosts:            b.Parsed.FixedCosts,
				AvgUnitPrice:          b.Parsed.AvgUnitPrice(),
				AvgUnitPriceExclFixed: b.Parsed.AvgUnitPriceExclFixed(),
				OfferName:             b.Parsed.OfferName,
				OfferCode:             b.Parsed.OfferCode,
				PaymentDate:           &pmt,
				PaymentMethod:         b.Paid.Title,
			})
		}
		if !hist.TotalConsumption.IsZero() {
			hist.WeightedAvgUnit = hist.TotalPaid.DivRound(hist.TotalConsumption, 4)
			hist.WeightedAvgUnitExclFixed = hist.TotalPaid.Sub(hist.TotalFixedCosts).DivRound(hist.TotalConsumption, 4)
		}
		hist.CurrentOffer = currentOfferFor(lt, account)
		hist.SupplyPoint = supplyPointFor(lt, account)
		r.History = append(r.History, hist)
	}

	sort.SliceStable(r.CurrentDues.OutstandingBills, func(i, j int) bool {
		return r.CurrentDues.OutstandingBills[i].DueDate.Before(r.CurrentDues.OutstandingBills[j].DueDate)
	})
	return r
}

func LedgerLabel(t string) string {
	switch t {
	case api.LedgerElectricity:
		return "Luce"
	case api.LedgerGas:
		return "Gas"
	default:
		return t
	}
}

func supplyPointFor(ledgerType string, a api.AccountDetails) *SupplyPoint {
	ledger, ok := a.LedgerByType(ledgerType)
	if !ok {
		return nil
	}
	for _, prop := range a.Properties {
		switch ledgerType {
		case api.LedgerElectricity:
			for _, sp := range prop.ElectricitySupplyPoints {
				return &SupplyPoint{LedgerNumber: ledger.Number, Code: sp.POD, CodeType: "POD"}
			}
		case api.LedgerGas:
			for _, sp := range prop.GasSupplyPoints {
				return &SupplyPoint{LedgerNumber: ledger.Number, Code: sp.PDR, CodeType: "PDR"}
			}
		}
	}
	return nil
}

func currentOfferFor(ledgerType string, a api.AccountDetails) *CurrentOffer {
	for _, prop := range a.Properties {
		if ledgerType == api.LedgerElectricity {
			for _, sp := range prop.ElectricitySupplyPoints {
				for _, ag := range sp.Agreements.Edges {
					if ag.Node.IsActive {
						vt := ag.Node.ValidTo
						return &CurrentOffer{
							DisplayName:          ag.Node.Product.DisplayName,
							ConsumptionCharge:    ag.Node.Product.Params.ConsumptionCharge,
							AnnualStandingCharge: ag.Node.Product.Params.AnnualStandingCharge,
							ProductType:          ag.Node.Product.Params.ProductType,
							ValidTo:              &vt,
						}
					}
				}
				return &CurrentOffer{
					DisplayName:          sp.Product.DisplayName,
					ConsumptionCharge:    sp.Product.Params.ConsumptionCharge,
					AnnualStandingCharge: sp.Product.Params.AnnualStandingCharge,
					ProductType:          sp.Product.Params.ProductType,
				}
			}
		} else if ledgerType == api.LedgerGas {
			for _, sp := range prop.GasSupplyPoints {
				for _, ag := range sp.Agreements.Edges {
					if ag.Node.IsActive {
						vt := ag.Node.ValidTo
						return &CurrentOffer{
							DisplayName:          ag.Node.Product.DisplayName,
							ConsumptionCharge:    ag.Node.Product.Params.ConsumptionCharge,
							AnnualStandingCharge: ag.Node.Product.Params.AnnualStandingCharge,
							ProductType:          ag.Node.Product.Params.ProductType,
							ValidTo:              &vt,
						}
					}
				}
				return &CurrentOffer{
					DisplayName:          sp.Product.DisplayName,
					ConsumptionCharge:    sp.Product.Params.ConsumptionCharge,
					AnnualStandingCharge: sp.Product.Params.AnnualStandingCharge,
					ProductType:          sp.Product.Params.ProductType,
				}
			}
		}
	}
	return nil
}

func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func RenderText(w io.Writer, r Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Account: %s\nGenerato il %s\n\n", r.Account, r.GeneratedAt.Format("2006-01-02 15:04"))

	fmt.Fprintln(&b, "═══ Quanto devi pagare ora ═══")
	if len(r.CurrentDues.OutstandingBills) == 0 {
		fmt.Fprintln(&b, "Nessun importo dovuto. Tutte le bollette risultano pagate.")
		if len(r.CurrentDues.LastPaidByLedger) > 0 {
			fmt.Fprintln(&b, "Ultimi pagamenti registrati:")
			for ledger, t := range r.CurrentDues.LastPaidByLedger {
				fmt.Fprintf(&b, "  • %-4s: %s\n", LedgerLabel(ledger), t.Format("2006-01-02"))
			}
		}
	} else {
		for _, d := range r.CurrentDues.OutstandingBills {
			fmt.Fprintf(&b, "  • %s — mese %s, scade il %s, importo €%s\n",
				d.Ledger,
				d.PeriodEnd.Format("2006-01"),
				d.DueDate.Format("2006-01-02"),
				d.Amount.StringFixed(2),
			)
		}
		fmt.Fprintf(&b, "\nTotale da pagare ora: €%s", r.CurrentDues.TotalDue.StringFixed(2))
		if r.CurrentDues.NextDueDate != nil {
			fmt.Fprintf(&b, " — prossima scadenza: %s", r.CurrentDues.NextDueDate.Format("2006-01-02"))
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "═══ Storico pagamenti — visione esperta ═══")
	for _, h := range r.History {
		fmt.Fprintf(&b, "\n― %s ―\n", h.Ledger)
		if h.SupplyPoint != nil {
			fmt.Fprintf(&b, "%s: %s — ledger %s\n", h.SupplyPoint.CodeType, h.SupplyPoint.Code, h.SupplyPoint.LedgerNumber)
		}
		if h.CurrentOffer != nil {
			fmt.Fprintf(&b, "Offerta attiva: %s (%s €/%s consumo, %s €/anno fisso)",
				h.CurrentOffer.DisplayName,
				h.CurrentOffer.ConsumptionCharge,
				strings.ToLower(h.Unit),
				h.CurrentOffer.AnnualStandingCharge,
			)
			if h.CurrentOffer.ValidTo != nil {
				fmt.Fprintf(&b, ", scadenza %s", h.CurrentOffer.ValidTo.Format("2006-01-02"))
			}
			fmt.Fprintln(&b)
		}
		if len(h.Bills) == 0 {
			fmt.Fprintln(&b, "Nessuna bolletta pagata trovata.")
			continue
		}
		fmt.Fprintf(&b, "%-9s %-10s %-12s %-15s %-19s %-15s %s\n",
			"Mese", "Consumo", "Importo", "€/unità", "€/unità (var.)", "Pagato il", "Offerta")
		for _, bl := range h.Bills {
			month := bl.PeriodEnd.Format("2006-01")
			pdate := "—"
			if bl.PaymentDate != nil {
				pdate = bl.PaymentDate.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "%-9s %-10s €%-11s %-15s %-19s %-15s %s (%s)\n",
				month,
				fmt.Sprintf("%s %s", bl.Consumption.StringFixed(0), bl.Unit),
				bl.TotalPaid.StringFixed(2),
				fmt.Sprintf("%s €/%s", bl.AvgUnitPrice.StringFixed(4), bl.Unit),
				fmt.Sprintf("%s €/%s", bl.AvgUnitPriceExclFixed.StringFixed(4), bl.Unit),
				pdate,
				bl.OfferName,
				bl.PaymentMethod,
			)
		}
		fmt.Fprintf(&b, "Totale: %s %s consumati, €%s spesi (di cui €%s fissi), media pesata %s €/%s — escl. costi fissi %s €/%s\n",
			h.TotalConsumption.StringFixed(0),
			h.Unit,
			h.TotalPaid.StringFixed(2),
			h.TotalFixedCosts.StringFixed(2),
			h.WeightedAvgUnit.StringFixed(4),
			h.Unit,
			h.WeightedAvgUnitExclFixed.StringFixed(4),
			h.Unit,
		)
		seen := map[string]bool{}
		var offers []string
		for _, bl := range h.Bills {
			key := bl.OfferName + "|" + bl.OfferCode
			if !seen[key] && bl.OfferName != "" {
				seen[key] = true
				if bl.OfferCode != "" {
					offers = append(offers, fmt.Sprintf("%s [%s]", bl.OfferName, bl.OfferCode))
				} else {
					offers = append(offers, bl.OfferName)
				}
			}
		}
		if len(offers) > 0 {
			fmt.Fprintf(&b, "Offerte attraversate: %s\n", strings.Join(offers, "; "))
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}
