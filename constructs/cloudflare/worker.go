package cloudflare

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/nimbus-local/forge"
)

// WorkerArgs configures a Cloudflare Worker script construct.
type WorkerArgs struct {
	// Handler is the path to a JS/TS entry point. Bundled with the esbuild CLI if available,
	// otherwise read as-is. Use for JavaScript/TypeScript workers.
	Handler string
	// GoHandler is the path to a Go package compiled to WASM at deploy time.
	// The package must export a 'fetch' function via syscall/js or implement WASI (wasip1).
	// Requires go toolchain in PATH with GOARCH=wasm GOOS=wasip1 support.
	GoHandler string
	// CompatibilityDate pins the Workers runtime version (e.g. "2024-01-01").
	// Defaults to the date the stack was first deployed if omitted.
	CompatibilityDate string
	// KVBindings attaches KV namespaces as Worker bindings.
	// The binding variable name in JS is the namespace's envKey (SCREAMING_SNAKE_CASE).
	KVBindings []*KVNamespace
	// D1Bindings attaches D1 databases as Worker bindings.
	D1Bindings []*D1Database
	// R2Bindings attaches R2 buckets as Worker bindings.
	R2Bindings []*R2Bucket
	// Link injects linked resources as plain-text env bindings (SST_* variables).
	// Use this to share ARNs, URLs, or IDs from AWS constructs with a Worker.
	Link []forge.Linkable
	// Domains attaches custom hostnames to the Worker. Requires ZoneID in CloudflareConfig.
	Domains []string
}

// Worker is a Cloudflare Worker script construct.
type Worker struct {
	name     string
	resource *cf.WorkersScript
	ctx      *forge.RunContext
}

// NewWorker creates a Cloudflare Worker script.
func NewWorker(ctx *forge.RunContext, name string, args *WorkerArgs) *Worker {
	if args == nil {
		args = &WorkerArgs{}
	}
	if args.Handler == "" && args.GoHandler == "" {
		panic("forge: WorkerArgs.Handler or WorkerArgs.GoHandler must be set for " + name)
	}

	pctx := ctx.Pulumi()

	// ── Build worker content ──────────────────────────────────────────────────
	content, webassemblyBindings := buildWorkerContent(name, args)

	// ── KV namespace bindings ─────────────────────────────────────────────────
	var kvBindings cf.WorkersScriptKvNamespaceBindingArray
	for _, kv := range args.KVBindings {
		kvBindings = append(kvBindings, cf.WorkersScriptKvNamespaceBindingArgs{
			Name:        pulumi.String(envKey(kv.name)),
			NamespaceId: kv.resource.ID().ToStringOutput(),
		})
	}

	// ── D1 database bindings ──────────────────────────────────────────────────
	var d1Bindings cf.WorkersScriptD1DatabaseBindingArray
	for _, d1 := range args.D1Bindings {
		d1Bindings = append(d1Bindings, cf.WorkersScriptD1DatabaseBindingArgs{
			Name:       pulumi.String(envKey(d1.name)),
			DatabaseId: d1.resource.ID().ToStringOutput(),
		})
	}

	// ── R2 bucket bindings ────────────────────────────────────────────────────
	var r2Bindings cf.WorkersScriptR2BucketBindingArray
	for _, r2 := range args.R2Bindings {
		r2Bindings = append(r2Bindings, cf.WorkersScriptR2BucketBindingArgs{
			Name:       pulumi.String(envKey(r2.name)),
			BucketName: r2.resource.Name,
		})
	}

	// ── Plain-text bindings from linked resources ─────────────────────────────
	var plainTextBindings cf.WorkersScriptPlainTextBindingArray
	for _, link := range args.Link {
		for k, v := range link.LinkEnv() {
			v := v // capture
			plainTextBindings = append(plainTextBindings, cf.WorkersScriptPlainTextBindingArgs{
				Name: pulumi.String(k),
				Text: v,
			})
		}
	}

	scriptArgs := &cf.WorkersScriptArgs{
		AccountId: pulumi.String(accountID(ctx)),
		Name:      pulumi.String(qualifiedName(ctx, name)),
		Content:   pulumi.String(content),
	}

	if len(kvBindings) > 0 {
		scriptArgs.KvNamespaceBindings = kvBindings
	}
	if len(d1Bindings) > 0 {
		scriptArgs.D1DatabaseBindings = d1Bindings
	}
	if len(r2Bindings) > 0 {
		scriptArgs.R2BucketBindings = r2Bindings
	}
	if len(webassemblyBindings) > 0 {
		scriptArgs.WebassemblyBindings = webassemblyBindings
	}
	if len(plainTextBindings) > 0 {
		scriptArgs.PlainTextBindings = plainTextBindings
	}
	if args.CompatibilityDate != "" {
		scriptArgs.CompatibilityDate = pulumi.StringPtr(args.CompatibilityDate)
	}

	script, err := cf.NewWorkersScript(pctx, name, scriptArgs)
	panicOnErr(err, name+": workers script")

	// ── Custom domain routes ──────────────────────────────────────────────────
	zid := zoneID(ctx)
	for _, domain := range args.Domains {
		if zid == "" {
			panic(fmt.Sprintf("forge: Worker %q has Domains but no ZoneID — set AppConfig.Cloudflare.ZoneID or CLOUDFLARE_ZONE_ID", name))
		}
		_, err = cf.NewWorkersDomain(pctx, name+"-domain-"+sanitize(domain), &cf.WorkersDomainArgs{
			AccountId: pulumi.String(accountID(ctx)),
			Hostname:  pulumi.String(domain),
			Service:   script.Name,
			ZoneId:    pulumi.String(zid),
		})
		panicOnErr(err, name+": worker domain "+domain)
	}

	return &Worker{name: name, resource: script, ctx: ctx}
}

