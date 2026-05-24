package constructs

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// qualifiedName returns "<appName>-<stage>-<name>" so resources are namespaced
// per stage and never collide across environments.
func qualifiedName(ctx *forge.RunContext, name string) string {
	return fmt.Sprintf("%s-%s-%s", ctx.App.Name, ctx.Stage, name)
}

// bucketName returns a globally unique S3 bucket name, lowercased to satisfy
// S3 naming constraints. The account ID suffix prevents collisions across accounts.
func bucketName(ctx *forge.RunContext, name string) string {
	return strings.ToLower(fmt.Sprintf("%s-%s", qualifiedName(ctx, name), ctx.AccountID))
}

// defaultTags returns the standard set of resource tags used by all constructs,
// merged with any extra tags defined in the active StageConfig.
func defaultTags(ctx *forge.RunContext, name string) pulumi.StringMap {
	tags := pulumi.StringMap{
		"forge:app":   pulumi.String(ctx.App.Name),
		"forge:stage": pulumi.String(ctx.Stage),
		"forge:name":  pulumi.String(name),
	}
	for k, v := range ctx.ExtraTags() {
		tags[k] = pulumi.String(v)
	}
	return tags
}

// envKey converts a camelCase or kebab-case name to SCREAMING_SNAKE_CASE
// suitable for use as an environment variable suffix.
//
//	"MyTable"  → "MY_TABLE"
//	"todo-api" → "TODO_API"
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

// resolvePath returns an absolute path. If p is already absolute it is returned
// unchanged. If relative, it is resolved against ctx.WorkDir (the infra/
// directory at deploy time) — not the process CWD, which Pulumi changes to its
// own workspace temp directory before running inline programs.
func resolvePath(ctx *forge.RunContext, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(ctx.WorkDir, p)
}

// panicOnErr panics with a descriptive message if err is non-nil.
// Pulumi constructs call this pattern; errors propagate up through Pulumi's engine.
func panicOnErr(err error, context string) {
	if err != nil {
		panic(fmt.Sprintf("forge [%s]: %v", context, err))
	}
}

// validLogRetentionDays are the only values accepted by the CloudWatch Logs API.
var validLogRetentionDays = []int{1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653}

// resolveLogRetention converts a user-supplied LogRetentionDays value to the
// integer passed to cloudwatch.LogGroupArgs.RetentionInDays (0 = never expire).
// A value of 0 returns 14 (the forge default). A value of -1 returns 0 (CloudWatch
// "never expire"). Any other value must be in the CloudWatch allowed list.
func resolveLogRetention(days int) int {
	if days == 0 {
		return 14
	}
	if days == -1 {
		return 0 // CloudWatch: 0 = never expire
	}
	for _, v := range validLogRetentionDays {
		if days == v {
			return days
		}
	}
	panic(fmt.Sprintf(
		"forge: invalid LogRetentionDays %d — valid values: 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653, or -1 for never expire",
		days,
	))
}
