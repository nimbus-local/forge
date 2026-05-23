// Package cloudflare provides Pulumi constructs for Cloudflare resources (Workers, KV, D1, R2).
// Import it in your infra/sst.config.go alongside the forge constructs package.
package cloudflare

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	forge "github.com/nimbus-local/forge"
)

// qualifiedName returns "<appName>-<stage>-<name>" so resources are namespaced per stage.
func qualifiedName(ctx *forge.RunContext, name string) string {
	return fmt.Sprintf("%s-%s-%s", ctx.App.Name, ctx.Stage, name)
}

// envKey converts a camelCase or kebab-case name to SCREAMING_SNAKE_CASE.
func envKey(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	s := b.String()
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

// panicOnErr panics with a descriptive message if err is non-nil.
func panicOnErr(err error, context string) {
	if err != nil {
		panic(fmt.Sprintf("forge [%s]: %v", context, err))
	}
}

// accountID resolves the Cloudflare account ID from AppConfig or environment.
func accountID(ctx *forge.RunContext) string {
	if ctx.App.Cloudflare != nil && ctx.App.Cloudflare.AccountID != "" {
		return ctx.App.Cloudflare.AccountID
	}
	if v := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); v != "" {
		return v
	}
	panic("forge: Cloudflare AccountID must be set in AppConfig.Cloudflare.AccountID or CLOUDFLARE_ACCOUNT_ID env var")
}

// zoneID resolves the Cloudflare zone ID from AppConfig or environment.
// Returns empty string if not configured (zone ID is optional for most resources).
func zoneID(ctx *forge.RunContext) string {
	if ctx.App.Cloudflare != nil && ctx.App.Cloudflare.ZoneID != "" {
		return ctx.App.Cloudflare.ZoneID
	}
	return os.Getenv("CLOUDFLARE_ZONE_ID")
}
