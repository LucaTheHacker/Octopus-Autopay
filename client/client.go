package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"
)

const (
	BaseURL   = "https://octopusenergy.it"
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	return &Client{
		BaseURL: BaseURL,
		HTTP: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}, nil
}

// DoJSON sends req with the standard headers and returns the body if 2xx.
func (c *Client) DoJSON(req *http.Request) ([]byte, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	req.Header.Set("Accept-Language", "it-IT,it;q=0.9")
	req.Header.Set("Origin", c.BaseURL)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s -> %d: %s", req.Method, req.URL.String(), resp.StatusCode, truncate(string(body), 256))
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ----- auth -----

var nextDataPattern = regexp.MustCompile(`<script[^>]+id="__NEXT_DATA__"[^>]*>([\s\S]*?)</script>`)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginEnvelope struct {
	Data string `json:"data"`
}

// Login fetches /login (to extract the Next.js buildId) and posts credentials.
// Returns the buildId for later use by the bills endpoint.
func (c *Client) Login(ctx context.Context, email, password string) (buildID string, err error) {
	buildID, err = c.fetchBuildID(ctx)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(loginRequest{Email: email, Password: password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Referer", c.BaseURL+"/login")

	respBody, err := c.DoJSON(req)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	var env loginEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return "", fmt.Errorf("login: parse response: %w", err)
	}
	if !strings.EqualFold(env.Data, "Success") {
		return "", fmt.Errorf("login failed: %s", env.Data)
	}
	return buildID, nil
}

func (c *Client) fetchBuildID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/login", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET /login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET /login -> %d", resp.StatusCode)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("read /login: %w", err)
	}
	id, err := ExtractBuildID(buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("extract buildId: %w", err)
	}
	return id, nil
}

func ExtractBuildID(html []byte) (string, error) {
	m := nextDataPattern.FindSubmatch(html)
	if m == nil {
		return "", fmt.Errorf("__NEXT_DATA__ script tag not found")
	}
	var nd struct {
		BuildID string `json:"buildId"`
	}
	if err := json.Unmarshal(m[1], &nd); err != nil {
		return "", fmt.Errorf("parse __NEXT_DATA__: %w", err)
	}
	if nd.BuildID == "" {
		return "", fmt.Errorf("buildId missing from __NEXT_DATA__")
	}
	return nd.BuildID, nil
}

// ----- GraphQL -----

type GraphQLRequest struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
}

type graphQLEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

func (c *Client) GraphQL(ctx context.Context, req GraphQLRequest, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/graphql/kraken", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/graphql-response+json, application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Referer", c.BaseURL+"/area-personale")

	respBody, err := c.DoJSON(httpReq)
	if err != nil {
		return err
	}
	var env graphQLEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return fmt.Errorf("parse graphql envelope: %w", err)
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("graphql %s: %s", req.OperationName, env.Errors[0].Message)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("graphql %s: empty data", req.OperationName)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode %s data: %w", req.OperationName, err)
	}
	return nil
}
