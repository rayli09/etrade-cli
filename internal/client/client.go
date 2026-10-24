// Package client is a Go client for the E*TRADE Developer Platform REST
// API v1. Endpoint paths, parameter names, and defaults follow the
// official API documentation at developer.etrade.com (the reference pages
// hosted at apisb.etrade.com/docs) and were cross-checked against the
// pyetrade Python wrapper.
//
// Every call is signed with OAuth 1.0a (HMAC-SHA1) using the consumer
// key/secret and the access-token pair from the token file. Response
// bodies are returned raw, as the official docs define both JSON and XML
// shapes; the CLI requests JSON (the .json path suffix on reads, as in
// pyetrade and the official examples, plus an Accept header) unless
// --format xml is selected.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rayli09/etrade-cli/internal/auth"
)

// DefaultTimeout for API calls.
const DefaultTimeout = 30 * time.Second

// TokenSource provides the access-token pair for signing.
// *auth.Manager satisfies this interface.
type TokenSource interface {
	AccessToken() (string, string, error)
}

// Response is the result of an API call: status, headers, raw body.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// IsError reports whether the status code indicates an error.
func (r *Response) IsError() bool { return r.StatusCode >= 400 }

// JSON decodes the response body into v.
func (r *Response) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Client speaks to one E*TRADE environment (production or sandbox).
type Client struct {
	BaseURL        string // defaults to auth.ProdBaseURL / SandboxBaseURL by Sandbox
	Sandbox        bool
	Format         string // "json" (default) or "xml"
	ConsumerKey    string
	ConsumerSecret string
	Tokens         TokenSource
	HTTP           *http.Client
	Timeout        time.Duration

	Nonce     func() string
	Timestamp func() int64
}

// New builds a Client.
func New(consumerKey, consumerSecret string, tokens TokenSource) *Client {
	return &Client{ConsumerKey: consumerKey, ConsumerSecret: consumerSecret, Tokens: tokens}
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimSuffix(c.BaseURL, "/")
	}
	if c.Sandbox {
		return auth.SandboxBaseURL
	}
	return auth.ProdBaseURL
}

func (c *Client) format() string {
	if c.Format == "xml" {
		return "xml"
	}
	return "json"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// jsonPath appends the .json suffix E*TRADE uses to select a JSON
// response for read endpoints (and for order preview/place, per the
// official examples). With --format xml the path is left bare.
func (c *Client) jsonPath(path string) string {
	if c.format() == "json" {
		return path + ".json"
	}
	return path
}

// do performs one signed request. query values that are empty are dropped
// by the endpoint methods before they get here. jsonBody, when non-nil,
// is marshaled and sent as application/json; JSON bodies do not
// participate in the OAuth signature (RFC 5849 signs form bodies and
// query parameters only).
func (c *Client) do(ctx context.Context, method, path string,
	query url.Values, jsonBody any) (*Response, error) {
	u := c.baseURL() + path
	var bodyBytes []byte
	if jsonBody != nil {
		data, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, err
		}
		bodyBytes = data
	}
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured")
	}
	tok, tokSecret, err := c.Tokens.AccessToken()
	if err != nil {
		return nil, err
	}
	signer := &auth.Signer{
		ConsumerKey:    c.ConsumerKey,
		ConsumerSecret: c.ConsumerSecret,
		Token:          tok,
		TokenSecret:    tokSecret,
		Nonce:          c.Nonce,
		Timestamp:      c.Timestamp,
	}
	header, err := signer.AuthorizationHeader(method, u, query, nil, nil)
	if err != nil {
		return nil, err
	}
	full := u
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", header)
	if c.format() == "json" {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "application/xml")
	}
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

// get is do() for a read.
func (c *Client) get(ctx context.Context, path string, query url.Values) (*Response, error) {
	return c.do(ctx, http.MethodGet, c.jsonPath(path), query, nil)
}

