package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	_ "time/tzdata" // America/New_York must resolve even without system tzdata.
)

// Token is an E*TRADE OAuth 1.0a access-token pair. Unlike OAuth2 there is
// no refresh token and no expires_in: the pair dies at midnight US Eastern
// on the day it was issued (see TokenFile.Expired).
type Token struct {
	AccessToken       string `json:"oauth_token"`
	AccessTokenSecret string `json:"oauth_token_secret"`
}

// TokenFile is the on-disk token format for etrade-cli:
//
//	{
//	  "creation_timestamp": 1792814400,
//	  "expires_at": 1792902000,          // next midnight America/New_York
//	  "sandbox": false,
//	  "last_renewed_timestamp": 0,
//	  "token": {"oauth_token": "...", "oauth_token_secret": "..."}
//	}
//
// expires_at is recorded at login time because E*TRADE's rule is
// calendar-based (midnight ET), not duration-based.
type TokenFile struct {
	CreationTimestamp int64 `json:"creation_timestamp"`
	ExpiresAt         int64 `json:"expires_at"`
	Sandbox           bool  `json:"sandbox"`
	LastRenewedAt     int64 `json:"last_renewed_timestamp,omitempty"`
	Token             Token `json:"token"`
}

// NextMidnightET returns the first midnight in America/New_York strictly
// after t — the moment an access token issued at t expires.
func NextMidnightET(t time.Time) time.Time {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Unreachable with time/tzdata linked, but never guess an expiry.
		panic(err)
	}
	et := t.In(loc)
	return time.Date(et.Year(), et.Month(), et.Day()+1, 0, 0, 0, 0, loc)
}

// NewTokenFile builds a TokenFile for a freshly issued access token pair.
func NewTokenFile(accessToken, accessTokenSecret string, sandbox bool,
	now time.Time) *TokenFile {
	return &TokenFile{
		CreationTimestamp: now.Unix(),
		ExpiresAt:         NextMidnightET(now).Unix(),
		Sandbox:           sandbox,
		Token: Token{
			AccessToken:       accessToken,
			AccessTokenSecret: accessTokenSecret,
		},
	}
}

// LoadTokenFile reads and parses a token file.
func LoadTokenFile(path string) (*TokenFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf TokenFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing token file %s: %w", path, err)
	}
	if tf.Token.AccessToken == "" || tf.Token.AccessTokenSecret == "" {
		return nil, fmt.Errorf(
			"token file %s does not contain an access token pair", path)
	}
	return &tf, nil
}

// Save writes the token file atomically with owner-only permissions
// (0600), the same discipline as schwab-cli: write a temp file in place,
// then rename over the target.
func (tf *TokenFile) Save(path string) error {
	data, err := json.Marshal(tf)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// os.WriteFile honors the umask and ignores the mode when the temp
	// file already exists; chmod explicitly so the guarantee is real.
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Age returns how long ago the token was issued.
func (tf *TokenFile) Age(now time.Time) time.Duration {
	return now.Sub(time.Unix(tf.CreationTimestamp, 0))
}

// Expired reports whether the access token has passed its midnight-ET
// expiry. Once expired it cannot be renewed; a new login is required.
func (tf *TokenFile) Expired(now time.Time) bool {
	if tf.ExpiresAt == 0 {
		return true
	}
	return !now.Before(time.Unix(tf.ExpiresAt, 0))
}

// InactivityDeadline is when the token goes inactive if no API calls are
// made: two hours after the last renewal (or after issuance). An inactive
// token can be reactivated the same day with Renew; an expired one
// cannot.
func (tf *TokenFile) InactivityDeadline() time.Time {
	last := tf.CreationTimestamp
	if tf.LastRenewedAt > last {
		last = tf.LastRenewedAt
	}
	return time.Unix(last, 0).Add(2 * time.Hour)
}

// ErrTokenExpired is returned when a command needs an access token but
// the stored one passed its midnight-ET expiry.
var ErrTokenExpired = errors.New(
	"E*TRADE access token expired at midnight US Eastern; " +
		"re-run `etrade-cli auth login` to create a new one")

// Manager loads the token file for API calls, mirroring schwab-cli's
// auth.Manager role. E*TRADE tokens cannot be refreshed silently, so
// AccessToken either returns the stored pair or fails with
// ErrTokenExpired / the load error.
type Manager struct {
	Path string
	Now  func() time.Time // injectable for tests
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// AccessToken returns the stored access-token pair, or an error when the
// file is missing/unreadable or the token has expired.
func (m *Manager) AccessToken() (string, string, error) {
	tf, err := LoadTokenFile(m.Path)
	if err != nil {
		return "", "", err
	}
	if tf.Expired(m.now()) {
		return "", "", ErrTokenExpired
	}
	return tf.Token.AccessToken, tf.Token.AccessTokenSecret, nil
}
