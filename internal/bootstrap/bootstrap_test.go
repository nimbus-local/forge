package bootstrap

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ── mock S3 client ────────────────────────────────────────────────────────────

type mockS3 struct {
	buckets     map[string]bool
	headErr     error // returned by HeadBucket; nil = check map
	createErr   error
	settingsErr error // returned by all PutBucket* calls
}

func newMockS3() *mockS3 { return &mockS3{buckets: map[string]bool{}} }

func (m *mockS3) HeadBucket(_ context.Context, in *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headErr != nil {
		return nil, m.headErr
	}
	name := aws.ToString(in.Bucket)
	if !m.buckets[name] {
		return nil, &s3_types.NotFound{}
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *mockS3) CreateBucket(_ context.Context, in *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.buckets[aws.ToString(in.Bucket)] = true
	return &s3.CreateBucketOutput{}, nil
}

func (m *mockS3) PutPublicAccessBlock(_ context.Context, _ *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	return &s3.PutPublicAccessBlockOutput{}, m.settingsErr
}

func (m *mockS3) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, m.settingsErr
}

func (m *mockS3) PutBucketEncryption(_ context.Context, _ *s3.PutBucketEncryptionInput, _ ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error) {
	return &s3.PutBucketEncryptionOutput{}, m.settingsErr
}

func (m *mockS3) PutBucketLifecycleConfiguration(_ context.Context, _ *s3.PutBucketLifecycleConfigurationInput, _ ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
	return &s3.PutBucketLifecycleConfigurationOutput{}, m.settingsErr
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestBucketName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		app, stage, want string
	}{
		{"myapp", "dev", "myapp-dev-forge-state"},
		{"todo-api", "prod", "todo-api-prod-forge-state"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := BucketName(tc.app, tc.stage); got != tc.want {
				t.Errorf("BucketName(%q, %q) = %q, want %q", tc.app, tc.stage, got, tc.want)
			}
		})
	}
}

func TestEnsureWithClient_BucketNotExist_CreatesIt(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	created, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err != nil {
		t.Fatalf("ensureWithClient: %v", err)
	}
	if !created {
		t.Error("want created=true when bucket did not exist")
	}
	if !mock.buckets["my-bucket"] {
		t.Error("bucket should exist in mock after creation")
	}
}

func TestEnsureWithClient_Idempotent(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	mock.buckets["my-bucket"] = true // bucket already exists

	created, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err != nil {
		t.Fatalf("ensureWithClient: %v", err)
	}
	if created {
		t.Error("want created=false when bucket already exists")
	}
}

func TestEnsureWithClient_NonUsEast1Region(t *testing.T) {
	t.Parallel()
	// Verifies that a LocationConstraint is set for non-us-east-1 regions.
	var gotConstraint string
	mock := newMockS3()
	mock.buckets = map[string]bool{}

	// Override CreateBucket to capture the region constraint.
	captureMock := &createCaptureMock{inner: mock, onCreateBucket: func(in *s3.CreateBucketInput) {
		if in.CreateBucketConfiguration != nil {
			gotConstraint = string(in.CreateBucketConfiguration.LocationConstraint)
		}
	}}

	_, err := ensureWithClient(context.Background(), captureMock, "my-bucket", "us-west-2")
	if err != nil {
		t.Fatalf("ensureWithClient: %v", err)
	}
	if gotConstraint != "us-west-2" {
		t.Errorf("LocationConstraint = %q, want %q", gotConstraint, "us-west-2")
	}
}

func TestEnsureWithClient_HeadBucketError(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	mock.headErr = fmt.Errorf("access denied")

	_, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err == nil {
		t.Fatal("want error when HeadBucket fails")
	}
}

func TestEnsureWithClient_CreateBucketError(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	mock.createErr = fmt.Errorf("bucket name taken")

	_, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err == nil {
		t.Fatal("want error when CreateBucket fails")
	}
}

func TestEnsureWithClient_SettingsError(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	mock.settingsErr = fmt.Errorf("settings failure")

	_, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err == nil {
		t.Fatal("want error when bucket settings fail")
	}
}

func TestEnsureWithClient_HTTP404CountsAsMissing(t *testing.T) {
	t.Parallel()
	// Simulate an HTTP-404-style error (not types.NotFound) from HeadBucket.
	mock := newMockS3()
	mock.headErr = &httpStatusError{code: 404}

	created, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err != nil {
		t.Fatalf("ensureWithClient: %v", err)
	}
	if !created {
		t.Error("want created=true when HeadBucket returns HTTP 404")
	}
}

func TestEnsureWithClient_HTTP403IsRealError(t *testing.T) {
	t.Parallel()
	mock := newMockS3()
	mock.headErr = &httpStatusError{code: 403}

	_, err := ensureWithClient(context.Background(), mock, "my-bucket", "us-east-1")
	if err == nil {
		t.Fatal("want error when HeadBucket returns HTTP 403")
	}
}

// httpStatusError is a minimal error that satisfies the HTTPStatusCode() int interface
// used in the bucketExists fallback.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string        { return fmt.Sprintf("http %d", e.code) }
func (e *httpStatusError) HTTPStatusCode() int  { return e.code }

// ── createCaptureMock ─────────────────────────────────────────────────────────

// createCaptureMock wraps mockS3 to intercept CreateBucket calls.
type createCaptureMock struct {
	inner          *mockS3
	onCreateBucket func(*s3.CreateBucketInput)
}

func (c *createCaptureMock) HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return c.inner.HeadBucket(ctx, in, opts...)
}
func (c *createCaptureMock) CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if c.onCreateBucket != nil {
		c.onCreateBucket(in)
	}
	return c.inner.CreateBucket(ctx, in, opts...)
}
func (c *createCaptureMock) PutPublicAccessBlock(ctx context.Context, in *s3.PutPublicAccessBlockInput, opts ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	return c.inner.PutPublicAccessBlock(ctx, in, opts...)
}
func (c *createCaptureMock) PutBucketVersioning(ctx context.Context, in *s3.PutBucketVersioningInput, opts ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return c.inner.PutBucketVersioning(ctx, in, opts...)
}
func (c *createCaptureMock) PutBucketEncryption(ctx context.Context, in *s3.PutBucketEncryptionInput, opts ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error) {
	return c.inner.PutBucketEncryption(ctx, in, opts...)
}
func (c *createCaptureMock) PutBucketLifecycleConfiguration(ctx context.Context, in *s3.PutBucketLifecycleConfigurationInput, opts ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
	return c.inner.PutBucketLifecycleConfiguration(ctx, in, opts...)
}
