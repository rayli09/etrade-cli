// Package cmd defines the etrade-cli command tree, mirroring schwab-cli's
// layout: an auth group for the OAuth 1.0a token lifecycle and one command
// group per API area (accounts, alerts, market, orders). Every command
// maps to exactly one E*TRADE Developer Platform REST v1 endpoint — see
// README.md for the parity checklist.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/auth"
	"github.com/rayli09/etrade-cli/internal/client"
)

var (
	flagConsumerKey    string
	flagConsumerSecret string
	flagConfigPath     string
	flagSandbox        bool
	flagTokenPath      string
	flagTimeout        time.Duration
	flagBaseURL        string
	flagFormat         string
)

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "etrade-cli",
	Short: "Command-line client for the E*TRADE Developer Platform REST API v1",
	Long: `etrade-cli is a Go command-line client for the E*TRADE Developer
Platform REST API v1, modeled on schwab-cli. Every documented v1 endpoint
is a subcommand: OAuth 1.0a authorization, accounts, alerts, market data,
and orders.

Credentials come from your E*TRADE API consumer key/secret: pass
--consumer-key/--consumer-secret, set ETRADE_CONSUMER_KEY /
ETRADE_CONSUMER_SECRET (or the ETRADE_SANDBOX_* variants with --sandbox),
or use a 0600 JSON config file (see --config). Then run
'etrade-cli auth login' once per day: E*TRADE access tokens expire at
midnight US Eastern and cannot be refreshed.

Order-mutating commands (orders place, orders place-changed,
orders cancel, alerts delete) are dry-run by default: they print the
request and send nothing. Pass --execute to actually send them. Orders
must be previewed first — place commands require a previewId.`,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if flagFormat != "json" && flagFormat != "xml" {
			return fmt.Errorf("invalid --format %q: must be json or xml", flagFormat)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagConsumerKey, "consumer-key", "",
		"consumer key (env: ETRADE_CONSUMER_KEY / ETRADE_SANDBOX_CONSUMER_KEY, or config file)")
	rootCmd.PersistentFlags().StringVar(&flagConsumerSecret, "consumer-secret", "",
		"consumer secret (env: ETRADE_CONSUMER_SECRET / ETRADE_SANDBOX_CONSUMER_SECRET, or config file)")
	rootCmd.PersistentFlags().StringVar(&flagConfigPath, "config", "",
		"path to a 0600 JSON config file with consumer_key/consumer_secret "+
			"(default ~/.etrade-cli.json when present)")
	rootCmd.PersistentFlags().BoolVar(&flagSandbox, "sandbox", false,
		"use the sandbox environment (https://apisb.etrade.com) and its token file/keys")
	rootCmd.PersistentFlags().StringVar(&flagTokenPath, "token-path", "",
		"path to the token file (env: ETRADE_TOKEN_PATH; default token.json, "+
			"or token.sandbox.json with --sandbox)")
	rootCmd.PersistentFlags().DurationVar(&flagTimeout, "timeout",
		client.DefaultTimeout, "HTTP timeout for API calls")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "base-url", "",
		"override the API base URL (env: ETRADE_BASE_URL; for testing)")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", "json",
		"response format to request: json or xml")

	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(accountsCmd)
	rootCmd.AddCommand(alertsCmd)
	rootCmd.AddCommand(marketCmd)
	rootCmd.AddCommand(ordersCmd)
}

// configFile mirrors the optional JSON config file.
type configFile struct {
	ConsumerKey           string `json:"consumer_key"`
	ConsumerSecret        string `json:"consumer_secret"`
	SandboxConsumerKey    string `json:"sandbox_consumer_key"`
	SandboxConsumerSecret string `json:"sandbox_consumer_secret"`
}

func loadConfigFile() (*configFile, error) {
	path := flagConfigPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return &configFile{}, nil
		}
		path = filepath.Join(home, ".etrade-cli.json")
		if _, err := os.Stat(path); err != nil {
			return &configFile{}, nil // no config file is fine
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: config file %s is readable by "+
			"others (mode %o); it should be 0600\n", path, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg configFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}
	return &cfg, nil
}