// setParam sets a query parameter only when value is non-empty.
func setParam(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// boolParam renders an optional boolean the way the API expects it.
func boolParam(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

////////////////////////////////////////////////////////////////////////////
// Accounts — /v1/accounts

// ListAccounts returns the current user's brokerage accounts.
// GET /v1/accounts/list
func (c *Client) ListAccounts(ctx context.Context) (*Response, error) {
	return c.get(ctx, "/v1/accounts/list", nil)
}

// BalanceOptions are the query parameters of Get Account Balances.
type BalanceOptions struct {
	InstType    string // default BROKERAGE
	AccountType string // optional registered account type
	RealTimeNAV *bool  // optional
}

// GetAccountBalance returns balance information for an account.
// GET /v1/accounts/{accountIdKey}/balance
func (c *Client) GetAccountBalance(ctx context.Context, accountIDKey string,
	o BalanceOptions) (*Response, error) {
	q := url.Values{}
	inst := o.InstType
	if inst == "" {
		inst = "BROKERAGE"
	}
	q.Set("instType", inst)
	setParam(q, "accountType", o.AccountType)
	setParam(q, "realTimeNAV", boolParam(o.RealTimeNAV))
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+"/balance", q)
}

// PortfolioOptions are the query parameters of View Portfolio.
type PortfolioOptions struct {
	Count          int    // positions per page (API default 50)
	SortBy         string // column to sort by
	SortOrder      string // ASC or DESC
	PageNumber     int
	MarketSession  string // REGULAR or EXTENDED
	TotalsRequired *bool
	LotsRequired   *bool
	View           string // PERFORMANCE, FUNDAMENTAL, OPTIONSWATCH, QUICK, COMPLETE
}

// GetAccountPortfolio returns the positions of an account.
// GET /v1/accounts/{accountIdKey}/portfolio
func (c *Client) GetAccountPortfolio(ctx context.Context, accountIDKey string,
	o PortfolioOptions) (*Response, error) {
	q := url.Values{}
	if o.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", o.Count))
	}
	setParam(q, "sortBy", o.SortBy)
	setParam(q, "sortOrder", o.SortOrder)
	if o.PageNumber > 0 {
		q.Set("pageNumber", fmt.Sprintf("%d", o.PageNumber))
	}
	setParam(q, "marketSession", o.MarketSession)
	setParam(q, "totalsRequired", boolParam(o.TotalsRequired))
	setParam(q, "lotsRequired", boolParam(o.LotsRequired))
	setParam(q, "view", o.View)
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+"/portfolio", q)
}

// GetPortfolioPositionLot returns the lot details of one portfolio
// position (the "portfolio position lot details" view of View Portfolio).
// GET /v1/accounts/{accountIdKey}/portfolio/{positionId}
func (c *Client) GetPortfolioPositionLot(ctx context.Context,
	accountIDKey, positionID string) (*Response, error) {
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+
		"/portfolio/"+url.PathEscape(positionID), nil)
}

// TransactionsOptions are the query parameters of List Transactions.
// Dates are MMDDYYYY strings, the API's format.
type TransactionsOptions struct {
	StartDate string // MMDDYYYY
	EndDate   string // MMDDYYYY
	SortOrder string // ASC or DESC
	Marker    string
	Count     int
}

// ListTransactions returns transactions for an account (two years of
// history). GET /v1/accounts/{accountIdKey}/transactions
func (c *Client) ListTransactions(ctx context.Context, accountIDKey string,
	o TransactionsOptions) (*Response, error) {
	q := url.Values{}
	setParam(q, "startDate", o.StartDate)
	setParam(q, "endDate", o.EndDate)
	setParam(q, "sortOrder", o.SortOrder)
	setParam(q, "marker", o.Marker)
	if o.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", o.Count))
	}
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+"/transactions", q)
}

// GetTransactionDetails returns one transaction, optionally from a store.
// GET /v1/accounts/{accountIdKey}/transactions/{transactionId}
func (c *Client) GetTransactionDetails(ctx context.Context,
	accountIDKey, transactionID, storeID string) (*Response, error) {
	q := url.Values{}
	setParam(q, "storeId", storeID)
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+
		"/transactions/"+url.PathEscape(transactionID), q)
}

////////////////////////////////////////////////////////////////////////////
// Alerts — /v1/user/alerts

// AlertsOptions are the query parameters of List Alerts.
type AlertsOptions struct {
	Count     int    // default 25, max 300
	Category  string // STOCK, ACCOUNT
	Status    string // READ, UNREAD, DELETED
	Direction string // ASC or DESC (by create date)
	Search    string // subject search text
}

// ListAlerts returns alerts for the current user.
// GET /v1/user/alerts
func (c *Client) ListAlerts(ctx context.Context, o AlertsOptions) (*Response, error) {
	q := url.Values{}
	if o.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", o.Count))
	}
	setParam(q, "category", o.Category)
	setParam(q, "status", o.Status)
	setParam(q, "direction", o.Direction)
	setParam(q, "search", o.Search)
	return c.get(ctx, "/v1/user/alerts", q)
}

