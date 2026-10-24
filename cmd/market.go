package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/client"
)

var marketCmd = &cobra.Command{
	Use:   "market",
	Short: "Market data (E*TRADE Market API v1)",
}

func init() {
	marketCmd.AddCommand(marketLookupCmd)

	q := marketQuoteCmd
	q.Flags().StringVar(&mqDetailFlag, "detail-flag", "",
		"FUNDAMENTAL, INTRADAY, OPTIONS, WEEK_52, MF_DETAIL, or ALL")
	q.Flags().StringVar(&mqEarnings, "require-earnings-date", "",
		"include earnings date: true or false")
	q.Flags().StringVar(&mqSkipMini, "skip-mini-options-check", "",
		"skip the mini-options check: true or false")
	marketCmd.AddCommand(q)

	oc := marketOptionChainsCmd
	oc.Flags().StringVar(&ocExpiry, "expiry-date", "",
		"expiration date YYYY-MM-DD (sets expiry day/month/year)")
	oc.Flags().StringVar(&ocExpiryDay, "expiry-day", "", "expiry day DD")
	oc.Flags().StringVar(&ocExpiryMonth, "expiry-month", "", "expiry month MM")
	oc.Flags().StringVar(&ocExpiryYear, "expiry-year", "", "expiry year YYYY")
	oc.Flags().StringVar(&ocChainType, "chain-type", "", "CALL, PUT, or CALLPUT")
	oc.Flags().StringVar(&ocStrikeNear, "strike-price-near", "",
		"fetch strikes near this price")
	oc.Flags().IntVar(&ocNoStrikes, "no-of-strikes", 0, "number of strikes to fetch")
	oc.Flags().StringVar(&ocIncludeWeekly, "include-weekly", "",
		"include weekly options: true or false")
	oc.Flags().StringVar(&ocSkipAdjusted, "skip-adjusted", "",
		"skip adjusted options: true or false")
	oc.Flags().StringVar(&ocCategory, "option-category", "",
		"STANDARD, ALL, or MINI")
	oc.Flags().StringVar(&ocPriceType, "price-type", "", "ATNM or ALL")
	marketCmd.AddCommand(oc)

	ed := marketOptionExpireDatesCmd
	ed.Flags().StringVar(&edExpiryType, "expiry-type", "",
		"expiry type (e.g. ALL, WEEKLY, MONTHLY)")
	marketCmd.AddCommand(ed)
}

var marketLookupCmd = &cobra.Command{
	Use:   "lookup <search>",
	Short: "Look up product by company name (GET /v1/market/lookup/{search})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.LookupProduct(ctx, args[0])
		})
	},
}

var (
	mqDetailFlag string
	mqEarnings   string
	mqSkipMini   string
)

var marketQuoteCmd = &cobra.Command{
	Use:   "quote <symbol> [symbol...]",
	Short: "Get quotes, up to 25 symbols (GET /v1/market/quote/{symbols})",
	Long: `Option symbols use the API format
underlying:year:month:day:CALL|PUT:strike, e.g. GOOG:2027:1:15:CALL:200.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 25 {
			return fmt.Errorf("quote accepts at most 25 symbols, got %d", len(args))
		}
		earn, err := parseOptBool("require-earnings-date", mqEarnings)
		if err != nil {
			return err
		}
		skip, err := parseOptBool("skip-mini-options-check", mqSkipMini)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetQuote(ctx, args, client.QuoteOptions{
				DetailFlag:           strings.ToUpper(mqDetailFlag),
				RequireEarningsDate:  earn,
				SkipMiniOptionsCheck: skip,
			})
		})
	},
}

var (
	ocExpiry        string
	ocExpiryDay     string
	ocExpiryMonth   string
	ocExpiryYear    string
	ocChainType     string
	ocStrikeNear    string
	ocNoStrikes     int
	ocIncludeWeekly string
	ocSkipAdjusted  string
	ocCategory      string
	ocPriceType     string
)

var marketOptionChainsCmd = &cobra.Command{
	Use:   "option-chains <symbol>",
	Short: "Get option chains (GET /v1/market/optionchains)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		day, month, year := ocExpiryDay, ocExpiryMonth, ocExpiryYear
		if ocExpiry != "" {
			t, err := time.Parse("2006-01-02", ocExpiry)
			if err != nil {
				return fmt.Errorf("invalid --expiry-date %q: must be YYYY-MM-DD", ocExpiry)
			}
			day = t.Format("02")
			month = t.Format("01")
			year = t.Format("2006")
		}
		weekly, err := parseOptBool("include-weekly", ocIncludeWeekly)
		if err != nil {
			return err
		}
		skipAdj, err := parseOptBool("skip-adjusted", ocSkipAdjusted)
		if err != nil {
			return err
		}
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetOptionChains(ctx, client.OptionChainsOptions{
				Symbol: args[0], ExpiryDay: day, ExpiryMonth: month,
				ExpiryYear: year, ChainType: strings.ToUpper(ocChainType),
				StrikePriceNear: ocStrikeNear, NoOfStrikes: ocNoStrikes,
				IncludeWeekly: weekly, SkipAdjusted: skipAdj,
				OptionCategory: strings.ToUpper(ocCategory),
				PriceType:      strings.ToUpper(ocPriceType),
			})
		})
	},
}

var edExpiryType string

var marketOptionExpireDatesCmd = &cobra.Command{
	Use:   "option-expire-dates <symbol>",
	Short: "Get option expiration dates (GET /v1/market/optionexpiredate)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(func(ctx context.Context, c *client.Client) (*client.Response, error) {
			return c.GetOptionExpireDates(ctx, args[0], strings.ToUpper(edExpiryType))
		})
	},
}
