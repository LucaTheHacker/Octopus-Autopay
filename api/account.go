package api

import (
	"context"
	"fmt"
	"time"

	"octopus-autopay/client"
)

const accountQuery = `query Account($accountNumber: String!, $isGas: Boolean!, $isElectricity: Boolean!, $includeTariffInfo: Boolean!) {
  account(accountNumber: $accountNumber) {
    id
    createdAt
    ledgers {
      ledgerType
      number
    }
    properties {
      id
      address
      postcode
      electricitySupplyPoints @include(if: $isElectricity) {
        id
        status
        pod
        enrolmentStatus
        supplyStartDate
        product {
          displayName
          params {
            consumptionCharge
            annualStandingCharge
            productType
          }
        }
        agreements(first: 5) {
          edges {
            node {
              id
              validFrom
              validTo
              isActive
              isRollover
              product @include(if: $includeTariffInfo) {
                displayName
                params {
                  consumptionCharge
                  annualStandingCharge
                  productType
                }
              }
            }
          }
        }
      }
      gasSupplyPoints @include(if: $isGas) {
        id
        status
        pdr
        enrolmentStatus
        supplyStartDate
        product {
          displayName
          params {
            consumptionCharge
            annualStandingCharge
            productType
          }
        }
        agreements(first: 5) {
          edges {
            node {
              id
              validFrom
              validTo
              isActive
              isRollover
              product @include(if: $includeTariffInfo) {
                displayName
                params {
                  consumptionCharge
                  annualStandingCharge
                  productType
                }
              }
            }
          }
        }
      }
    }
  }
}`

type AccountResponse struct {
	Account AccountDetails `json:"account"`
}

type AccountDetails struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	Ledgers    []Ledger   `json:"ledgers"`
	Properties []Property `json:"properties"`
}

type Ledger struct {
	LedgerType string `json:"ledgerType"`
	Number     string `json:"number"`
}

type Property struct {
	ID                      string                `json:"id"`
	Address                 string                `json:"address"`
	Postcode                string                `json:"postcode"`
	ElectricitySupplyPoints []ElectricitySupplyPt `json:"electricitySupplyPoints"`
	GasSupplyPoints         []GasSupplyPt         `json:"gasSupplyPoints"`
}

type ElectricitySupplyPt struct {
	ID              string                  `json:"id"`
	Status          string                  `json:"status"`
	POD             string                  `json:"pod"`
	EnrolmentStatus string                  `json:"enrolmentStatus"`
	SupplyStartDate string                  `json:"supplyStartDate"`
	Product         ElectricityProduct      `json:"product"`
	Agreements      ElectricityAgreementsCx `json:"agreements"`
}

type GasSupplyPt struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	PDR             string          `json:"pdr"`
	EnrolmentStatus string          `json:"enrolmentStatus"`
	SupplyStartDate string          `json:"supplyStartDate"`
	Product         GasProduct      `json:"product"`
	Agreements      GasAgreementsCx `json:"agreements"`
}

type ElectricityProduct struct {
	DisplayName string        `json:"displayName"`
	Params      ProductParams `json:"params"`
}

type GasProduct struct {
	DisplayName string        `json:"displayName"`
	Params      ProductParams `json:"params"`
}

type ProductParams struct {
	ConsumptionCharge    string `json:"consumptionCharge"`
	AnnualStandingCharge string `json:"annualStandingCharge"`
	ProductType          string `json:"productType"`
}

type ElectricityAgreementsCx struct {
	Edges []ElectricityAgreementEdge `json:"edges"`
}

type GasAgreementsCx struct {
	Edges []GasAgreementEdge `json:"edges"`
}

type ElectricityAgreementEdge struct {
	Node ElectricityAgreement `json:"node"`
}

type GasAgreementEdge struct {
	Node GasAgreement `json:"node"`
}

type ElectricityAgreement struct {
	ID         int64              `json:"id"`
	ValidFrom  time.Time          `json:"validFrom"`
	ValidTo    time.Time          `json:"validTo"`
	IsActive   bool               `json:"isActive"`
	IsRollover bool               `json:"isRollover"`
	Product    ElectricityProduct `json:"product"`
}

type GasAgreement struct {
	ID         int64      `json:"id"`
	ValidFrom  time.Time  `json:"validFrom"`
	ValidTo    time.Time  `json:"validTo"`
	IsActive   bool       `json:"isActive"`
	IsRollover bool       `json:"isRollover"`
	Product    GasProduct `json:"product"`
}

func FetchAccount(ctx context.Context, c *client.Client, accountNumber string) (AccountDetails, error) {
	var resp AccountResponse
	err := c.GraphQL(ctx, client.GraphQLRequest{
		OperationName: "Account",
		Query:         accountQuery,
		Variables: map[string]any{
			"accountNumber":     accountNumber,
			"isGas":             true,
			"isElectricity":     true,
			"includeTariffInfo": true,
		},
	}, &resp)
	if err != nil {
		return AccountDetails{}, fmt.Errorf("Account query: %w", err)
	}
	return resp.Account, nil
}

func (a AccountDetails) LedgerByType(t string) (Ledger, bool) {
	for _, l := range a.Ledgers {
		if l.LedgerType == t {
			return l, true
		}
	}
	return Ledger{}, false
}
