package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/client"
)

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Account information (E*TRADE Accounts API v1)",
}

func init() {
	accountsCmd.AddCommand(accountsListCmd)

	bal := accountsBalanceCmd
	bal.Flags().StringVar(&balInstType, "inst-type", "BROKERAGE",
		"institution type")
	bal.Flags().StringVar(&balAccountType, "account-type", "",
		"registered account type (optional)")
	bal.Flags().StringVar(&balRealTime, "real-time-nav", "",
		"real-time net asset value: true or false (optional)")
	accountsCmd.AddCommand(bal)

	pf := accountsPortfolioCmd
	pf.Flags().IntVar(&pfCount, "count", 0, "positions per page (API default 50)")
	pf.Flags().StringVar(&pfSortBy, "sort-by", "", "column to sort by")
	pf.Flags().StringVar(&pfSortOrder, "sort-order", "", "ASC or DESC")
	pf.Flags().IntVar(&pfPageNumber, "page-number", 0, "page number")
	pf.Flags().StringVar(&pfMarketSession, "market-session", "", "REGULAR or EXTENDED")
	pf.Flags().StringVar(&pfTotals, "totals-required", "", "include totals: true or false")
	pf.Flags().StringVar(&pfLots, "lots-required", "", "include lots: true or false")
	pf.Flags().StringVar(&pfView, "view", "",
		"PERFORMANCE, FUNDAMENTAL, OPTIONSWATCH, QUICK, or COMPLETE")
	accountsCmd.AddCommand(pf)

	accountsCmd.AddCommand(accountsPortfolioLotCmd)

	tx := accountsTransactionsCmd
	tx.Flags().StringVar(&txStart, "start-date", "", "start date MMDDYYYY")
	tx.Flags().StringVar(&txEnd, "end-date", "", "end date MMDDYYYY")
	tx.Flags().StringVar(&txSortOrder, "sort-order", "", "ASC or DESC")
	tx.Flags().StringVar(&txMarker, "marker", "", "paging marker")
	tx.Flags().IntVar(&txCount, "count", 0, "transactions to return")
	accountsCmd.AddCommand(tx)

	txd := accountsTransactionDetailsCmd
	txd.Flags().StringVar(&txdStoreID, "store-id", "",
		"storage location for older transactions (optional)")
	accountsCmd.AddCommand(txd)
}

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List accounts (GET /v1/accounts/list)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.ListAccounts(ctx)
		})
	},
}

var (
	balInstType    string
	balAccountType string
	balRealTime    string
)

var accountsBalanceCmd = &cobra.Command{
	Use:   "balance <account-id-key>",
	Short: "Account balance (GET /v1/accounts/{key}/balance)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := parseOptBool("real-time-nav", balRealTime)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetAccountBalance(ctx, args[0], client.BalanceOptions{
				InstType: balInstType, AccountType: balAccountType, RealTimeNAV: rt,
			})
		})
	},
}

var (
	pfCount         int
	pfSortBy        string
	pfSortOrder     string
	pfPageNumber    int
	pfMarketSession string
	pfTotals        string
	pfLots          string
	pfView          string
)

var accountsPortfolioCmd = &cobra.Command{
	Use:   "portfolio <account-id-key>",
	Short: "Account portfolio (GET /v1/accounts/{key}/portfolio)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		totals, err := parseOptBool("totals-required", pfTotals)
		if err != nil {
			return err
		}
		lots, err := parseOptBool("lots-required", pfLots)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetAccountPortfolio(ctx, args[0], client.PortfolioOptions{
				Count: pfCount, SortBy: pfSortBy, SortOrder: pfSortOrder,
				PageNumber: pfPageNumber, MarketSession: pfMarketSession,
				TotalsRequired: totals, LotsRequired: lots, View: pfView,
			})
		})
	},
}

var accountsPortfolioLotCmd = &cobra.Command{
	Use:   "portfolio-lot <account-id-key> <position-id>",
	Short: "Portfolio position lot details (GET /v1/accounts/{key}/portfolio/{position-id})",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetPortfolioPositionLot(ctx, args[0], args[1])
		})
	},
}

var (
	txStart     string
	txEnd       string
	txSortOrder string
	txMarker    string
	txCount     int
)

var accountsTransactionsCmd = &cobra.Command{
	Use:   "transactions <account-id-key>",
	Short: "List transactions (GET /v1/accounts/{key}/transactions)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		start, err := parseMMDDYYYY("start-date", txStart)
		if err != nil {
			return err
		}
		end, err := parseMMDDYYYY("end-date", txEnd)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.ListTransactions(ctx, args[0], client.TransactionsOptions{
				StartDate: start, EndDate: end, SortOrder: txSortOrder,
				Marker: txMarker, Count: txCount,
			})
		})
	},
}

var txdStoreID string

var accountsTransactionDetailsCmd = &cobra.Command{
	Use:   "transaction-details <account-id-key> <transaction-id>",
	Short: "Transaction details (GET /v1/accounts/{key}/transactions/{id})",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetTransactionDetails(ctx, args[0], args[1], txdStoreID)
		})
	},
}
