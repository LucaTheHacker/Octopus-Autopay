package api

import (
	"context"
	"fmt"

	"octopus-autopay/client"
)

const viewerQuery = `query Viewer {
  viewer {
    id
    fullName
    givenName
    email
    accounts {
      ... on AccountType {
        id
        number
      }
    }
  }
}`

type ViewerResponse struct {
	Viewer Viewer `json:"viewer"`
}

type Viewer struct {
	ID        string    `json:"id"`
	FullName  string    `json:"fullName"`
	GivenName string    `json:"givenName"`
	Email     string    `json:"email"`
	Accounts  []Account `json:"accounts"`
}

type Account struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

func FetchViewer(ctx context.Context, c *client.Client) (Viewer, error) {
	var resp ViewerResponse
	err := c.GraphQL(ctx, client.GraphQLRequest{
		OperationName: "Viewer",
		Query:         viewerQuery,
		Variables:     map[string]any{},
	}, &resp)
	if err != nil {
		return Viewer{}, fmt.Errorf("Viewer query: %w", err)
	}
	if len(resp.Viewer.Accounts) == 0 {
		return Viewer{}, fmt.Errorf("Viewer query returned no accounts")
	}
	return resp.Viewer, nil
}
