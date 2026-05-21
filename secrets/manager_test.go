package secrets

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// ── mock SSM client ───────────────────────────────────────────────────────────

type mockSSM struct {
	params map[string]string
}

func newMockSSM() *mockSSM { return &mockSSM{params: map[string]string{}} }

func (m *mockSSM) PutParameter(_ context.Context, in *ssm.PutParameterInput, _ ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	m.params[aws.ToString(in.Name)] = aws.ToString(in.Value)
	return &ssm.PutParameterOutput{}, nil
}

func (m *mockSSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	name := aws.ToString(in.Name)
	val, ok := m.params[name]
	if !ok {
		return nil, &ssm_types.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{
		Parameter: &ssm_types.Parameter{
			Name:  aws.String(name),
			Value: aws.String(val),
		},
	}, nil
}

func (m *mockSSM) DeleteParameter(_ context.Context, in *ssm.DeleteParameterInput, _ ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	name := aws.ToString(in.Name)
	if _, ok := m.params[name]; !ok {
		return nil, &ssm_types.ParameterNotFound{}
	}
	delete(m.params, name)
	return &ssm.DeleteParameterOutput{}, nil
}

func (m *mockSSM) GetParametersByPath(_ context.Context, in *ssm.GetParametersByPathInput, _ ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	prefix := aws.ToString(in.Path)
	var out []ssm_types.Parameter
	for k, v := range m.params {
		if strings.HasPrefix(k, prefix) {
			out = append(out, ssm_types.Parameter{
				Name:  aws.String(k),
				Value: aws.String(v),
			})
		}
	}
	return &ssm.GetParametersByPathOutput{Parameters: out}, nil
}

// ── helper ────────────────────────────────────────────────────────────────────

func newTestManager() *Manager {
	return newWithClient(newMockSSM(), "myapp", "test")
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSet(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	if err := m.Set(context.Background(), "DB_URL", "postgres://localhost/test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	ctx := context.Background()

	if err := m.Set(ctx, "API_KEY", "secret-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := m.Get(ctx, "API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "secret-value" {
		t.Errorf("Get = %q, want %q", got, "secret-value")
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	m := newTestManager()

	_, err := m.Get(context.Background(), "MISSING")
	if err == nil {
		t.Fatal("Get of missing secret should return error")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error should mention secret name, got: %v", err)
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	ctx := context.Background()

	if err := m.Set(ctx, "TMP", "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Remove(ctx, "TMP"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Second remove should fail.
	if err := m.Remove(ctx, "TMP"); err == nil {
		t.Error("Remove of already-deleted secret should return error")
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	ctx := context.Background()

	for _, name := range []string{"A", "B", "C"} {
		if err := m.Set(ctx, name, name+"-val"); err != nil {
			t.Fatalf("Set(%q): %v", name, err)
		}
	}

	names, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 3 {
		t.Errorf("List returned %d names, want 3", len(names))
	}
}

func TestListEmpty(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	names, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("empty List should return 0 names, got %d", len(names))
	}
}

func TestLoadAll(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	ctx := context.Background()

	secrets := map[string]string{"X": "x-val", "Y": "y-val"}
	for k, v := range secrets {
		if err := m.Set(ctx, k, v); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	got, err := m.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for k, want := range secrets {
		// LoadAll returns the full SSM path as key; strip prefix.
		prefix := "/forge/myapp/test/"
		fullKey := prefix + k
		if v, ok := got[fullKey]; !ok {
			// Also check without prefix (trimming happens in LoadAll).
			if v2, ok2 := got[k]; ok2 {
				if v2 != want {
					t.Errorf("LoadAll[%q] = %q, want %q", k, v2, want)
				}
			} else {
				t.Errorf("LoadAll missing key %q (tried %q and %q)", k, k, fullKey)
			}
		} else if v != want {
			t.Errorf("LoadAll[%q] = %q, want %q", fullKey, v, want)
		}
	}
}

func TestNew_EndpointWiring(t *testing.T) {
	// Verifies that New() applies FORGE_AWS_ENDPOINT to the SSM client.
	// Uses a port unlikely to be in use; we only check that the client is
	// constructed without error — the endpoint is validated on first API call.
	t.Setenv("FORGE_AWS_ENDPOINT", "http://localhost:19566")
	m, err := New("app", "test", "", "us-east-1")
	if err != nil {
		t.Fatalf("New with FORGE_AWS_ENDPOINT set: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil Manager")
	}
}

func TestSetOverwrite(t *testing.T) {
	t.Parallel()
	m := newTestManager()
	ctx := context.Background()

	if err := m.Set(ctx, "KEY", "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Set(ctx, "KEY", "second"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}

	got, err := m.Get(ctx, "KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("after overwrite Get = %q, want %q", got, "second")
	}
}
