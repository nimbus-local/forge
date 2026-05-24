package constructs

import (
	"fmt"
	"path/filepath"
	"testing"

	forge "github.com/nimbus-local/forge"
)

func TestQualifiedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		appName string
		stage   string
		name    string
		want    string
	}{
		{"myapp", "dev", "UsersTable", "myapp-dev-UsersTable"},
		{"todo", "prod", "Api", "todo-prod-Api"},
		{"my-app", "staging", "uploads-bucket", "my-app-staging-uploads-bucket"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			ctx := &forge.RunContext{
				Stage: tc.stage,
				App:   &forge.AppConfig{Name: tc.appName},
			}
			if got := qualifiedName(ctx, tc.name); got != tc.want {
				t.Errorf("qualifiedName(%q, %q, %q) = %q, want %q", tc.appName, tc.stage, tc.name, got, tc.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()
	ctx := &forge.RunContext{
		Stage:   "dev",
		App:     &forge.AppConfig{Name: "myapp"},
		WorkDir: "/home/user/project/infra",
	}

	t.Run("relative path resolved against WorkDir", func(t *testing.T) {
		t.Parallel()
		got := resolvePath(ctx, "../functions/api.zip")
		want := filepath.Join("/home/user/project/infra", "../functions/api.zip")
		if got != want {
			t.Errorf("resolvePath(relative) = %q, want %q", got, want)
		}
	})

	t.Run("absolute path returned unchanged", func(t *testing.T) {
		t.Parallel()
		abs := "/usr/local/functions/api.zip"
		got := resolvePath(ctx, abs)
		if got != abs {
			t.Errorf("resolvePath(absolute) = %q, want %q", got, abs)
		}
	})

	t.Run("dot path resolved against WorkDir", func(t *testing.T) {
		t.Parallel()
		got := resolvePath(ctx, ".")
		want := filepath.Join("/home/user/project/infra", ".")
		if got != want {
			t.Errorf("resolvePath(.) = %q, want %q", got, want)
		}
	})
}

func TestBucketName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		appName, stage, accountID, name, want string
	}{
		{"myapp", "dev", "123456789012", "Uploads", "myapp-dev-uploads-123456789012"},
		{"todo-api", "prod", "999999999999", "Assets", "todo-api-prod-assets-999999999999"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			ctx := &forge.RunContext{
				Stage:     tc.stage,
				App:       &forge.AppConfig{Name: tc.appName},
				AccountID: tc.accountID,
			}
			if got := bucketName(ctx, tc.name); got != tc.want {
				t.Errorf("bucketName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestEnvKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"MyTable", "MY_TABLE"},
		{"todo-api", "TODO_API"},
		{"usersTable", "USERS_TABLE"},
		{"MyBucket", "MY_BUCKET"},
		{"simple", "SIMPLE"},
		{"S", "S"},
		{"MyApiGateway", "MY_API_GATEWAY"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := envKey(tc.in); got != tc.want {
				t.Errorf("envKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveLogRetention(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   int
		want int
	}{
		{0, 14},  // default
		{-1, 0},  // never expire → CloudWatch 0
		{14, 14}, // valid, returned as-is
		{30, 30},
		{365, 365},
		{3653, 3653},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("days=%d", tc.in), func(t *testing.T) {
			t.Parallel()
			if got := resolveLogRetention(tc.in); got != tc.want {
				t.Errorf("resolveLogRetention(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveLogRetention_InvalidPanics(t *testing.T) {
	t.Parallel()
	for _, bad := range []int{2, 4, 6, 8, 100, 999} {
		bad := bad
		t.Run(fmt.Sprintf("days=%d", bad), func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for invalid LogRetentionDays %d", bad)
				}
			}()
			resolveLogRetention(bad)
		})
	}
}

func TestDefaultTags(t *testing.T) {
	t.Parallel()

	t.Run("base tags present", func(t *testing.T) {
		t.Parallel()
		ctx := &forge.RunContext{
			Stage: "prod",
			App:   &forge.AppConfig{Name: "myapp"},
		}
		tags := defaultTags(ctx, "MyResource")
		for _, key := range []string{"forge:app", "forge:stage", "forge:name"} {
			if _, ok := tags[key]; !ok {
				t.Errorf("defaultTags missing key %q", key)
			}
		}
		if len(tags) != 3 {
			t.Errorf("want 3 base tags, got %d", len(tags))
		}
	})

	t.Run("extra tags from stage config", func(t *testing.T) {
		t.Parallel()
		// ExtraTags() reads the unexported stageTags field, so we exercise
		// the no-extra-tags path here; extra-tags is covered by the integration path.
		ctx := &forge.RunContext{
			Stage: "dev",
			App:   &forge.AppConfig{Name: "myapp"},
		}
		tags := defaultTags(ctx, "res")
		if len(tags) != 3 {
			t.Errorf("want exactly 3 tags when no stage extras, got %d", len(tags))
		}
	})
}
