package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type staticTokens struct{ tok, secret string }

func (s staticTokens) AccessToken() (string, string, error) { return s.tok, s.secret, nil }

type errTokens struct{ err error }

func (e errTokens) AccessToken() (string, string, error) { return "", "", e.err }

type recorded struct {
	Method        string
	Path          string
	Query         url.Values
	Authorization string
	Accept        string
	ContentType   string
	Body          []byte
}

type stub struct {
	mu       sync.Mutex
	requests []recorded
	status   int
	body     string
}

func (s *stub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
			Authorization: r.Header.Get("Authorization"),
			Accept:        r.Header.Get("Accept"),
			ContentType:   r.Header.Get("Content-Type"),
			Body:          body,
		})
		s.mu.Unlock()
		status := s.status
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
		b := s.body
		if b == "" {
			b = `{"ok":true}`
		}
		w.Write([]byte(b))
	})
}

func newStubClient(t *testing.T, s *stub) (*Client, func()) {
	t.Helper()
	ts := httptest.NewServer(s.handler())
	c := New("ck", "cs", staticTokens{"at", "as"})
	c.BaseURL = ts.URL
	c.Nonce = func() string { return "n1" }
	c.Timestamp = func() int64 { return 1700000000 }
	return c, ts.Close
}

func lastReq(t *testing.T, s *stub) recorded {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("stub received no requests")
	}
	return s.requests[len(s.requests)-1]
}

func TestListAccounts(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	resp, err := c.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.IsError() {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r := lastReq(t, s)
	if r.Method != "GET" || r.Path != "/v1/accounts/list.json" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
	if !strings.HasPrefix(r.Authorization, "OAuth ") ||
		!strings.Contains(r.Authorization, "oauth_signature=") ||
		!strings.Contains(r.Authorization, `oauth_token="at"`) {
		t.Fatalf("bad Authorization header: %q", r.Authorization)
	}
	if r.Accept != "application/json" {
		t.Fatalf("Accept = %q", r.Accept)
	}
}

func TestGetAccountBalance(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	rt := true
	_, err := c.GetAccountBalance(context.Background(), "KEY1", BalanceOptions{
		AccountType: "INDIVIDUAL", RealTimeNAV: &rt,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/balance.json" {
		t.Fatalf("path = %s", r.Path)
	}
	if r.Query.Get("instType") != "BROKERAGE" ||
		r.Query.Get("accountType") != "INDIVIDUAL" ||
		r.Query.Get("realTimeNAV") != "true" {
		t.Fatalf("query = %v", r.Query)
	}
}

func TestGetAccountPortfolio(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	totals := true
	_, err := c.GetAccountPortfolio(context.Background(), "KEY1", PortfolioOptions{
		Count: 25, SortBy: "symbol", SortOrder: "ASC", PageNumber: 2,
		MarketSession: "REGULAR", TotalsRequired: &totals, View: "COMPLETE",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/portfolio.json" {
		t.Fatalf("path = %s", r.Path)
	}
	want := map[string]string{
		"count": "25", "sortBy": "symbol", "sortOrder": "ASC",
		"pageNumber": "2", "marketSession": "REGULAR",
		"totalsRequired": "true", "view": "COMPLETE",
	}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, r.Query.Get(k), v)
		}
	}
	if _, present := r.Query["lotsRequired"]; present {
		t.Errorf("unset lotsRequired must be omitted, query = %v", r.Query)
	}
}

func TestGetPortfolioPositionLot(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.GetPortfolioPositionLot(context.Background(), "KEY1", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/portfolio/12345678.json" {
		t.Fatalf("path = %s", r.Path)
	}
}

func TestListTransactions(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.ListTransactions(context.Background(), "KEY1", TransactionsOptions{
		StartDate: "01012026", EndDate: "01312026", SortOrder: "ASC",
		Marker: "m9", Count: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/transactions.json" {
		t.Fatalf("path = %s", r.Path)
	}
	want := map[string]string{
		"startDate": "01012026", "endDate": "01312026",
		"sortOrder": "ASC", "marker": "m9", "count": "10",
	}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, r.Query.Get(k), v)
		}
	}
}