// GetAlertDetails returns one alert. GET /v1/user/alerts/{id}
func (c *Client) GetAlertDetails(ctx context.Context, alertID string,
	htmlTags *bool) (*Response, error) {
	q := url.Values{}
	setParam(q, "htmlTags", boolParam(htmlTags))
	return c.get(ctx, "/v1/user/alerts/"+url.PathEscape(alertID), q)
}

// DeleteAlerts deletes one or more alerts (the API takes a comma-joined
// list of alert IDs in the path). DELETE /v1/user/alerts/{id,...}
// This is a mutating call; the CLI guards it with dry-run by default.
func (c *Client) DeleteAlerts(ctx context.Context, alertIDs []string) (*Response, error) {
	esc := make([]string, len(alertIDs))
	for i, id := range alertIDs {
		esc[i] = url.PathEscape(id)
	}
	return c.do(ctx, http.MethodDelete,
		"/v1/user/alerts/"+strings.Join(esc, ","), nil, nil)
}

////////////////////////////////////////////////////////////////////////////
// Market — /v1/market

// LookupProduct searches securities by full or partial company name.
// GET /v1/market/lookup/{search}
func (c *Client) LookupProduct(ctx context.Context, search string) (*Response, error) {
	return c.get(ctx, "/v1/market/lookup/"+url.PathEscape(search), nil)
}

// QuoteOptions are the query parameters of Get Quotes.
type QuoteOptions struct {
	DetailFlag           string // FUNDAMENTAL, INTRADAY, OPTIONS, WEEK_52, MF_DETAIL, ALL
	RequireEarningsDate  *bool
	SkipMiniOptionsCheck *bool
}

// GetQuote returns quotes for one or more symbols (max 25; option symbols
// use underlying:year:month:day:type:strike format).
// GET /v1/market/quote/{symbols}
func (c *Client) GetQuote(ctx context.Context, symbols []string,
	o QuoteOptions) (*Response, error) {
	q := url.Values{}
	setParam(q, "detailFlag", o.DetailFlag)
	setParam(q, "requireEarningsDate", boolParam(o.RequireEarningsDate))
	setParam(q, "skipMiniOptionsCheck", boolParam(o.SkipMiniOptionsCheck))
	joined := strings.Join(symbols, ",")
	return c.get(ctx, "/v1/market/quote/"+joined, q)
}

// OptionChainsOptions are the query parameters of Get Option Chains; the
// official documentation lists exactly these filters.
type OptionChainsOptions struct {
	Symbol          string // required underlier
	ExpiryDay       string // DD
	ExpiryMonth     string // MM
	ExpiryYear      string // YYYY
	ChainType       string // CALL, PUT, CALLPUT
	StrikePriceNear string
	NoOfStrikes     int
	IncludeWeekly   *bool
	SkipAdjusted    *bool
	OptionCategory  string // STANDARD, ALL, MINI
	PriceType       string // ATNM, ALL
}

// GetOptionChains returns the option chain for an underlier.
// GET /v1/market/optionchains
func (c *Client) GetOptionChains(ctx context.Context, o OptionChainsOptions) (*Response, error) {
	q := url.Values{}
	q.Set("symbol", o.Symbol)
	setParam(q, "expiryDay", o.ExpiryDay)
	setParam(q, "expiryMonth", o.ExpiryMonth)
	setParam(q, "expiryYear", o.ExpiryYear)
	setParam(q, "chainType", o.ChainType)
	setParam(q, "strikePriceNear", o.StrikePriceNear)
	if o.NoOfStrikes > 0 {
		q.Set("noOfStrikes", fmt.Sprintf("%d", o.NoOfStrikes))
	}
	setParam(q, "includeWeekly", boolParam(o.IncludeWeekly))
	setParam(q, "skipAdjusted", boolParam(o.SkipAdjusted))
	setParam(q, "optionCategory", o.OptionCategory)
	setParam(q, "priceType", o.PriceType)
	return c.get(ctx, "/v1/market/optionchains", q)
}

// GetOptionExpireDates returns option expiration dates for a symbol.
// GET /v1/market/optionexpiredate
func (c *Client) GetOptionExpireDates(ctx context.Context,
	symbol, expiryType string) (*Response, error) {
	q := url.Values{}
	q.Set("symbol", symbol)
	setParam(q, "expiryType", expiryType)
	return c.get(ctx, "/v1/market/optionexpiredate", q)
}

////////////////////////////////////////////////////////////////////////////
// Orders — /v1/accounts/{accountIdKey}/orders

