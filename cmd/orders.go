package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/client"
)

var ordersCmd = &cobra.Command{
	Use:   "orders",
	Short: "Orders (E*TRADE Order API v1)",
	Long: `Order commands. Request bodies for preview/place/change are the
full PreviewOrderRequest / PlaceOrderRequest JSON objects documented by
the Order API — every documented order type travels in that body
(EQ, OPTN including multi-leg SPREADS, MF, and the rest), so all of them
are supported by passing the JSON with --file, --json, or stdin.

SAFETY RAIL (same as schwab-cli): place, place-changed, and cancel are
dry-run by default — they print the exact request and send nothing
without --execute. E*TRADE requires orders to be previewed first: place
and place-changed require a previewId (from the matching preview
command), passed with --preview-id or already present in the body.`,
}

func init() {
	l := ordersListCmd
	l.Flags().StringVar(&olMarker, "marker", "", "paging marker")
	l.Flags().IntVar(&olCount, "count", 0, "orders to return (max 100)")
	l.Flags().StringVar(&olStatus, "status", "",
		"OPEN, EXECUTED, CANCELLED, INDIVIDUAL_FILLS, CANCEL_REQUESTED, EXPIRED, or REJECTED")
	l.Flags().StringVar(&olFrom, "from-date", "", "from date MMDDYYYY")
	l.Flags().StringVar(&olTo, "to-date", "", "to date MMDDYYYY")
	l.Flags().StringSliceVar(&olSymbols, "symbol", nil, "filter by symbol(s)")
	l.Flags().StringVar(&olSecurityType, "security-type", "", "EQ, OPTN, MF, or MMF")
	l.Flags().StringVar(&olTxnType, "transaction-type", "",
		"ATNM, BUY, SELL, SELL_SHORT, BUY_TO_COVER, or MF_EXCHANGE")
	l.Flags().StringVar(&olSession, "market-session", "", "REGULAR or EXTENDED")
	ordersCmd.AddCommand(l)

	ordersCmd.AddCommand(ordersDetailsCmd)

	pv := ordersPreviewCmd
	bodyFlags(pv, &pvFile, &pvJSON)
	ordersCmd.AddCommand(pv)

	pl := ordersPlaceCmd
	bodyFlags(pl, &plFile, &plJSON)
	pl.Flags().StringVar(&plPreviewID, "preview-id", "",
		"previewId from orders preview (required unless the body carries PreviewIds)")
	pl.Flags().BoolVar(&plExecute, "execute", false,
		"actually place the order (default is dry-run)")
	ordersCmd.AddCommand(pl)

	cp := ordersChangePreviewCmd
	bodyFlags(cp, &cpFile, &cpJSON)
	ordersCmd.AddCommand(cp)

	pc := ordersPlaceChangedCmd
	bodyFlags(pc, &pcFile, &pcJSON)
	pc.Flags().StringVar(&pcPreviewID, "preview-id", "",
		"previewId from orders change-preview (required unless the body carries PreviewIds)")
	pc.Flags().BoolVar(&pcExecute, "execute", false,
		"actually place the changed order (default is dry-run)")
	ordersCmd.AddCommand(pc)

	cx := ordersCancelCmd
	cx.Flags().BoolVar(&cxExecute, "execute", false,
		"actually cancel the order (default is dry-run)")
	ordersCmd.AddCommand(cx)
}

// bodyFlags registers the shared order-body input flags.
func bodyFlags(cmd *cobra.Command, file, inline *string) {
	cmd.Flags().StringVar(file, "file", "", "read the order request JSON from this file")
	cmd.Flags().StringVar(inline, "json", "", "order request JSON as an inline string")
	cmd.MarkFlagsMutuallyExclusive("file", "json")
}

// readJSONBody loads a request body (a full PreviewOrderRequest /
// PlaceOrderRequest object) from --file, --json, or stdin, in that order,
// mirroring schwab-cli's readOrderSpec. The returned value is the decoded
// JSON object, which the client re-marshals verbatim.
func readJSONBody(file, inline string) (map[string]any, error) {
	var data []byte
	var err error
	switch {
	case file != "":
		data, err = os.ReadFile(file)
		if err != nil {
			return nil, err
		}
	case inline != "":
		data = []byte(inline)
	default:
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("order request is not valid JSON: %w", err)
	}
	return body, nil
}

// ensurePreviewID enforces E*TRADE's preview-first rule for a place
// body: the wrapper object (wrapperKey, e.g. "PlaceOrderRequest") must
// carry PreviewIds, either already in the body or injected from the
// --preview-id flag. It returns an error naming the missing step when
// neither is present — before anything is sent or even dry-run printed
// as an executable-looking request.
func ensurePreviewID(body map[string]any, wrapperKey, previewID string) error {
	wrapper, ok := body[wrapperKey].(map[string]any)
	if !ok {
		return fmt.Errorf("order request must be a JSON object with a %q object", wrapperKey)
	}
	if _, present := wrapper["PreviewIds"]; present {
		return nil
	}
	if previewID == "" {
		return fmt.Errorf("no previewId: E*TRADE requires orders to be " +
			"previewed first — run the matching preview command and pass " +
			"--preview-id, or include PreviewIds in the request body")
	}
	// The API models previewId as a numeric id; keep the JSON number when
	// the flag parses as one, else pass the string through.
	var idVal any = previewID
	var num json.Number
	dec := json.NewDecoder(strings.NewReader(previewID))
	dec.UseNumber()
	if err := dec.Decode(&num); err == nil {
		if n, err := num.Int64(); err == nil {
			idVal = n
		}
	}
	wrapper["PreviewIds"] = map[string]any{"previewId": idVal}
	return nil
}

