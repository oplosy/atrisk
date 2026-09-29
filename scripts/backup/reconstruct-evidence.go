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
	ctx := context.Background()
	dsn := os.Getenv("RESTORE_PG_DSN")
	if dsn == "" {
		panic("RESTORE_PG_DSN is required")
	}
	p, err := database.OpenPool(ctx, dsn)
	if err != nil {
		panic(err)
	}
	defer p.Close()
	var decisionID string
	if err := p.QueryRow(ctx, "SELECT decision_id::text FROM decision_evidence ORDER BY decision_id LIMIT 1").Scan(&decisionID); err != nil {
		panic(fmt.Errorf("find restored sealed decision: %w", err))
	}
	store, err := archive.NewS3StoreFromConfig(ctx, archive.ClientConfig{
		Endpoint: os.Getenv("RESTORE_S3_ENDPOINT"), Region: os.Getenv("RESTORE_S3_REGION"),
		AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Bucket: os.Getenv("RESTORE_S3_BUCKET"),
	})
	if err != nil {
		panic(err)
	}
	sealed, err := (evidence.Service{Pool: p, Archive: store}).Reconstruct(ctx, decisionID)
	if err != nil {
		panic(fmt.Errorf("reconstruct restored sealed decision: %w", err))
	}
	fmt.Println(sealed.SHA256)
}