// OrdersOptions are the query parameters of List Orders. Dates are
// MMDDYYYY strings, the API's format.
type OrdersOptions struct {
	Marker          string
	Count           int    // max 100
	Status          string // OPEN, EXECUTED, CANCELLED, INDIVIDUAL_FILLS, CANCEL_REQUESTED, EXPIRED, REJECTED
	FromDate        string // MMDDYYYY
	ToDate          string // MMDDYYYY
	Symbols         []string
	SecurityType    string // EQ, OPTN, MF, MMF
	TransactionType string // ATNM, BUY, SELL, SELL_SHORT, BUY_TO_COVER, MF_EXCHANGE
	MarketSession   string // REGULAR, EXTENDED
}

// ListOrders returns orders for an account.
// GET /v1/accounts/{accountIdKey}/orders
func (c *Client) ListOrders(ctx context.Context, accountIDKey string,
	o OrdersOptions) (*Response, error) {
	q := url.Values{}
	setParam(q, "marker", o.Marker)
	if o.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", o.Count))
	}
	setParam(q, "status", o.Status)
	setParam(q, "fromDate", o.FromDate)
	setParam(q, "toDate", o.ToDate)
	if len(o.Symbols) > 0 {
		q.Set("symbol", strings.Join(o.Symbols, ","))
	}
	setParam(q, "securityType", o.SecurityType)
	setParam(q, "transactionType", o.TransactionType)
	setParam(q, "marketSession", o.MarketSession)
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders", q)
}

// GetOrderDetails returns one order.
// GET /v1/accounts/{accountIdKey}/orders/{orderId}
func (c *Client) GetOrderDetails(ctx context.Context,
	accountIDKey, orderID string) (*Response, error) {
	return c.get(ctx, "/v1/accounts/"+url.PathEscape(accountIDKey)+
		"/orders/"+url.PathEscape(orderID), nil)
}

// PreviewOrder previews an order. The body is the full
// PreviewOrderRequest JSON object (orderType EQ, OPTN, SPREADS, MF, … —
// every security type the API documents travels in this body, so the CLI
// supports all of them by construction).
// POST /v1/accounts/{accountIdKey}/orders/preview
func (c *Client) PreviewOrder(ctx context.Context, accountIDKey string,
	body any) (*Response, error) {
	return c.do(ctx, http.MethodPost,
		c.jsonPath("/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders/preview"),
		nil, body)
}

// PlaceOrder places a previously previewed order (the body is the full
// PlaceOrderRequest JSON object and must carry the previewId — the cmd
// layer enforces that). POST /v1/accounts/{accountIdKey}/orders/place
// Mutating: the CLI guards it with dry-run by default.
func (c *Client) PlaceOrder(ctx context.Context, accountIDKey string,
	body any) (*Response, error) {
	return c.do(ctx, http.MethodPost,
		c.jsonPath("/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders/place"),
		nil, body)
}

// PreviewChangedOrder previews a change to an existing order.
// PUT /v1/accounts/{accountIdKey}/orders/{orderId}/change/preview
// Mutating-adjacent (a preview only, but the CLI guards the change flow
// with dry-run by default at the place step).
func (c *Client) PreviewChangedOrder(ctx context.Context,
	accountIDKey, orderID string, body any) (*Response, error) {
	return c.do(ctx, http.MethodPut,
		"/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders/"+
			url.PathEscape(orderID)+"/change/preview", nil, body)
}

// PlaceChangedOrder places a previously preview-changed order.
// PUT /v1/accounts/{accountIdKey}/orders/{orderId}/change/place
// Mutating: the CLI guards it with dry-run by default.
func (c *Client) PlaceChangedOrder(ctx context.Context,
	accountIDKey, orderID string, body any) (*Response, error) {
	return c.do(ctx, http.MethodPut,
		"/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders/"+
			url.PathEscape(orderID)+"/change/place", nil, body)
}

// CancelOrder cancels an order. The body is the CancelOrderRequest JSON
// object ({"CancelOrderRequest":{"orderId":...}} built by the cmd layer).
// PUT /v1/accounts/{accountIdKey}/orders/cancel
// Mutating: the CLI guards it with dry-run by default.
func (c *Client) CancelOrder(ctx context.Context, accountIDKey string,
	body any) (*Response, error) {
	return c.do(ctx, http.MethodPut,
		"/v1/accounts/"+url.PathEscape(accountIDKey)+"/orders/cancel",
		nil, body)
}
