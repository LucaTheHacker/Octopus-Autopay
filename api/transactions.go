package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"octopus-autopay/client"
)

const transactionsQuery = `query Transactions($accountNumber: String!, $ledgerNumber: String!, $first: Int!, $after: String, $fromDate: Date, $toDate: Date) {
  account(accountNumber: $accountNumber) {
    ledgers(ledgerNumber: $ledgerNumber) {
      ledgerType
      transactions(first: $first, after: $after, fromDate: $fromDate, toDate: $toDate) {
        edges {
          node {
            __typename
            ... on Payment {
              __typename
              id
              title
              createdAt
              isLateFailedPayment
              amounts {
                gross
              }
            }
          }
        }
        pageInfo {
          endCursor
          hasNextPage
          hasPreviousPage
          startCursor
        }
      }
    }
  }
}`

type TransactionsResponse struct {
	Account TransactionsAccount `json:"account"`
}

type TransactionsAccount struct {
	Ledgers []TransactionsLedger `json:"ledgers"`
}

type TransactionsLedger struct {
	LedgerType   string             `json:"ledgerType"`
	Transactions TransactionsConnCx `json:"transactions"`
}

type TransactionsConnCx struct {
	Edges    []TransactionEdge `json:"edges"`
	PageInfo PageInfo          `json:"pageInfo"`
}

type TransactionEdge struct {
	Node Transaction `json:"node"`
}

type PageInfo struct {
	EndCursor       string `json:"endCursor"`
	HasNextPage     bool   `json:"hasNextPage"`
	HasPreviousPage bool   `json:"hasPreviousPage"`
	StartCursor     string `json:"startCursor"`
}

// Transaction is the discriminated union over Payment / Charge / Credit / Refund.
// Only Payment carries values we need; the rest are kept as Typename.
type Transaction struct {
	Typename string
	Payment  *Payment
}

func (t *Transaction) UnmarshalJSON(b []byte) error {
	var head struct {
		Typename string `json:"__typename"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	t.Typename = head.Typename
	if head.Typename == "Payment" {
		var p Payment
		if err := json.Unmarshal(b, &p); err != nil {
			return fmt.Errorf("decode Payment: %w", err)
		}
		t.Payment = &p
	}
	return nil
}

type Payment struct {
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	CreatedAt           time.Time `json:"createdAt"`
	IsLateFailedPayment bool      `json:"isLateFailedPayment"`
	Amounts             Amounts   `json:"amounts"`
}

type Amounts struct {
	// Gross is in integer cents (13598 = 135.98 €).
	Gross int64 `json:"gross"`
}

func (a Amounts) Euros() decimal.Decimal {
	return decimal.NewFromInt(a.Gross).Div(decimal.NewFromInt(100))
}

func FetchTransactions(ctx context.Context, c *client.Client, accountNumber, ledgerNumber string) ([]Payment, error) {
	var payments []Payment
	cursor := ""
	for {
		vars := map[string]any{
			"accountNumber": accountNumber,
			"ledgerNumber":  ledgerNumber,
			"first":         50,
		}
		if cursor != "" {
			vars["after"] = cursor
		}
		var resp TransactionsResponse
		err := c.GraphQL(ctx, client.GraphQLRequest{
			OperationName: "Transactions",
			Query:         transactionsQuery,
			Variables:     vars,
		}, &resp)
		if err != nil {
			return nil, fmt.Errorf("Transactions(%s): %w", ledgerNumber, err)
		}
		if len(resp.Account.Ledgers) == 0 {
			break
		}
		l := resp.Account.Ledgers[0]
		for _, e := range l.Transactions.Edges {
			if e.Node.Payment != nil && !e.Node.Payment.IsLateFailedPayment {
				payments = append(payments, *e.Node.Payment)
			}
		}
		if !l.Transactions.PageInfo.HasNextPage || l.Transactions.PageInfo.EndCursor == "" {
			break
		}
		cursor = l.Transactions.PageInfo.EndCursor
	}
	return payments, nil
}
