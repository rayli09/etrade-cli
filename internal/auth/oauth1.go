// Package auth implements E*TRADE's OAuth 1.0a (RFC 5849) authorization
// flow with hand-written HMAC-SHA1 signing on the standard library only,
// plus the on-disk access-token file.
//
// E*TRADE's flow, per the official Developer Platform documentation:
//
//  1. Get Request Token — GET {base}/oauth/request_token, signed with the
//     consumer key/secret and oauth_callback=oob.
//  2. Authorize — the user opens
//     https://us.etrade.com/e/t/etws/authorize?key=<consumer-key>&token=<request-token>
//     in a browser, logs in, and receives a verifier code.
//  3. Get Access Token — GET {base}/oauth/access_token, signed with the
//     request token and the verifier code.
//  4. Renew Access Token — GET {base}/oauth/renew_access_token reactivates
//     a token that went inactive after two hours without API calls. It
//     does not return a new token.
//  5. Revoke Access Token — GET {base}/oauth/revoke_access_token.
//
// Access tokens hard-expire at midnight US Eastern Time on the day they
// are issued, after which only a fresh interactive login (steps 1–3)
// produces a new one. There is no refresh token.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // HMAC-SHA1 is mandated by OAuth 1.0a / E*TRADE; not a password hash.
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// AuthorizeBaseURL is the browser authorization endpoint, identical for
// the sandbox and production environments.
const AuthorizeBaseURL = "https://us.etrade.com/e/t/etws/authorize"

// ProdBaseURL and SandboxBaseURL are the API hosts. OAuth endpoints live
// directly under the host (/oauth/...); REST v1 resources live under /v1.
const (
	ProdBaseURL    = "https://api.etrade.com"
	SandboxBaseURL = "https://apisb.etrade.com"
)

// Signer signs requests per OAuth 1.0a with HMAC-SHA1. Token and
// TokenSecret are empty before an access (or request) token exists.
// Nonce and Timestamp are injectable for deterministic tests.
type Signer struct {
	ConsumerKey    string
	ConsumerSecret string
	Token          string
	TokenSecret    string

	Nonce     func() string
	Timestamp func() int64
}

func (s *Signer) nonce() string {
	if s.Nonce != nil {
		return s.Nonce()
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (s *Signer) timestamp() int64 {
	if s.Timestamp != nil {
		return s.Timestamp()
	}
	return nowUnix()
}

// pctEncode percent-encodes s exactly as RFC 5849 §3.6 requires: only the
// unreserved characters A-Z a-z 0-9 - . _ ~ pass through; everything else
// becomes %XX with uppercase hex. (net/url's QueryEscape is not suitable:
// it encodes space as '+' and leaves some reserved characters alone.)
func pctEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// signatureBaseString builds the RFC 5849 §3.4.1 signature base string for
// a request: the HTTP method, the base-string URI (scheme, host, and path
// only — no query), and the normalized parameter string. params must
// contain every parameter that participates in the signature: the query
// parameters, any application/x-www-form-urlencoded body parameters, and
// the oauth_* protocol parameters except oauth_signature.
func signatureBaseString(method string, u *url.URL, params url.Values) string {
	pairs := make([][2]string, 0, len(params))
	for k, vs := range params {
		for _, v := range vs {
			pairs = append(pairs, [2]string{pctEncode(k), pctEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p[0] + "=" + p[1]
	}
	baseURL := u.Scheme + "://" + u.Host + u.EscapedPath()
	return strings.ToUpper(method) + "&" + pctEncode(baseURL) + "&" +
		pctEncode(strings.Join(parts, "&"))
}

// sign computes the HMAC-SHA1 signature for the base string. The signing
// key is pctEncode(consumerSecret) + "&" + pctEncode(tokenSecret), where
// the token secret is empty before a token exists.
func (s *Signer) sign(baseString string) string {
	key := pctEncode(s.ConsumerSecret) + "&" + pctEncode(s.TokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(baseString))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// oauthParams returns the oauth_* protocol parameters for a request,
// including any extras (oauth_callback, oauth_verifier).
func (s *Signer) oauthParams(extra map[string]string) url.Values {
	p := url.Values{}
	p.Set("oauth_consumer_key", s.ConsumerKey)
	p.Set("oauth_nonce", s.nonce())
	p.Set("oauth_signature_method", "HMAC-SHA1")
	p.Set("oauth_timestamp", fmt.Sprintf("%d", s.timestamp()))
	p.Set("oauth_version", "1.0")
	if s.Token != "" {
		p.Set("oauth_token", s.Token)
	}
	for k, v := range extra {
		p.Set(k, v)
	}
	return p
}

// AuthorizationHeader returns the value of the Authorization header for a
// request to rawURL with the given query parameters, form-body parameters
// (nil for non-form bodies — JSON bodies never participate in the
// signature), and extra oauth_* parameters (oauth_callback /
// oauth_verifier when applicable). The signature covers the query, the
// form body, and the oauth parameters, per RFC 5849 §3.4.1.3.
func (s *Signer) AuthorizationHeader(method, rawURL string,
	query, form url.Values, extraOAuth map[string]string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	params := url.Values{}
	for k, vs := range u.Query() {
		for _, v := range vs {
			params.Add(k, v)
		}
	}
	for k, vs := range query {
		for _, v := range vs {
			params.Add(k, v)
		}
	}
	for k, vs := range form {
		for _, v := range vs {
			params.Add(k, v)
		}
	}
	oauth := s.oauthParams(extraOAuth)
	for k, vs := range oauth {
		for _, v := range vs {
			params.Add(k, v)
		}
	}
	sig := s.sign(signatureBaseString(method, u, params))
	oauth.Set("oauth_signature", sig)

	keys := make([]string, 0, len(oauth))
	for k := range oauth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("OAuth ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%q", pctEncode(k), pctEncode(oauth.Get(k)))
	}
	return b.String(), nil
}

// AuthorizeURL builds the browser URL the user opens to authorize a
// request token and receive a verifier code.
func AuthorizeURL(consumerKey, requestToken string) string {
	q := url.Values{}
	q.Set("key", consumerKey)
	q.Set("token", requestToken)
	return AuthorizeBaseURL + "?" + q.Encode()
}

// ParseTokenResponse parses an OAuth token endpoint response body
// (application/x-www-form-urlencoded) into its oauth_token and
// oauth_token_secret values.
func ParseTokenResponse(body []byte) (token, tokenSecret string, err error) {
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", fmt.Errorf("parsing token response: %w", err)
	}
	token = vals.Get("oauth_token")
	tokenSecret = vals.Get("oauth_token_secret")
	if token == "" || tokenSecret == "" {
		return "", "", fmt.Errorf(
			"token response missing oauth_token/oauth_token_secret")
	}
	return token, tokenSecret, nil
}
