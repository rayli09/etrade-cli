package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// what was printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func runRoot(t *testing.T, out *string, args ...string) error {
	t.Helper()
	var err error
	*out = captureStdout(t, func() {
		rootCmd.SetArgs(args)
		err = rootCmd.Execute()
	})
	return err
}

const placeBody = `{"PlaceOrderRequest":{"orderType":"EQ","clientOrderId":"x1",` +
	`"PreviewIds":{"previewId":12345},"Order":[{"Instrument":[{"Product":` +
	`{"securityType":"EQ","symbol":"AAPL"},"orderAction":"BUY","quantity":1}]}]}}`

// TestPlaceOrderDryRunSendsNothing runs `orders place` without --execute.
// It must print the dry-run request and succeed WITHOUT credentials or a
// token file — proof nothing was sent (any send attempt would fail on
// credential/token resolution first).
func TestPlaceOrderDryRunSendsNothing(t *testing.T) {
	var out string
	err := runRoot(t, &out, "orders", "place", "KEY1", "--json", placeBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run": true`) {
		t.Fatalf("output missing dry_run marker:\n%s", out)
	}
	if !strings.Contains(out, "/v1/accounts/KEY1/orders/place") ||
		!strings.Contains(out, `"method": "POST"`) {
		t.Fatalf("dry-run output missing method/path:\n%s", out)
	}
	if !strings.Contains(out, "12345") {
		t.Fatalf("dry-run output missing the request body:\n%s", out)
	}
}

func TestPlaceOrderRequiresPreviewID(t *testing.T) {
	body := `{"PlaceOrderRequest":{"orderType":"EQ","Order":[]}}`
	var out string
	err := runRoot(t, &out, "orders", "place", "KEY1", "--json", body)
	if err == nil {
		t.Fatal("expected an error when no previewId is present")
	}
	if !strings.Contains(err.Error(), "preview") {
		t.Fatalf("error should explain the preview-first rule: %v", err)
	}
}

func TestPlaceOrderPreviewIDFlagInjects(t *testing.T) {
	body := `{"PlaceOrderRequest":{"orderType":"EQ","Order":[]}}`
	var out string
	err := runRoot(t, &out, "orders", "place", "KEY1", "--json", body,
		"--preview-id", "777")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run": true`) || !strings.Contains(out, "777") {
		t.Fatalf("preview-id not injected into dry-run body:\n%s", out)
	}
}

func TestCancelOrderDryRun(t *testing.T) {
	var out string
	err := runRoot(t, &out, "orders", "cancel", "KEY1", "825")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run": true`) ||
		!strings.Contains(out, "/v1/accounts/KEY1/orders/cancel") ||
		!strings.Contains(out, "CancelOrderRequest") ||
		!strings.Contains(out, "825") {
		t.Fatalf("cancel dry-run output wrong:\n%s", out)
	}
}