var (
	olMarker       string
	olCount        int
	olStatus       string
	olFrom         string
	olTo           string
	olSymbols      []string
	olSecurityType string
	olTxnType      string
	olSession      string
)

var ordersListCmd = &cobra.Command{
	Use:   "list <account-id-key>",
	Short: "List orders (GET /v1/accounts/{key}/orders)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		from, err := parseMMDDYYYY("from-date", olFrom)
		if err != nil {
			return err
		}
		to, err := parseMMDDYYYY("to-date", olTo)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.ListOrders(ctx, args[0], client.OrdersOptions{
				Marker: olMarker, Count: olCount, Status: olStatus,
				FromDate: from, ToDate: to, Symbols: olSymbols,
				SecurityType: olSecurityType, TransactionType: olTxnType,
				MarketSession: olSession,
			})
		})
	},
}

var ordersDetailsCmd = &cobra.Command{
	Use:   "details <account-id-key> <order-id>",
	Short: "Order details (GET /v1/accounts/{key}/orders/{order-id})",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetOrderDetails(ctx, args[0], args[1])
		})
	},
}

var pvFile, pvJSON string

var ordersPreviewCmd = &cobra.Command{
	Use:   "preview <account-id-key>",
	Short: "Preview an order (POST /v1/accounts/{key}/orders/preview)",
	Long: `Previews an order from a full PreviewOrderRequest JSON body
(--file, --json, or stdin). A preview changes nothing and returns the
previewId that orders place requires. Example body:

  {"PreviewOrderRequest": {"orderType": "EQ", "clientOrderId": "abc123",
    "Order": [{"Instrument": [{"Product": {"securityType": "EQ",
    "symbol": "AAPL"}, "orderAction": "BUY", "quantityType": "QUANTITY",
    "quantity": 1}], "orderTerm": "GOOD_FOR_DAY", "priceType": "MARKET",
    "marketSession": "REGULAR"}]}}`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readJSONBody(pvFile, pvJSON)
		if err != nil {
			return err
		}
		if _, ok := body["PreviewOrderRequest"]; !ok {
			return fmt.Errorf("order request must contain a \"PreviewOrderRequest\" object")
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.PreviewOrder(ctx, args[0], body)
		})
	},
}

var (
	plFile, plJSON string
	plPreviewID    string
	plExecute      bool
)

var ordersPlaceCmd = &cobra.Command{
	Use:   "place <account-id-key>",
	Short: "Place a previewed order — DRY-RUN unless --execute (POST /v1/accounts/{key}/orders/place)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readJSONBody(plFile, plJSON)
		if err != nil {
			return err
		}
		if err := ensurePreviewID(body, "PlaceOrderRequest", plPreviewID); err != nil {
			return err
		}
		path := "/v1/accounts/" + args[0] + "/orders/place"
		if handled, err := dryRunGuard(plExecute, "POST", path, body); handled {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.PlaceOrder(ctx, args[0], body)
		})
	},
}

var cpFile, cpJSON string

var ordersChangePreviewCmd = &cobra.Command{
	Use:   "change-preview <account-id-key> <order-id>",
	Short: "Preview a changed order (PUT /v1/accounts/{key}/orders/{order-id}/change/preview)",
	Long: `Previews a change to an existing order from a full
PreviewOrderRequest JSON body (--file, --json, or stdin). Returns the
previewId that orders place-changed requires.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readJSONBody(cpFile, cpJSON)
		if err != nil {
			return err
		}
		if _, ok := body["PreviewOrderRequest"]; !ok {
			return fmt.Errorf("order request must contain a \"PreviewOrderRequest\" object")
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.PreviewChangedOrder(ctx, args[0], args[1], body)
		})
	},
}

var (
	pcFile, pcJSON string
	pcPreviewID    string
	pcExecute      bool
)

var ordersPlaceChangedCmd = &cobra.Command{
	Use:   "place-changed <account-id-key> <order-id>",
	Short: "Place a previewed changed order — DRY-RUN unless --execute (PUT .../change/place)",
	Long: `Places a changed order. Note: E*TRADE implements a change by
cancelling the original order and creating a new one with a new orderId.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readJSONBody(pcFile, pcJSON)
		if err != nil {
			return err
		}
		if err := ensurePreviewID(body, "PlaceOrderRequest", pcPreviewID); err != nil {
			return err
		}
		path := "/v1/accounts/" + args[0] + "/orders/" + args[1] + "/change/place"
		if handled, err := dryRunGuard(pcExecute, "PUT", path, body); handled {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.PlaceChangedOrder(ctx, args[0], args[1], body)
		})
	},
}

var cxExecute bool

var ordersCancelCmd = &cobra.Command{
	Use:   "cancel <account-id-key> <order-id>",
	Short: "Cancel an order — DRY-RUN unless --execute (PUT /v1/accounts/{key}/orders/cancel)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var idVal any = args[1]
		var num json.Number
		dec := json.NewDecoder(strings.NewReader(args[1]))
		dec.UseNumber()
		if err := dec.Decode(&num); err == nil {
			if n, err := num.Int64(); err == nil {
				idVal = n
			}
		}
		body := map[string]any{
			"CancelOrderRequest": map[string]any{"orderId": idVal},
		}
		path := "/v1/accounts/" + args[0] + "/orders/cancel"
		if handled, err := dryRunGuard(cxExecute, "PUT", path, body); handled {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.CancelOrder(ctx, args[0], body)
		})
	},
}
