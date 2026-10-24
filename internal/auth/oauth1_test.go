package auth

import (
	"net/url"
	"strings"
	"testing"
)

// TestSignatureRFC5849Vector verifies HMAC-SHA1 signing against the
// canonical OAuth 1.0a example from RFC 5849 §3.4 (the photos.example.net
// example also used by the OAuth Core 1.0 spec, Appendix A).
func TestSignatureRFC5849Vector(t *testing.T) {
	s := &Signer{
		ConsumerKey:    "dpf43f3p2l4k3l03",
		ConsumerSecret: "kd94hf93k423kf44",
		Token:          "nnch734d00sl2jdk",
		TokenSecret:    "pfkkdhi9sl3r4s00",
		Nonce:          func() string { return "kllo9940pd9333jh" },
		Timestamp:      func() int64 { return 1191242096 },
	}
	header, err := s.AuthorizationHeader("GET",
		"http://photos.example.net/photos?file=vacation.jpg&size=original",
		nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The expected signature from the spec is
	// tR3+Ty81lMeYAr/Fid0kMTYa/WM= — percent-encoded in the header.
	want := `oauth_signature="tR3%2BTy81lMeYAr%2FFid0kMTYa%2FWM%3D"`
	if !strings.Contains(header, want) {
		t.Fatalf("header %q does not contain expected signature %q", header, want)
	}
}

func TestSignatureBaseString(t *testing.T) {
	u, err := url.Parse("https://api.etrade.com/v1/market/quote/AAPL")
	if err != nil {
		t.Fatal(err)
	}
	params := url.Values{}
	params.Set("b", "2")
	params.Set("a", "z")
	params.Add("a", "a") // duplicate key: sorted by encoded value
	params.Set("oauth_consumer_key", "key")
	got := signatureBaseString("get", u, params)
	wantPrefix := "GET&https%3A%2F%2Fapi.etrade.com%2Fv1%2Fmarket%2Fquote%2FAAPL&"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("base string %q missing prefix %q", got, wantPrefix)
	}
	// Normalized params: a=a&a=z&b=2&oauth_consumer_key=key, encoded as a
	// whole into the third segment.
	third := got[len(wantPrefix):]
	wantThird := "a%3Da%26a%3Dz%26b%3D2%26oauth_consumer_key%3Dkey"
	if third != wantThird {
		t.Fatalf("normalized params = %q, want %q", third, wantThird)
	}
}

func TestPctEncode(t *testing.T) {
	cases := map[string]string{
		"abcXYZ019-._~": "abcXYZ019-._~", // unreserved pass through
		"a b":           "a%20b",         // space is %20, never '+'
		"a+b":           "a%2Bb",
		"/path?x=1&y=2": "%2Fpath%3Fx%3D1%26y%3D2",
		"é":             "%C3%A9", // UTF-8 bytes, uppercase hex
		"*":             "%2A",
		"":              "",
	}
	for in, want := range cases {
		if got := pctEncode(in); got != want {
			t.Errorf("pctEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuthorizationHeaderParams(t *testing.T) {
	s := &Signer{
		ConsumerKey: "ck", ConsumerSecret: "cs",
		Nonce:     func() string { return "abc123" },
		Timestamp: func() int64 { return 1700000000 },
	}
	header, err := s.AuthorizationHeader("GET",
		"https://apisb.etrade.com/oauth/request_token", nil, nil,
		map[string]string{"oauth_callback": "oob"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(header, "OAuth ") {
		t.Fatalf("header %q does not start with OAuth", header)
	}
	for _, want := range []string{
		`oauth_callback="oob"`,
		`oauth_consumer_key="ck"`,
		`oauth_nonce="abc123"`,
		`oauth_signature_method="HMAC-SHA1"`,
		`oauth_timestamp="1700000000"`,
		`oauth_version="1.0"`,
		"oauth_signature=",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q missing %q", header, want)
		}
	}
	if strings.Contains(header, "oauth_token") {
		t.Errorf("header %q must not contain oauth_token before a token exists", header)
	}
	if strings.Contains(header, "cs") && !strings.Contains(header, "oauth_consumer_key") {
		t.Errorf("header leaks secret")
	}
}

func TestAuthorizeURL(t *testing.T) {
	got := AuthorizeURL("mykey", "tok en")
	want := "https://us.etrade.com/e/t/etws/authorize?key=mykey&token=tok+en"
	if got != want {
		t.Fatalf("AuthorizeURL = %q, want %q", got, want)
	}
}

func TestParseTokenResponse(t *testing.T) {
	tok, secret, err := ParseTokenResponse(
		[]byte("oauth_token=abc&oauth_token_secret=def&oauth_callback_confirmed=true"))
	if err != nil {
		t.Fatal(err)
	}
	if tok != "abc" || secret != "def" {
		t.Fatalf("got (%q, %q)", tok, secret)
	}
}

func TestParseTokenResponseMissing(t *testing.T) {
	if _, _, err := ParseTokenResponse([]byte("oauth_token=abc")); err == nil {
		t.Fatal("expected error for missing oauth_token_secret")
	}
	if _, _, err := ParseTokenResponse([]byte("")); err == nil {
		t.Fatal("expected error for empty body")
	}
}
