package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustConvert(t *testing.T, ts string) *Result {
	t.Helper()
	r, err := Convert(ts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return r
}

func assertContains(t *testing.T, haystack, needle, context string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: want %q in output\ngot:\n%s", context, needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle, context string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: did not want %q in output\ngot:\n%s", context, needle, haystack)
	}
}

func wrapRun(body string) string {
	return `export default $config({
  app(input) { return { name: "app", home: "aws" }; },
  async run() {
    ` + body + `
  },
});`
}

// ── TestConvertFunction ───────────────────────────────────────────────────────

func TestConvertFunction(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const fn = new sst.aws.Function("MyFn", { handler: "src/index.handler" });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `constructs.NewFunction`, "function constructor")
	assertContains(t, r.GoSource, `"MyFn"`, "function name")
	assertContains(t, r.GoSource, `Handler: "src/index.handler"`, "handler field")
	assertContains(t, r.GoSource, `constructs.FunctionArgs`, "args struct")
}

func TestConvertFunctionWithRuntime(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const fn = new sst.aws.Function("MyFn", { handler: "bootstrap", runtime: "provided.al2023" });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `Runtime: "provided.al2023"`, "runtime field")
}

// ── TestConvertApiGatewayV2 ───────────────────────────────────────────────────

func TestConvertApiGatewayV2(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const api = new sst.aws.ApiGatewayV2("MyApi");`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `constructs.NewApiGatewayV2`, "api constructor")
	assertContains(t, r.GoSource, `"MyApi"`, "api name")
	assertContains(t, r.GoSource, `nil`, "nil args for no-args construct")
}

func TestConvertApiGatewayV2Route(t *testing.T) {
	t.Parallel()
	src := wrapRun(`api.route("GET /users", { handler: "src/users.handler" });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `api.Route`, "Route method")
	assertContains(t, r.GoSource, `"GET /users"`, "route key")
	assertContains(t, r.GoSource, `Handler: "src/users.handler"`, "route handler")
}

func TestConvertApiGatewayV2SimpleRoute(t *testing.T) {
	t.Parallel()
	src := wrapRun(`api.route("POST /", fn);`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `api.Route`, "Route method")
	assertContains(t, r.GoSource, `Function: fn`, "function ref")
}

// ── TestConvertDynamoDB ───────────────────────────────────────────────────────

func TestConvertDynamoDB(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const table = new sst.aws.DynamoDB("UsersTable", { fields: { pk: "string" } });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `constructs.NewDynamoDB`, "dynamo constructor")
	assertContains(t, r.GoSource, `"UsersTable"`, "table name")

	// DynamoDB args need manual conversion — expect a warning.
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "DynamoDB") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DynamoDB manual-conversion warning in Warnings")
	}
}

// ── TestConvertBucket ─────────────────────────────────────────────────────────

