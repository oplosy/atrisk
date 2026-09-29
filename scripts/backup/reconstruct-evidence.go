package main

import (
	"context"
	"fmt"
	"os"

	"github.com/oplosy/atrisk/internal/application/evidence"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("RESTORE_PG_DSN")
	if dsn == "" {
		return fmt.Errorf("RESTORE_PG_DSN is required")
	}
	decisionID := os.Getenv("RESTORE_DECISION_ID")
	if decisionID == "" {
		return fmt.Errorf("RESTORE_DECISION_ID is required")
	}
	p, err := database.OpenPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer p.Close()
	store, err := archive.NewS3StoreFromConfig(ctx, archive.ClientConfig{
		Endpoint: os.Getenv("RESTORE_S3_ENDPOINT"), Region: os.Getenv("RESTORE_S3_REGION"),
		AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Bucket: os.Getenv("RESTORE_S3_BUCKET"),
	})
	if err != nil {
		return err
	}
	sealed, err := (evidence.Service{Pool: p, Archive: store}).Reconstruct(ctx, decisionID)
	if err != nil {
		return fmt.Errorf("reconstruct restored sealed decision: %w", err)
	}
	fmt.Println(sealed.SHA256)
	return nil
}
