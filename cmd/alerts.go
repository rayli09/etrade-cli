package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/client"
)

var alertsCmd = &cobra.Command{
	Use:   "alerts",
	Short: "User alerts (E*TRADE Alerts API v1)",
}

func init() {
	l := alertsListCmd
	l.Flags().IntVar(&alCount, "count", 0, "alerts to return (default 25, max 300)")
	l.Flags().StringVar(&alCategory, "category", "", "STOCK or ACCOUNT")
	l.Flags().StringVar(&alStatus, "status", "", "READ, UNREAD, or DELETED")
	l.Flags().StringVar(&alDirection, "direction", "", "ASC or DESC (by create date)")
	l.Flags().StringVar(&alSearch, "search", "", "search by alert subject")
	alertsCmd.AddCommand(l)

	d := alertsDetailsCmd
	d.Flags().StringVar(&adHTMLTags, "html-tags", "",
		"include HTML tags in the message text: true or false")
	alertsCmd.AddCommand(d)

	del := alertsDeleteCmd
	del.Flags().BoolVar(&delExecute, "execute", false,
		"actually delete (default is dry-run)")
	alertsCmd.AddCommand(del)
}

var (
	alCount     int
	alCategory  string
	alStatus    string
	alDirection string
	alSearch    string
)

var alertsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List alerts (GET /v1/user/alerts)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.ListAlerts(ctx, client.AlertsOptions{
				Count: alCount, Category: alCategory, Status: alStatus,
				Direction: alDirection, Search: alSearch,
			})
		})
	},
}

var adHTMLTags string

var alertsDetailsCmd = &cobra.Command{
	Use:   "details <alert-id>",
	Short: "Alert details (GET /v1/user/alerts/{id})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		html, err := parseOptBool("html-tags", adHTMLTags)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetAlertDetails(ctx, args[0], html)
		})
	},
}

var delExecute bool

var alertsDeleteCmd = &cobra.Command{
	Use:   "delete <alert-id> [alert-id...]",
	Short: "Delete alerts — DRY-RUN unless --execute (DELETE /v1/user/alerts/{id,...})",
	Long: `Deletes one or more alerts. SAFETY RAIL: without --execute this
command only prints the request it would send and sends nothing.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "/v1/user/alerts/" + joinComma(args)
		if handled, err := dryRunGuard(delExecute, "DELETE", path, nil); handled {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.DeleteAlerts(ctx, args)
		})
	},
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// dryRunGuard implements the order safety rail, mirroring schwab-cli:
// without --execute the command prints the exact request it would send
// and returns handled=true; nothing is transmitted, and no credentials
// or token are needed to produce the preview.
func dryRunGuard(execute bool, method, path string, body any) (bool, error) {
	if execute {
		return false, nil
	}
	out := map[string]any{
		"dry_run": true,
		"method":  method,
		"path":    path,
		"note":    "nothing was sent; pass --execute to send this request",
	}
	if body != nil {
		out["body"] = body
	}
	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return true, err
	}
	fmt.Println(string(enc))
	return true, nil
}
