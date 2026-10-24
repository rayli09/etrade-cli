package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// nowUnix is a small indirection so token expiry code and signing share
// one clock source.
var nowUnix = func() int64 { return time.Now().Unix() }

// OAuthClient performs the five authorization operations against an
// E*TRADE base URL (ProdBaseURL or SandboxBaseURL, overridable for
// tests). It carries no token state: callers pass token material in and
// persist what comes back.
type OAuthClient struct {
	ConsumerKey    string
	ConsumerSecret string
	BaseURL        string // defaults to ProdBaseURL
	HTTP           *http.Client
	Timeout        time.Duration // defaults to 30s

	Nonce     func() string
	Timestamp func() int64
}

func (c *OAuthClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return ProdBaseURL
}

func (c *OAuthClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func (c *OAuthClient) signer(token, tokenSecret string) *Signer {
	return &Signer{
		ConsumerKey:    c.ConsumerKey,
		ConsumerSecret: c.ConsumerSecret,
		Token:          token,
		TokenSecret:    tokenSecret,
		Nonce:          c.Nonce,
		Timestamp:      c.Timestamp,
	}
}

// get performs one signed OAuth GET and returns the response body. The
// body is returned (not printed) even for error statuses, without any
// credential material in the error text.
func (c *OAuthClient) get(ctx context.Context, path string,
	token, tokenSecret string, extraOAuth map[string]string) ([]byte, error) {
	u := c.baseURL() + path
	header, err := c.signer(token, tokenSecret).
		AuthorizationHeader(http.MethodGet, u, nil, nil, extraOAuth)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", header)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth request to %s failed with status %d: %s",
			path, resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	const max = 300
	s := string(b)
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// GetRequestToken performs step 1 of the flow (oauth_callback=oob) and
// returns the request token pair. The user then opens AuthorizeURL and
// receives a verifier code.
func (c *OAuthClient) GetRequestToken(ctx context.Context) (string, string, error) {
	body, err := c.get(ctx, "/oauth/request_token", "", "",
		map[string]string{"oauth_callback": "oob"})
	if err != nil {
		return "", "", err
	}
	return ParseTokenResponse(body)
}

// GetAccessToken performs step 3: exchanges a request token plus the
// verifier code from the authorize page for an access token pair.
func (c *OAuthClient) GetAccessToken(ctx context.Context,
	requestToken, requestTokenSecret, verifier string) (string, string, error) {
	body, err := c.get(ctx, "/oauth/access_token", requestToken,
		requestTokenSecret, map[string]string{"oauth_verifier": verifier})
	if err != nil {
		return "", "", err
	}
	return ParseTokenResponse(body)
}

// RenewAccessToken reactivates an access token that went inactive after
// two hours without API calls. E*TRADE returns no new token; success is
// the HTTP 200 itself. An expired token (past midnight ET) cannot be
// renewed and E*TRADE rejects the call.
func (c *OAuthClient) RenewAccessToken(ctx context.Context,
	accessToken, accessTokenSecret string) error {
	_, err := c.get(ctx, "/oauth/renew_access_token",
		accessToken, accessTokenSecret, nil)
	return err
}

// RevokeAccessToken revokes an access token.
func (c *OAuthClient) RevokeAccessToken(ctx context.Context,
	accessToken, accessTokenSecret string) error {
	_, err := c.get(ctx, "/oauth/revoke_access_token",
		accessToken, accessTokenSecret, nil)
	return err
}