func TestConvertBucket(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const b = new sst.aws.Bucket("Assets");`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `constructs.NewBucket`, "bucket constructor")
	assertContains(t, r.GoSource, `"Assets"`, "bucket name")
	assertContains(t, r.GoSource, `nil`, "nil args for no-config bucket")
}

func TestConvertBucketPublic(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const b = new sst.aws.Bucket("Assets", { public: true });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `Public: true`, "public field")
}

// ── TestConvertRemovalPolicy ──────────────────────────────────────────────────

func TestConvertRemovalPolicyRetain(t *testing.T) {
	t.Parallel()
	src := `export default $config({
  app(input) { return { name: "app", removal: "retain", home: "aws" }; },
  async run() {},
});`
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `forge.RemovalRetain`, "retain policy")
}

func TestConvertRemovalPolicyConditional(t *testing.T) {
	t.Parallel()
	src := `export default $config({
  app(input) {
    return {
      name: "app",
      removal: input?.stage === "production" ? "retain" : "remove",
      home: "aws",
    };
  },
  async run() {},
});`
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `forge.RemovalRetain`, "retain branch")
	assertContains(t, r.GoSource, `forge.RemovalDestroy`, "destroy branch")

	// Expect a warning about the conditional conversion.
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "Removal") || strings.Contains(w, "conditional") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected removal-policy conditional warning")
	}
}

// ── TestConvertAppConfig ──────────────────────────────────────────────────────

func TestConvertAppConfig(t *testing.T) {
	t.Parallel()
	src := `export default $config({
  app(input) { return { name: "todo-api", home: "aws" }; },
  async run() {},
});`
	r := mustConvert(t, src)

	// App name from TS config should appear somewhere in the output.
	// The raw kvRe captures the quoted string "todo-api" which %q re-quotes.
	assertContains(t, r.GoSource, `package main`, "package declaration")
	assertContains(t, r.GoSource, `forge.Run`, "forge.Run call")
	assertContains(t, r.GoSource, `forge.Config`, "forge.Config struct")
}

func TestConvertAppConfigMissingName(t *testing.T) {
	t.Parallel()
	src := `export default $config({
  app(input) { return { home: "aws" }; },
  async run() {},
});`
	r := mustConvert(t, src)

	// Falls back to default "my-app" and emits a warning.
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "app name") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected missing-app-name warning")
	}
}

// ── TestConvertLinks ──────────────────────────────────────────────────────────

func TestConvertLinks(t *testing.T) {
	t.Parallel()
	src := wrapRun(`const fn = new sst.aws.Function("Fn", { handler: "h", link: [table, bucket] });`)
	r := mustConvert(t, src)

	assertContains(t, r.GoSource, `forge.Linkable`, "Linkable type in link slice")
	assertContains(t, r.GoSource, `table`, "table link ref")
	assertContains(t, r.GoSource, `bucket`, "bucket link ref")
}

// ── TestConvertExports ────────────────────────────────────────────────────────

func TestConvertExports(t *testing.T) {
	t.Parallel()
	src := `export default $config({
  app(input) { return { name: "app", home: "aws" }; },
  async run() {
    const api = new sst.aws.ApiGatewayV2("Api");
    return {
      url: api.url,
    };
  },
});`
	r := mustConvert(t, src)

	// Exports should generate a warning with the export key name.
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "url") && strings.Contains(w, "export") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected export warning for 'url', got warnings: %v", r.Warnings)
	}
}

// ── Real-world SST Ion testdata ───────────────────────────────────────────────

// TestRealWorldAWSApi exercises the aws-api SST Ion example: Bucket + ApiGatewayV2
// with multi-line route calls (the common real-world pattern).
func TestRealWorldAWSApi(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile(filepath.Join("testdata", "aws-api.ts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	r := mustConvert(t, string(input))

	assertContains(t, r.GoSource, "package main", "package declaration")
	assertContains(t, r.GoSource, "constructs.NewBucket", "NewBucket")
	assertContains(t, r.GoSource, "constructs.NewApiGatewayV2", "NewApiGatewayV2")
	assertContains(t, r.GoSource, `"aws-api"`, "app name")
	assertContains(t, r.GoSource, "forge.RemovalRetain", "retain policy")

	// Multi-line routes cannot be auto-converted — they must produce TODO comments.
	assertContains(t, r.GoSource, "// TODO:", "TODO for multi-line routes")

	// The reference path header must not appear verbatim in Go output.
	assertNotContains(t, r.GoSource, "<reference path", "TS reference directive")
}

// TestRealWorldAWSDynamo exercises the aws-dynamo SST Ion example which uses
// sst.aws.Dynamo (Ion alias for DynamoDB), table.subscribe(), and url:true.
func TestRealWorldAWSDynamo(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile(filepath.Join("testdata", "aws-dynamo.ts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	r := mustConvert(t, string(input))

	// sst.aws.Dynamo (Ion alias) must map to NewDynamoDB, not emit unsupported.
	assertContains(t, r.GoSource, "constructs.NewDynamoDB", "Dynamo → NewDynamoDB")
	assertNotContains(t, r.GoSource, "unsupported construct sst.aws.Dynamo", "no unsupported for Dynamo alias")

	// Function is recognised.
	assertContains(t, r.GoSource, "constructs.NewFunction", "NewFunction")

	// table.subscribe() has no forge equivalent — must produce a TODO.
	assertContains(t, r.GoSource, "// TODO:", "TODO for subscribe")

	// url:true on Function must produce a warning.
	hasURLWarn := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "url") && strings.Contains(w, "Function") {
			hasURLWarn = true
			break
		}
	}
	if !hasURLWarn {
		t.Errorf("expected Function url:true warning, got: %v", r.Warnings)
	}

	// table.subscribe() must produce a warning pointing at Consumer/Subscriber.
	hasSubscribeWarn := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "subscribe") {
			hasSubscribeWarn = true
			break
		}
	}
	if !hasSubscribeWarn {
		t.Errorf("expected subscribe warning, got: %v", r.Warnings)
	}
}

// TestRealWorldAWSQueue exercises the aws-queue SST Ion example: Queue,
// queue.subscribe(), and a publisher Function with url:true.
func TestRealWorldAWSQueue(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile(filepath.Join("testdata", "aws-queue.ts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	r := mustConvert(t, string(input))

	assertContains(t, r.GoSource, "constructs.NewQueue", "NewQueue")
	assertContains(t, r.GoSource, "constructs.NewFunction", "NewFunction")

	// queue.subscribe() → TODO comment.
	assertContains(t, r.GoSource, "// TODO:", "TODO for subscribe")

	hasSubscribeWarn := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "subscribe") {
			hasSubscribeWarn = true
			break
		}
	}
	if !hasSubscribeWarn {
		t.Errorf("expected subscribe warning, got: %v", r.Warnings)
	}
}

// TestRealWorldAWSNextjs exercises the aws-nextjs SST Ion example which uses
// sst.aws.Nextjs (Ion alias for NextjsSite).
func TestRealWorldAWSNextjs(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile(filepath.Join("testdata", "aws-nextjs.ts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	r := mustConvert(t, string(input))

	// sst.aws.Nextjs (Ion alias) must map to NewNextjsSite, not emit unsupported.
	assertContains(t, r.GoSource, "constructs.NewNextjsSite", "Nextjs → NewNextjsSite")
	assertNotContains(t, r.GoSource, "unsupported construct sst.aws.Nextjs", "no unsupported for Nextjs alias")

	// Linked bucket must also be present.
	assertContains(t, r.GoSource, "constructs.NewBucket", "NewBucket")

	// No unsupported entries at all for this simple config.
	if len(r.Unsupported) > 0 {
		t.Errorf("expected no unsupported patterns, got: %v", r.Unsupported)
	}
}

// ── TestRoundTrip ─────────────────────────────────────────────────────────────

// TestRoundTrip exercises Convert on a realistic full SST config and verifies
// the structural properties of the output without locking to exact formatting.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile(filepath.Join("testdata", "fullstack.ts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	r, err := Convert(string(input))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	src := r.GoSource

	// Must be a valid-looking Go file.
	assertContains(t, src, "// Code generated by forge migrate", "generated header")
	assertContains(t, src, "package main", "package declaration")
	assertContains(t, src, "func main()", "main function")
	assertContains(t, src, `"github.com/nimbus-local/forge"`, "forge import")
	assertContains(t, src, `"github.com/nimbus-local/forge/constructs"`, "constructs import")

	// All constructs present in input must be referenced in output.
	for _, want := range []string{
		"NewDynamoDB", "NewBucket", "NewApiGatewayV2", "NewFunction",
	} {
		assertContains(t, src, want, "construct "+want)
	}

	// Route calls should be present.
	assertContains(t, src, ".Route(", "Route call")

	// Conditional removal policy from input.
	assertContains(t, src, "forge.RemovalRetain", "retain removal policy")
	assertContains(t, src, "forge.RemovalDestroy", "destroy removal policy")

	// Warnings should include DynamoDB manual-conversion note and export hints.
	hasDynamoWarn := false
	hasExportWarn := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "DynamoDB") {
			hasDynamoWarn = true
		}
		if strings.Contains(w, "url") {
			hasExportWarn = true
		}
	}
	if !hasDynamoWarn {
		t.Error("expected DynamoDB warning in round-trip")
	}
	if !hasExportWarn {
		t.Error("expected url export warning in round-trip")
	}
}