func TestGetTransactionDetails(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.GetTransactionDetails(context.Background(), "KEY1", "987", "store7")
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/transactions/987.json" ||
		r.Query.Get("storeId") != "store7" {
		t.Fatalf("got %s query=%v", r.Path, r.Query)
	}
}

func TestListAlerts(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.ListAlerts(context.Background(), AlertsOptions{
		Count: 50, Category: "STOCK", Status: "UNREAD",
		Direction: "ASC", Search: "AAPL",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/user/alerts.json" {
		t.Fatalf("path = %s", r.Path)
	}
	want := map[string]string{
		"count": "50", "category": "STOCK", "status": "UNREAD",
		"direction": "ASC", "search": "AAPL",
	}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, r.Query.Get(k), v)
		}
	}
}

func TestGetAlertDetails(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	html := true
	_, err := c.GetAlertDetails(context.Background(), "6772", &html)
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/user/alerts/6772.json" || r.Query.Get("htmlTags") != "true" {
		t.Fatalf("got %s query=%v", r.Path, r.Query)
	}
}

func TestDeleteAlerts(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.DeleteAlerts(context.Background(), []string{"6772", "6774"})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Method != "DELETE" || r.Path != "/v1/user/alerts/6772,6774" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
}

func TestLookupProduct(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.LookupProduct(context.Background(), "apple inc")
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/market/lookup/apple inc.json" { // server sees the decoded path
		t.Fatalf("path = %s", r.Path)
	}
}

func TestGetQuote(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	earn := true
	_, err := c.GetQuote(context.Background(), []string{"AAPL", "MSFT"}, QuoteOptions{
		DetailFlag: "ALL", RequireEarningsDate: &earn,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/market/quote/AAPL,MSFT.json" {
		t.Fatalf("path = %s", r.Path)
	}
	if r.Query.Get("detailFlag") != "ALL" || r.Query.Get("requireEarningsDate") != "true" {
		t.Fatalf("query = %v", r.Query)
	}
}

func TestGetOptionChains(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	skip := true
	_, err := c.GetOptionChains(context.Background(), OptionChainsOptions{
		Symbol: "GOOG", ExpiryDay: "15", ExpiryMonth: "01", ExpiryYear: "2027",
		ChainType: "CALL", StrikePriceNear: "200", NoOfStrikes: 10,
		SkipAdjusted: &skip, OptionCategory: "STANDARD", PriceType: "ATNM",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/market/optionchains.json" {
		t.Fatalf("path = %s", r.Path)
	}
	want := map[string]string{
		"symbol": "GOOG", "expiryDay": "15", "expiryMonth": "01",
		"expiryYear": "2027", "chainType": "CALL", "strikePriceNear": "200",
		"noOfStrikes": "10", "skipAdjusted": "true",
		"optionCategory": "STANDARD", "priceType": "ATNM",
	}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, r.Query.Get(k), v)
		}
	}
}

func TestGetOptionExpireDates(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.GetOptionExpireDates(context.Background(), "GOOG", "ALL")
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/market/optionexpiredate.json" ||
		r.Query.Get("symbol") != "GOOG" || r.Query.Get("expiryType") != "ALL" {
		t.Fatalf("got %s query=%v", r.Path, r.Query)
	}
}

func TestListOrders(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.ListOrders(context.Background(), "KEY1", OrdersOptions{
		Count: 100, Status: "OPEN", FromDate: "01012026", ToDate: "01312026",
		Symbols: []string{"AAPL", "MSFT"}, SecurityType: "EQ",
		TransactionType: "BUY", MarketSession: "REGULAR", Marker: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/orders.json" {
		t.Fatalf("path = %s", r.Path)
	}
	want := map[string]string{
		"count": "100", "status": "OPEN", "fromDate": "01012026",
		"toDate": "01312026", "symbol": "AAPL,MSFT", "securityType": "EQ",
		"transactionType": "BUY", "marketSession": "REGULAR", "marker": "m1",
	}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, r.Query.Get(k), v)
		}
	}
}

