package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/buildinfo"
	collector "github.com/oplosy/atrisk/internal/collector"
	"github.com/oplosy/atrisk/internal/platform/database"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the collector build information")
	configPath := flag.String("config", "", "path to the local JSON collector schedule file")
	once := flag.Bool("once", false, "claim and process one due schedule, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("collector", version, runtime.Version()))
		return
	}

	if *configPath == "" {
		fail("-config is required")
	}
	config, err := collector.LoadConfig(*configPath)
	if err != nil {
		fail("invalid collector configuration")
	}
	databaseURL := os.Getenv(config.DatabaseURLEnv)
	if databaseURL == "" {
		fail("database URL environment variable is empty")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	p, err := database.OpenPool(ctx, databaseURL)
	if err != nil {
		fail("database connection failed")
	}
	defer p.Close()
	archiveStore, err := archive.NewS3StoreFromConfig(ctx, archive.ClientConfig{
		Endpoint: config.Archive.Endpoint, Region: config.Archive.Region, Bucket: config.Archive.Bucket,
		AccessKeyID: os.Getenv(config.Archive.AccessKeyEnv), SecretAccessKey: os.Getenv(config.Archive.SecretKeyEnv),
	})
	if err != nil {
		fail("archive connection failed")
	}
	scheduleStore := collector.Store{Pool: p}
	for _, input := range config.Schedules {
		schedule, normalizeErr := collector.NormalizeSchedule(input)
		if normalizeErr != nil {
			fail("invalid collector schedule")
		}
		if _, err := scheduleStore.UpsertSchedule(ctx, schedule); err != nil {
			fail("collector schedule registration failed")
		}
	}
	host, _ := os.Hostname()
	owner := fmt.Sprintf("%s/%d", host, os.Getpid())
	worker := collector.Worker{Store: scheduleStore, Owner: owner, Lease: 2 * time.Minute, Handler: (collector.Runner{Pool: p, Archive: archiveStore}).Run}
	if *once {
		if err := worker.RunOnce(ctx); err != nil {
			fail("collector run failed")
		}
		return
	}
	if err := worker.Run(ctx, time.Second); err != nil && ctx.Err() == nil {
		fail("collector worker stopped")
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "atlasrisk collector:", message)
	os.Exit(2)
}
