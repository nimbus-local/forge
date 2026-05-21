package main

import (
	"context"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

var stagesCmd = &cobra.Command{
	Use:   "stages",
	Short: "List all deployed stages",
	Long: `List all stages that have been deployed for this app.

Stages are discovered by finding Pulumi state buckets that match
the naming pattern <app>-<stage>-forge-state in your AWS account.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := appNameFromConfig()

		stages, err := listDeployedStages(context.Background(), appName)
		if err != nil {
			return fmt.Errorf("list stages: %w", err)
		}

		if len(stages) == 0 {
			fmt.Printf("\n%s  No deployed stages found for app %s\n",
				dim.Render("○"), bold.Render(appName))
			fmt.Printf("%s  Deploy first with: %s\n\n",
				dim.Render("  "), bold.Render("forge deploy --stage <name>"))
			return nil
		}

		fmt.Printf("\n%s  Stages for %s\n\n", bold.Render("◈"), bold.Render(appName))
		for _, stage := range stages {
			fmt.Printf("  %s  %s\n", dim.Render("•"), stage)
		}
		fmt.Println()
		return nil
	},
}

// listDeployedStages finds all state buckets matching <app>-*-forge-state and
// returns the stage names extracted from those bucket names.
func listDeployedStages(ctx context.Context, appName string) ([]string, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if flagProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(flagProfile))
	}
	if flagRegion != "" {
		opts = append(opts, awsconfig.WithRegion(flagRegion))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}

	prefix := appName + "-"
	suffix := "-forge-state"

	var stages []string
	for _, b := range out.Buckets {
		name := *b.Name
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			stage := strings.TrimPrefix(name, prefix)
			stage = strings.TrimSuffix(stage, suffix)
			if stage != "" {
				stages = append(stages, stage)
			}
		}
	}
	return stages, nil
}
