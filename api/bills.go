package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"octopus-autopay/client"
)

// BillsResponse mirrors the prefetched react-query payload returned by
// GET /_next/data/{buildId}/.../pagamenti-e-bollette.json. Only the
// "accountBills" entry under dehydratedState carries useful data.
type BillsResponse struct {
	PageProps struct {
		DehydratedState struct {
			Queries []json.RawMessage `json:"queries"`
		} `json:"dehydratedState"`
	} `json:"pageProps"`
}

type accountBillsState struct {
	Data struct {
		Account struct {
			Ledgers []BillsLedger `json:"ledgers"`
		} `json:"account"`
	} `json:"data"`
}

type accountBillsQuery struct {
	State    accountBillsState `json:"state"`
	QueryKey []json.RawMessage `json:"queryKey"`
}

type BillsLedger struct {
	LedgerType string         `json:"ledgerType"`
	Number     string         `json:"number"`
	Statements StatementsConn `json:"statements"`
}

type StatementsConn struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	EdgeCount  int             `json:"edgeCount"`
	TotalCount int             `json:"totalCount"`
	Edges      []StatementEdge `json:"edges"`
}

type StatementEdge struct {
	Node Statement `json:"node"`
}

type Statement struct {
	ID            int64     `json:"id"`
	PdfURL        string    `json:"pdfUrl"`
	StartAt       time.Time `json:"startAt"`
	EndAt         time.Time `json:"endAt"`
	FirstIssuedAt time.Time `json:"firstIssuedAt"`
}

func FetchBills(ctx context.Context, c *client.Client, buildID, accountNumber string) ([]BillsLedger, error) {
	u := fmt.Sprintf(
		"%s/_next/data/%s/it/area-personale/account/%s/pagamenti-e-bollette.json?accountNumber=%s",
		c.BaseURL,
		url.PathEscape(buildID),
		url.PathEscape(accountNumber),
		url.QueryEscape(accountNumber),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", c.BaseURL+"/area-personale/account/"+accountNumber+"/pagamenti-e-bollette")

	body, err := c.DoJSON(req)
	if err != nil {
		return nil, fmt.Errorf("fetch bills: %w", err)
	}
	var raw BillsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode bills envelope: %w", err)
	}
	return ExtractAccountBills(raw)
}

func ExtractAccountBills(resp BillsResponse) ([]BillsLedger, error) {
	for _, q := range resp.PageProps.DehydratedState.Queries {
		var probe accountBillsQuery
		if err := json.Unmarshal(q, &probe); err != nil {
			continue
		}
		if len(probe.QueryKey) == 0 {
			continue
		}
		var keyHead string
		if err := json.Unmarshal(probe.QueryKey[0], &keyHead); err != nil {
			continue
		}
		if keyHead != "accountBills" {
			continue
		}
		return probe.State.Data.Account.Ledgers, nil
	}
	return nil, fmt.Errorf("accountBills query not found in dehydrated state")
}