func TestPlaceChangedDryRun(t *testing.T) {
	var out string
	err := runRoot(t, &out, "orders", "place-changed", "KEY1", "825",
		"--json", placeBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run": true`) ||
		!strings.Contains(out, "/v1/accounts/KEY1/orders/825/change/place") ||
		!strings.Contains(out, `"method": "PUT"`) {
		t.Fatalf("place-changed dry-run output wrong:\n%s", out)
	}
}

func TestAlertsDeleteDryRun(t *testing.T) {
	var out string
	err := runRoot(t, &out, "alerts", "delete", "6772", "6774")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run": true`) ||
		!strings.Contains(out, "/v1/user/alerts/6772,6774") ||
		!strings.Contains(out, `"method": "DELETE"`) {
		t.Fatalf("alerts delete dry-run output wrong:\n%s", out)
	}
}

func TestEnsurePreviewID(t *testing.T) {
	// Already present: untouched.
	body := map[string]any{"PlaceOrderRequest": map[string]any{
		"PreviewIds": map[string]any{"previewId": 5}}}
	if err := ensurePreviewID(body, "PlaceOrderRequest", ""); err != nil {
		t.Fatal(err)
	}
	// Flag injection: numeric strings become JSON numbers.
	body2 := map[string]any{"PlaceOrderRequest": map[string]any{}}
	if err := ensurePreviewID(body2, "PlaceOrderRequest", "42"); err != nil {
		t.Fatal(err)
	}
	w := body2["PlaceOrderRequest"].(map[string]any)
	pid := w["PreviewIds"].(map[string]any)["previewId"]
	if pid != int64(42) {
		t.Fatalf("previewId = %#v, want int64(42)", pid)
	}
	// Missing wrapper object.
	if err := ensurePreviewID(map[string]any{}, "PlaceOrderRequest", "42"); err == nil {
		t.Fatal("expected error for missing wrapper")
	}
	// Neither body nor flag.
	body3 := map[string]any{"PlaceOrderRequest": map[string]any{}}
	if err := ensurePreviewID(body3, "PlaceOrderRequest", ""); err == nil {
		t.Fatal("expected error when no previewId anywhere")
	}
}

func TestDryRunGuard(t *testing.T) {
	handled, err := dryRunGuard(true, "POST", "/x", nil)
	if handled || err != nil {
		t.Fatalf("execute=true must not be handled: %v %v", handled, err)
	}
	var out string
	out = captureStdout(t, func() {
		handled, err = dryRunGuard(false, "POST", "/x", map[string]any{"a": 1})
	})
	if !handled || err != nil {
		t.Fatalf("execute=false must be handled: %v %v", handled, err)
	}
	if !strings.Contains(out, `"dry_run": true`) || !strings.Contains(out, "/x") {
		t.Fatalf("guard output wrong:\n%s", out)
	}
}

func TestParseOptBool(t *testing.T) {
	if v, err := parseOptBool("x", ""); v != nil || err != nil {
		t.Fatalf("empty: %v %v", v, err)
	}
	if v, err := parseOptBool("x", "true"); err != nil || v == nil || !*v {
		t.Fatalf("true: %v %v", v, err)
	}
	if v, err := parseOptBool("x", "FALSE"); err != nil || v == nil || *v {
		t.Fatalf("false: %v %v", v, err)
	}
	if _, err := parseOptBool("x", "yes"); err == nil {
		t.Fatal("expected error for yes")
	}
}

func TestParseMMDDYYYY(t *testing.T) {
	if v, err := parseMMDDYYYY("d", ""); v != "" || err != nil {
		t.Fatalf("empty: %q %v", v, err)
	}
	if v, err := parseMMDDYYYY("d", "01312026"); err != nil || v != "01312026" {
		t.Fatalf("valid: %q %v", v, err)
	}
	if _, err := parseMMDDYYYY("d", "2026-01-31"); err == nil {
		t.Fatal("expected error for ISO format (API wants MMDDYYYY)")
	}
	if _, err := parseMMDDYYYY("d", "13312026"); err == nil {
		t.Fatal("expected error for month 13")
	}
}

// TestCommandTreeParity counts the leaf commands per group: every
// documented v1 endpoint must have exactly one. (All groups are flat.)
func TestCommandTreeParity(t *testing.T) {
	want := map[string]int{
		"auth":     4, // login, status, renew, revoke
		"accounts": 6, // list, balance, portfolio, portfolio-lot, transactions, transaction-details
		"alerts":   3, // list, details, delete
		"market":   4, // lookup, quote, option-chains, option-expire-dates
		"orders":   7, // list, details, preview, place, change-preview, place-changed, cancel
	}
	found := map[string]int{}
	for _, g := range rootCmd.Commands() {
		name := strings.Fields(g.Use)[0]
		if _, ok := want[name]; !ok {
			continue // cobra's completion/help commands are not groups
		}
		found[name] = len(g.Commands())
	}
	for group, n := range want {
		if found[group] != n {
			t.Errorf("group %s has %d leaf commands, want %d", group, found[group], n)
		}
	}
	if len(found) != len(want) {
		t.Errorf("groups found = %v, want keys of %v", found, want)
	}
}
