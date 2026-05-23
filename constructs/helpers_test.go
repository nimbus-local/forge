package constructs

import (
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