func TestGetOrderDetails(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	_, err := c.GetOrderDetails(context.Background(), "KEY1", "825")
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/KEY1/orders/825.json" {
		t.Fatalf("path = %s", r.Path)
	}
}

func TestPreviewOrder(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	body := map[string]any{"PreviewOrderRequest": map[string]any{"orderType": "EQ"}}
	_, err := c.PreviewOrder(context.Background(), "KEY1", body)
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Method != "POST" || r.Path != "/v1/accounts/KEY1/orders/preview.json" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
	if r.ContentType != "application/json" {
		t.Fatalf("Content-Type = %q", r.ContentType)
	}
	var sent map[string]any
	if err := json.Unmarshal(r.Body, &sent); err != nil {
		t.Fatal(err)
	}
	if _, ok := sent["PreviewOrderRequest"]; !ok {
		t.Fatalf("body not passed through: %s", r.Body)
	}
}

func TestPlaceOrder(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	body := map[string]any{"PlaceOrderRequest": map[string]any{"orderType": "OPTN"}}
	_, err := c.PlaceOrder(context.Background(), "KEY1", body)
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Method != "POST" || r.Path != "/v1/accounts/KEY1/orders/place.json" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
}

func TestChangeOrderFlow(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	body := map[string]any{"PreviewOrderRequest": map[string]any{"orderType": "EQ"}}
	if _, err := c.PreviewChangedOrder(context.Background(), "KEY1", "825", body); err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Method != "PUT" || r.Path != "/v1/accounts/KEY1/orders/825/change/preview" {
		t.Fatalf("preview-changed: got %s %s", r.Method, r.Path)
	}
	if _, err := c.PlaceChangedOrder(context.Background(), "KEY1", "825", body); err != nil {
		t.Fatal(err)
	}
	r = lastReq(t, s)
	if r.Method != "PUT" || r.Path != "/v1/accounts/KEY1/orders/825/change/place" {
		t.Fatalf("place-changed: got %s %s", r.Method, r.Path)
	}
}

func TestCancelOrder(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	body := map[string]any{"CancelOrderRequest": map[string]any{"orderId": 825}}
	_, err := c.CancelOrder(context.Background(), "KEY1", body)
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Method != "PUT" || r.Path != "/v1/accounts/KEY1/orders/cancel" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
	var sent map[string]any
	json.Unmarshal(r.Body, &sent)
	if _, ok := sent["CancelOrderRequest"]; !ok {
		t.Fatalf("body not passed through: %s", r.Body)
	}
}

func TestXMLFormatOmitsJSONSuffix(t *testing.T) {
	s := &stub{}
	c, done := newStubClient(t, s)
	defer done()
	c.Format = "xml"
	_, err := c.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := lastReq(t, s)
	if r.Path != "/v1/accounts/list" {
		t.Fatalf("path = %s, want no .json suffix in xml mode", r.Path)
	}
	if r.Accept != "application/xml" {
		t.Fatalf("Accept = %q", r.Accept)
	}
}

func TestErrorStatusPassthrough(t *testing.T) {
	s := &stub{status: 500, body: `{"Error":{"code":100}}`}
	c, done := newStubClient(t, s)
	defer done()
	resp, err := c.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsError() || resp.StatusCode != 500 {
		t.Fatalf("resp = %d", resp.StatusCode)
	}
}

func TestTokenErrorPropagates(t *testing.T) {
	s := &stub{}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	c := New("ck", "cs", errTokens{err: io.EOF})
	c.BaseURL = ts.URL
	_, err := c.ListAccounts(context.Background())
	if err == nil {
		t.Fatal("expected token error to propagate")
	}
	if len(s.requests) != 0 {
		t.Fatalf("no request must be sent when the token source fails, got %d", len(s.requests))
	}
}

func TestDefaultBaseURLs(t *testing.T) {
	prod := New("k", "s", staticTokens{})
	if got := prod.baseURL(); got != "https://api.etrade.com" {
		t.Fatalf("prod base = %q", got)
	}
	sb := New("k", "s", staticTokens{})
	sb.Sandbox = true
	if got := sb.baseURL(); got != "https://apisb.etrade.com" {
		t.Fatalf("sandbox base = %q", got)
	}
}
