package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNextMidnightET(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-10-23 15:00 EDT -> expiry 2026-10-24 00:00 EDT = 04:00 UTC.
	issued := time.Date(2026, 10, 23, 15, 0, 0, 0, loc)
	got := NextMidnightET(issued)
	want := time.Date(2026, 10, 24, 4, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextMidnightET = %v, want %v", got, want)
	}
	// Winter (EST): 2027-01-15 23:30 EST -> 2027-01-16 00:00 EST = 05:00 UTC.
	issued2 := time.Date(2027, 1, 15, 23, 30, 0, 0, loc)
	want2 := time.Date(2027, 1, 16, 5, 0, 0, 0, time.UTC)
	if got2 := NextMidnightET(issued2); !got2.Equal(want2) {
		t.Fatalf("NextMidnightET = %v, want %v", got2, want2)
	}
	// DST spring-forward day still lands on local midnight.
	issued3 := time.Date(2027, 3, 14, 12, 0, 0, 0, loc)
	got3 := NextMidnightET(issued3).In(loc)
	if got3.Day() != 15 || got3.Hour() != 0 || got3.Minute() != 0 {
		t.Fatalf("DST NextMidnightET = %v, want Mar 15 00:00 local", got3)
	}
}

func TestTokenFileSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tf := NewTokenFile("tok", "sec", true, now)
	if err := tf.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file perm = %o, want 600", perm)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
	loaded, err := LoadTokenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Token.AccessToken != "tok" || loaded.Token.AccessTokenSecret != "sec" {
		t.Fatalf("round-trip mismatch: %+v", loaded.Token)
	}
	if !loaded.Sandbox {
		t.Fatal("sandbox flag lost in round-trip")
	}
	if loaded.ExpiresAt != tf.ExpiresAt || loaded.CreationTimestamp != now.Unix() {
		t.Fatalf("timestamps lost: %+v", loaded)
	}
}

func TestTokenFileSaveTightensExistingPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	// Pre-create the temp path world-readable: Save must still land 0600.
	if err := os.WriteFile(path+".tmp", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tf := NewTokenFile("tok", "sec", false, time.Now())
	if err := tf.Save(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file perm = %o, want 600", perm)
	}
}

func TestLoadTokenFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadTokenFile(filepath.Join(dir, "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := LoadTokenFile(bad); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte(`{"token":{}}`), 0o600)
	if _, err := LoadTokenFile(empty); err == nil {
		t.Fatal("expected error for token file without a token pair")
	}
}

func TestTokenExpired(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	issued := time.Date(2026, 10, 23, 9, 0, 0, 0, loc)
	tf := NewTokenFile("tok", "sec", false, issued)
	if tf.Expired(time.Date(2026, 10, 23, 23, 59, 0, 0, loc)) {
		t.Fatal("token expired before midnight ET")
	}
	if !tf.Expired(time.Date(2026, 10, 24, 0, 0, 1, 0, loc)) {
		t.Fatal("token not expired after midnight ET")
	}
}

func TestInactivityDeadline(t *testing.T) {
	issued := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	tf := NewTokenFile("tok", "sec", false, issued)
	if d := tf.InactivityDeadline(); !d.Equal(issued.Add(2 * time.Hour)) {
		t.Fatalf("deadline = %v, want +2h", d)
	}
	tf.LastRenewedAt = issued.Add(90 * time.Minute).Unix()
	want := issued.Add(90*time.Minute + 2*time.Hour)
	if d := tf.InactivityDeadline(); !d.Equal(want) {
		t.Fatalf("deadline after renew = %v, want %v", d, want)
	}
}

func TestManagerAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	now := time.Now()
	tf := NewTokenFile("tok", "sec", false, now)
	if err := tf.Save(path); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Path: path, Now: func() time.Time { return now.Add(time.Hour) }}
	tok, secret, err := m.AccessToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok" || secret != "sec" {
		t.Fatalf("got (%q, %q)", tok, secret)
	}
}

func TestManagerAccessTokenExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	loc, _ := time.LoadLocation("America/New_York")
	issued := time.Date(2026, 10, 23, 9, 0, 0, 0, loc)
	tf := NewTokenFile("tok", "sec", false, issued)
	if err := tf.Save(path); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Path: path, Now: func() time.Time {
		return time.Date(2026, 10, 24, 8, 0, 0, 0, loc)
	}}
	_, _, err := m.AccessToken()
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
	if got := err.Error(); !strings.Contains(got, "midnight US Eastern") ||
		!strings.Contains(got, "auth login") {
		t.Fatalf("expiry error must name midnight ET and the login fix: %q", got)
	}
}

func TestManagerMissingFile(t *testing.T) {
	m := &Manager{Path: filepath.Join(t.TempDir(), "nope.json")}
	if _, _, err := m.AccessToken(); err == nil {
		t.Fatal("expected error for missing token file")
	}
}
