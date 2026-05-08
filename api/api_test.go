package api

import (
	"encoding/json"
	"testing"
)

// The user-provided response_listabollette.txt has unescaped quotes inside the
// queryHash strings (a transcription artefact); the real production response is
// valid JSON. Test ExtractAccountBills with a minimal synthetic envelope so the
// suite stays self-contained — fixture files in builder-data/ are gitignored.
func TestExtractAccountBillsSynthetic(t *testing.T) {
	envelope := `{
  "pageProps": {
    "dehydratedState": {
      "queries": [
        {
          "state": {"data": {"unrelated": true}},
          "queryKey": ["session"]
        },
        {
          "state": {
            "data": {
              "account": {
                "ledgers": [
                  {
                    "ledgerType": "ITA_ELECTRICITY_LEDGER",
                    "number": "L-AAA",
                    "statements": {
                      "pageInfo": {"hasNextPage": false, "endCursor": ""},
                      "edgeCount": 1,
                      "totalCount": 1,
                      "edges": [
                        {"node": {
                          "id": 1,
                          "pdfUrl": "https://example.com/x.pdf",
                          "startAt": "2026-02-28T23:00:00+00:00",
                          "endAt": "2026-03-31T22:00:00+00:00",
                          "firstIssuedAt": "2026-04-09T12:02:51.858925+00:00"
                        }}
                      ]
                    }
                  }
                ]
              }
            }
          },
          "queryKey": ["accountBills", {"accountNumber": "A-X"}]
        }
      ]
    }
  }
}`
	var raw BillsResponse
	if err := json.Unmarshal([]byte(envelope), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ledgers, err := ExtractAccountBills(raw)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ledgers) != 1 || ledgers[0].LedgerType != LedgerElectricity {
		t.Fatalf("ledgers = %+v", ledgers)
	}
	if len(ledgers[0].Statements.Edges) != 1 {
		t.Fatalf("edges = %d", len(ledgers[0].Statements.Edges))
	}
	got := ledgers[0].Statements.Edges[0].Node
	if got.PdfURL != "https://example.com/x.pdf" || got.ID != 1 {
		t.Errorf("statement = %+v", got)
	}
}
