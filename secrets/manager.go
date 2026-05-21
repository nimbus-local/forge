// Package secrets provides SSM Parameter Store backed secrets management,
// equivalent to SST's `sst secret` commands.
package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// ssmAPI is the subset of *ssm.Client used by Manager, extracted for testing.
type ssmAPI interface {
	PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
	GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

// Manager handles secret CRUD operations against SSM Parameter Store.
// Secrets are stored at /forge/<app>/<stage>/<name> as SecureString parameters.
type Manager struct {
	client  ssmAPI
	appName string
	stage   string
}

// New creates a Manager using the default AWS credential chain.
func New(appName, stage, awsProfile, awsRegion string) (*Manager, error) {
	opts := []func(*config.LoadOptions) error{}
	if awsProfile != "" {
		opts = append(opts, config.WithSharedConfigProfile(awsProfile))
	}
	if awsRegion != "" {
		opts = append(opts, config.WithRegion(awsRegion))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return newWithClient(ssm.NewFromConfig(cfg), appName, stage), nil
}

// newWithClient constructs a Manager with an injected client — used in tests.
func newWithClient(client ssmAPI, appName, stage string) *Manager {
	return &Manager{client: client, appName: appName, stage: stage}
}

// Set stores or overwrites a secret value.
func (m *Manager) Set(ctx context.Context, name, value string) error {
	_, err := m.client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(m.path(name)),
		Value:     aws.String(value),
		Type:      types.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
		Tags: []types.Tag{
			{Key: aws.String("forge:app"), Value: aws.String(m.appName)},
			{Key: aws.String("forge:stage"), Value: aws.String(m.stage)},
		},
	})
	if err != nil {
		return fmt.Errorf("set secret %q: %w", name, err)
	}
	return nil
}

// Get retrieves and decrypts a secret value.
func (m *Manager) Get(ctx context.Context, name string) (string, error) {
	out, err := m.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(m.path(name)),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("get secret %q: %w", name, err)
	}
	return *out.Parameter.Value, nil
}

// Remove deletes a secret.
func (m *Manager) Remove(ctx context.Context, name string) error {
	_, err := m.client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(m.path(name)),
	})
	if err != nil {
		return fmt.Errorf("remove secret %q: %w", name, err)
	}
	return nil
}

// List returns all secret names for the current app+stage.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	prefix := fmt.Sprintf("/forge/%s/%s/", m.appName, m.stage)
	var names []string
	var nextToken *string

	for {
		out, err := m.client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:           aws.String(prefix),
			WithDecryption: aws.Bool(false),
			NextToken:      nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list secrets: %w", err)
		}
		for _, p := range out.Parameters {
			name := strings.TrimPrefix(*p.Name, prefix)
			names = append(names, name)
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return names, nil
}

// LoadAll fetches all secrets for the stage as a map[name]value.
// Used by the dev tunnel to inject secrets as Lambda environment variables.
func (m *Manager) LoadAll(ctx context.Context) (map[string]string, error) {
	prefix := fmt.Sprintf("/forge/%s/%s/", m.appName, m.stage)
	result := map[string]string{}
	var nextToken *string

	for {
		out, err := m.client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:           aws.String(prefix),
			WithDecryption: aws.Bool(true),
			NextToken:      nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("load secrets: %w", err)
		}
		for _, p := range out.Parameters {
			name := strings.TrimPrefix(*p.Name, prefix)
			result[name] = *p.Value
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return result, nil
}

func (m *Manager) path(name string) string {
	return fmt.Sprintf("/forge/%s/%s/%s", m.appName, m.stage, name)
}
