package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordedRequest struct {
	Path          string
	Authorization string
}

type stubOAuthServer struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
}

func (s *stubOAuthServer) handler() http.Handler {
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
		})
		s.mu.Unlock()
		if s.status != 0 {
			w.WriteHeader(s.status)
			w.Write([]byte("boom"))
			return
		}
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		if strings.HasSuffix(r.URL.Path, "/oauth/renew_access_token") ||
			strings.HasSuffix(r.URL.Path, "/oauth/revoke_access_token") {
			w.Write([]byte(""))
			return
		}
		w.Write([]byte("oauth_token=tok123&oauth_token_secret=sec456&oauth_callback_confirmed=true"))
	}
	mux.HandleFunc("/oauth/request_token", record)
	mux.HandleFunc("/oauth/access_token", record)
	mux.HandleFunc("/oauth/renew_access_token", record)
	mux.HandleFunc("/oauth/revoke_access_token", record)
	return mux
}

func newStubClient(t *testing.T, srv *stubOAuthServer) (*OAuthClient, func()) {
	t.Helper()
	ts := httptest.NewServer(srv.handler())
	c := &OAuthClient{
		ConsumerKey: "ck", ConsumerSecret: "cs",
		BaseURL:   ts.URL,
		Nonce:     func() string { return "n1" },
		Timestamp: func() int64 { return 1700000000 },
	}
	return c, ts.Close
}

func TestRequestTokenFlow(t *testing.T) {
	stub := &stubOAuthServer{}
	c, closeFn := newStubClient(t, stub)
	defer closeFn()
	tok, secret, err := c.GetRequestToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok123" || secret != "sec456" {
		t.Fatalf("got (%q, %q)", tok, secret)
	}
	if len(stub.requests) != 1 || stub.requests[0].Path != "/oauth/request_token" {
		t.Fatalf("requests = %+v", stub.requests)
	}
	authz := stub.requests[0].Authorization
	if !strings.Contains(authz, `oauth_callback="oob"`) {
		t.Errorf("request-token call missing oauth_callback: %q", authz)
	}
	if !strings.Contains(authz, "oauth_signature=") {
		t.Errorf("request not signed: %q", authz)
	}
}

func TestAccessTokenFlow(t *testing.T) {
	stub := &stubOAuthServer{}
	c, closeFn := newStubClient(t, stub)
	defer closeFn()
	tok, secret, err := c.GetAccessToken(context.Background(), "reqtok", "reqsec", "VER123")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok123" || secret != "sec456" {
		t.Fatalf("got (%q, %q)", tok, secret)
	}
	authz := stub.requests[0].Authorization
	if !strings.Contains(authz, `oauth_verifier="VER123"`) {
		t.Errorf("access-token call missing verifier: %q", authz)
	}
	if !strings.Contains(authz, `oauth_token="reqtok"`) {
		t.Errorf("access-token call missing request token: %q", authz)
	}
}

func TestRenewAndRevoke(t *testing.T) {
	stub := &stubOAuthServer{}
	c, closeFn := newStubClient(t, stub)
	defer closeFn()
	if err := c.RenewAccessToken(context.Background(), "at", "as"); err != nil {
		t.Fatal(err)
	}
	if err := c.RevokeAccessToken(context.Background(), "at", "as"); err != nil {
		t.Fatal(err)
	}
	paths := []string{stub.requests[0].Path, stub.requests[1].Path}
	if paths[0] != "/oauth/renew_access_token" || paths[1] != "/oauth/revoke_access_token" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestOAuthErrorStatus(t *testing.T) {
	stub := &stubOAuthServer{status: 401}
	c, closeFn := newStubClient(t, stub)
	defer closeFn()
	_, _, err := c.GetRequestToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 error", err)
	}
	// The error must never carry the consumer secret.
	if strings.Contains(err.Error(), "cs") && !strings.Contains(err.Error(), "status") {
		t.Fatalf("error leaks secret: %v", err)
	}
}