// Name returns the Worker script name as a Pulumi output.
func (w *Worker) Name() pulumi.StringOutput { return w.resource.Name }

// LinkEnv implements forge.Linkable — exposes the worker name to linked resources.
func (w *Worker) LinkEnv() pulumi.StringMap {
	return pulumi.StringMap{
		fmt.Sprintf("SST_WORKER_%s_NAME", envKey(w.name)): w.resource.Name,
	}
}

// LinkName implements forge.Linkable.
func (w *Worker) LinkName() string { return w.name }

// ── Build helpers ─────────────────────────────────────────────────────────────

// buildWorkerContent returns the JS content string and any WASM bindings.
func buildWorkerContent(name string, args *WorkerArgs) (string, cf.WorkersScriptWebassemblyBindingArray) {
	if args.GoHandler != "" {
		return buildGoWASMWorker(name, args.GoHandler)
	}
	return buildJSWorker(args.Handler), nil
}

// buildJSWorker reads a JS/TS entry point, bundling with esbuild CLI if available.
func buildJSWorker(handler string) string {
	abs, err := filepath.Abs(handler)
	if err != nil {
		panic("forge: cannot resolve Handler path: " + err.Error())
	}

	// Try to bundle with esbuild CLI (must be in PATH).
	if esbuildPath, lookErr := exec.LookPath("esbuild"); lookErr == nil {
		cmd := exec.Command(esbuildPath,
			abs,
			"--bundle",
			"--platform=browser",
			"--format=esm",
			"--target=es2022",
		)
		out, runErr := cmd.Output()
		if runErr == nil {
			return string(out)
		}
		// esbuild failed — fall through to raw read
	}

	// Fall back to reading the file as-is.
	content, readErr := os.ReadFile(abs)
	if readErr != nil {
		panic(fmt.Sprintf("forge: cannot read Handler %q: %v", handler, readErr))
	}
	return string(content)
}

// buildGoWASMWorker compiles a Go package to WASM (wasip1) and returns the JS wrapper
// plus the WASM binary as a WebassemblyBinding named GOWORKER.
func buildGoWASMWorker(name, goHandler string) (string, cf.WorkersScriptWebassemblyBindingArray) {
	tmp, err := os.CreateTemp("", "forge-worker-*.wasm")
	if err != nil {
		panic("forge: cannot create temp file for WASM: " + err.Error())
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	abs, err := filepath.Abs(goHandler)
	if err != nil {
		panic("forge: cannot resolve GoHandler path: " + err.Error())
	}

	cmd := exec.Command("go", "build", "-o", tmp.Name(), abs)
	cmd.Env = append(os.Environ(), "GOARCH=wasm", "GOOS=wasip1")
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		panic(fmt.Sprintf("forge: go build WASM failed for %q:\n%s", goHandler, out))
	}

	wasmBytes, err := os.ReadFile(tmp.Name())
	if err != nil {
		panic("forge: cannot read compiled WASM: " + err.Error())
	}

	wasmB64 := base64.StdEncoding.EncodeToString(wasmBytes)

	// Minimal WASI-compatible JS wrapper. The WASM module is bound as GOWORKER.
	// Cloudflare Workers with WASI (wasip1) require the wasi-shim polyfill for full
	// WASI compatibility; this wrapper covers the common case of a simple fetch handler.
	jsWrapper := `
import { WASI } from '@cloudflare/workers-wasi';

export default {
  async fetch(request, env, ctx) {
    const wasi = new WASI({ env: Object.entries(env).reduce((a,[k,v])=>(a[k]=String(v),a),{}) });
    const { instance } = await WebAssembly.instantiate(env.GOWORKER, wasi.getImportObject());
    wasi.initialize(instance);
    if (typeof instance.exports.fetch === 'function') {
      return instance.exports.fetch(request, env, ctx);
    }
    return new Response('Worker running (no fetch export)', { status: 200 });
  },
};
`

	bindings := cf.WorkersScriptWebassemblyBindingArray{
		cf.WorkersScriptWebassemblyBindingArgs{
			Name:   pulumi.String("GOWORKER"),
			Module: pulumi.String(wasmB64),
		},
	}

	return strings.TrimSpace(jsWrapper), bindings
}

// sanitize replaces characters that are invalid in Pulumi resource names.
func sanitize(s string) string {
	return strings.NewReplacer(".", "-", "/", "-", ":", "-").Replace(s)
}
