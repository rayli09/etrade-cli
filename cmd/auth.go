package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/etrade-cli/internal/auth"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage OAuth 1.0a access tokens",
	Long: `Commands for the E*TRADE OAuth 1.0a token lifecycle:

  auth login   — request token, browser authorize URL, verifier-code
                 exchange for an access token (the daily login).
  auth status  — token age, midnight-ET expiry, and inactivity state,
                 without ever printing token values.
  auth renew   — Renew Access Token: reactivate a token that went
                 inactive after 2 hours without API calls (same day only).
  auth revoke  — Revoke Access Token and delete the local token file.

E*TRADE access tokens expire at midnight US Eastern on the day they are
issued. There is no refresh token: after midnight, auth login is the
only way back in.`,
}

var flagVerifier string

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Create an access token via the OAuth 1.0a authorize flow (daily)",
	Long: `Walks through E*TRADE's OAuth 1.0a flow by copy-pasting a URL,
exactly like the official sample client:

  1. etrade-cli requests a request token and prints the authorize URL.
  2. Open it in a browser, log in to E*TRADE, and allow access.
  3. Copy the verifier code the page shows and paste it at the prompt
     (or pass it non-interactively with --verifier).

The access token is written to the token file (--token-path; separate
files per environment) with 0600 permissions. It expires at midnight US
Eastern tonight.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		oc, err := oauthClient()
		if err != nil {
			return err
		}
		ctx := context.Background()
		reqToken, reqSecret, err := oc.GetRequestToken(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Open this URL in your browser, log in, and allow access:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "    "+auth.AuthorizeURL(oc.ConsumerKey, reqToken))
		fmt.Fprintln(os.Stderr)

		verifier := flagVerifier
		if verifier == "" {
			fmt.Fprint(os.Stderr, "Verifier code> ")
			reader := bufio.NewReader(os.Stdin)
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				return err
			}
			verifier = strings.TrimSpace(line)
		}
		if verifier == "" {
			return fmt.Errorf("empty verifier code")
		}
		accessToken, accessSecret, err := oc.GetAccessToken(ctx, reqToken, reqSecret, verifier)
		if err != nil {
			return err
		}
		tf := auth.NewTokenFile(accessToken, accessSecret, flagSandbox, time.Now())
		if err := tf.Save(tokenPath()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Token written to %s. It expires at midnight "+
			"US Eastern (%s); re-run this command tomorrow.\n",
			tokenPath(),
			time.Unix(tf.ExpiresAt, 0).UTC().Format(time.RFC3339))
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show token age and expiry state (never token values)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tf, err := auth.LoadTokenFile(tokenPath())
		if err != nil {
			return err
		}
		now := time.Now()
		out := map[string]any{
			"token_path":           tokenPath(),
			"sandbox":              tf.Sandbox,
			"creation_timestamp":   tf.CreationTimestamp,
			"created_at":           time.Unix(tf.CreationTimestamp, 0).UTC().Format(time.RFC3339),
			"token_age_seconds":    int64(tf.Age(now).Seconds()),
			"expires_at":           time.Unix(tf.ExpiresAt, 0).UTC().Format(time.RFC3339),
			"expired":              tf.Expired(now),
			"inactive_after":       tf.InactivityDeadline().UTC().Format(time.RFC3339),
			"access_token_present": tf.Token.AccessToken != "",
		}
		enc, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(enc))
		return nil
	},
}

var authRenewCmd = &cobra.Command{
	Use:   "renew",
	Short: "Renew (reactivate) an inactive access token — same day only",
	Long: `Calls the Renew Access Token endpoint. If the token has been idle
for two hours it is inactive; renew reactivates it without a new login.
Renew does not extend the token past midnight US Eastern: an expired
token fails here and 'auth login' is required.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tf, err := auth.LoadTokenFile(tokenPath())
		if err != nil {
			return err
		}
		if tf.Expired(time.Now()) {
			return auth.ErrTokenExpired
		}
		oc, err := oauthClient()
		if err != nil {
			return err
		}
		if err := oc.RenewAccessToken(context.Background(),
			tf.Token.AccessToken, tf.Token.AccessTokenSecret); err != nil {
			return err
		}
		tf.LastRenewedAt = time.Now().Unix()
		if err := tf.Save(tokenPath()); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Access token renewed (reactivated). It still "+
			"expires at midnight US Eastern.")
		return nil
	},
}

var authRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke the access token and delete the local token file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tf, err := auth.LoadTokenFile(tokenPath())
		if err != nil {
			return err
		}
		oc, err := oauthClient()
		if err != nil {
			return err
		}
		if err := oc.RevokeAccessToken(context.Background(),
			tf.Token.AccessToken, tf.Token.AccessTokenSecret); err != nil {
			return err
		}
		if err := os.Remove(tokenPath()); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Access token revoked and token file removed.")
		return nil
	},
}

func init() {
	authLoginCmd.Flags().StringVar(&flagVerifier, "verifier", "",
		"the verifier code from the authorize page (skips the interactive prompt)")
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authRenewCmd)
	authCmd.AddCommand(authRevokeCmd)
}
