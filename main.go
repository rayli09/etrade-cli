// Command etrade-cli is a Go command-line client for the E*TRADE Developer
// Platform REST API v1, modeled on schwab-cli (the Go port of schwab-py).
//
// See README.md in this repository for the endpoint-to-command parity
// checklist, the daily-login reality of E*TRADE's OAuth 1.0a tokens, and
// the dry-run order safety rails.
package main

import "github.com/rayli09/etrade-cli/cmd"

func main() {
	cmd.Execute()
}