// resolveCredentials applies the credential precedence: explicit flags,
// then sandbox-specific env, generic env, sandbox config, generic config.
// It never prints or logs the values.
func resolveCredentials() (key, secret string, err error) {
	cfg, err := loadConfigFile()
	if err != nil {
		return "", "", err
	}
	key = flagConsumerKey
	secret = flagConsumerSecret
	if flagSandbox {
		if key == "" {
			key = os.Getenv("ETRADE_SANDBOX_CONSUMER_KEY")
		}
		if secret == "" {
			secret = os.Getenv("ETRADE_SANDBOX_CONSUMER_SECRET")
		}
	}
	if key == "" {
		key = os.Getenv("ETRADE_CONSUMER_KEY")
	}
	if secret == "" {
		secret = os.Getenv("ETRADE_CONSUMER_SECRET")
	}
	if flagSandbox {
		if key == "" {
			key = cfg.SandboxConsumerKey
		}
		if secret == "" {
			secret = cfg.SandboxConsumerSecret
		}
	}
	if key == "" {
		key = cfg.ConsumerKey
	}
	if secret == "" {
		secret = cfg.ConsumerSecret
	}
	return key, secret, nil
}

// requireCredentials fails early with a helpful message when the consumer
// key or secret is missing.
func requireCredentials() (string, string, error) {
	key, secret, err := resolveCredentials()
	if err != nil {
		return "", "", err
	}
	if key == "" || secret == "" {
		return "", "", fmt.Errorf("consumer key and secret are required: pass " +
			"--consumer-key/--consumer-secret, set ETRADE_CONSUMER_KEY and " +
			"ETRADE_CONSUMER_SECRET (ETRADE_SANDBOX_* with --sandbox), or " +
			"create a 0600 config file (see --config)")
	}
	return key, secret, nil
}

// tokenPath resolves the token file location for the selected environment.
func tokenPath() string {
	if flagTokenPath != "" {
		return flagTokenPath
	}
	if p := os.Getenv("ETRADE_TOKEN_PATH"); p != "" {
		return p
	}
	if flagSandbox {
		return "token.sandbox.json"
	}
	return "token.json"
}

// baseURL resolves the API host: explicit override, else sandbox/prod.
func baseURL() string {
	if flagBaseURL != "" {
		return strings.TrimSuffix(flagBaseURL, "/")
	}
	if b := os.Getenv("ETRADE_BASE_URL"); b != "" {
		return strings.TrimSuffix(b, "/")
	}
	if flagSandbox {
		return auth.SandboxBaseURL
	}
	return auth.ProdBaseURL
}

// oauthClient builds the OAuth client for the token endpoints.
func oauthClient() (*auth.OAuthClient, error) {
	key, secret, err := requireCredentials()
	if err != nil {
		return nil, err
	}
	return &auth.OAuthClient{
		ConsumerKey:    key,
		ConsumerSecret: secret,
		BaseURL:        baseURL(),
		Timeout:        flagTimeout,
	}, nil
}

// tokenManager builds the auth.Manager for the configured token file.
func tokenManager() *auth.Manager {
	return &auth.Manager{Path: tokenPath()}
}

// newRESTClient builds a signed REST client from the global flags.
func newRESTClient() (*client.Client, error) {
	key, secret, err := requireCredentials()
	if err != nil {
		return nil, err
	}
	c := client.New(key, secret, tokenManager())
	c.BaseURL = baseURL()
	c.Sandbox = flagSandbox
	c.Format = flagFormat
	c.Timeout = flagTimeout
	return c, nil
}

// printResponse renders an API response: the body pretty-printed when it
// is JSON, verbatim otherwise (XML). Error statuses are reported on
// stderr and returned as errors.
func printResponse(resp *client.Response) error {
	if len(resp.Body) > 0 {
		if json.Valid(resp.Body) {
			var buf json.RawMessage = resp.Body
			out, err := json.MarshalIndent(buf, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		} else {
			fmt.Println(string(resp.Body))
		}
	}
	if resp.IsError() {
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return nil
}

// runCall executes one REST call and prints its response.
func runCall(fn func(ctx context.Context, c *client.Client) (*client.Response, error)) error {
	c, err := newRESTClient()
	if err != nil {
		return err
	}
	resp, err := fn(context.Background(), c)
	if err != nil {
		return err
	}
	return printResponse(resp)
}

// boolPtrFlag registers an optional boolean flag: unset leaves the API
// default alone, set sends true/false. pflag has no tri-state bool, so
// the flag is a string validated at use.
func parseOptBool(name, v string) (*bool, error) {
	if v == "" {
		return nil, nil
	}
	switch strings.ToLower(v) {
	case "true":
		b := true
		return &b, nil
	case "false":
		b := false
		return &b, nil
	}
	return nil, fmt.Errorf("invalid --%s %q: must be true or false", name, v)
}

// parseMMDDYYYY validates a date flag in the API's MMDDYYYY format.
func parseMMDDYYYY(name, v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse("01022006", v); err != nil {
		return "", fmt.Errorf("invalid --%s %q: must be MMDDYYYY", name, v)
	}
	return v, nil
}
