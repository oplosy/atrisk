package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	apiimports "github.com/oplosy/atrisk/apps/api/handlers/imports"
	apijournal "github.com/oplosy/atrisk/apps/api/handlers/journal"
	apiportfolio "github.com/oplosy/atrisk/apps/api/handlers/portfolio"
	apiquality "github.com/oplosy/atrisk/apps/api/handlers/quality"
	apireconciliation "github.com/oplosy/atrisk/apps/api/handlers/reconciliation"
	apirisk "github.com/oplosy/atrisk/apps/api/handlers/risk"
	"github.com/oplosy/atrisk/apps/api/handlers/timeline"
	apivaluation "github.com/oplosy/atrisk/apps/api/handlers/valuation"
	applicationjournal "github.com/oplosy/atrisk/internal/application/journal"
	applicationportfolio "github.com/oplosy/atrisk/internal/application/portfolio"
	appquality "github.com/oplosy/atrisk/internal/application/quality"
	applicationreconciliation "github.com/oplosy/atrisk/internal/application/reconciliation"
	applicationrisk "github.com/oplosy/atrisk/internal/application/risk"
	application "github.com/oplosy/atrisk/internal/application/timeline"
	appvaluation "github.com/oplosy/atrisk/internal/application/valuation"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/buildinfo"
	applicationimports "github.com/oplosy/atrisk/internal/imports"
	"github.com/oplosy/atrisk/internal/platform/database"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(runMigrate(os.Args[2:], os.Getenv, os.Stdout, os.Stderr))
	}
	showVersion := flag.Bool("version", false, "print the API build information")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("api", version, runtime.Version()))
		return
	}

	dsn := os.Getenv("ATLASRISK_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "ATLASRISK_DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		log.Printf("open API database: %v", err)
		os.Exit(1)
	}
	defer pool.Close()
	queries := database.New(pool)
	portfolioHandler := apiportfolio.New(applicationportfolio.Service{Queries: queries, Beginner: pool})
	qualityHandler := apiquality.New(appquality.Service{Queries: queries})
	valuationHandler := apivaluation.New(appvaluation.Service{Pool: pool})
	reconciliationHandler := apireconciliation.New(applicationreconciliation.Service{Pool: pool})
	riskHandler := apirisk.New(applicationrisk.Service{Pool: pool})
	var importArchive archive.Store
	if endpoint := os.Getenv("ATLASRISK_S3_ENDPOINT"); endpoint != "" {
		store, storeErr := archive.NewS3StoreFromConfig(ctx, archive.ClientConfig{
			Endpoint: endpoint, Region: os.Getenv("ATLASRISK_S3_REGION"),
			AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			Bucket: os.Getenv("ATLASRISK_S3_BUCKET"),
		})
		if storeErr != nil {
			log.Printf("configure import archive: %v", storeErr)
		} else {
			importArchive = store
		}
	}
	importHandler := apiimports.New(applicationimports.Service{Pool: pool, Archive: importArchive})
	journalHandler := apijournal.New(applicationjournal.Service{Pool: pool, Archive: importArchive})
	mux := http.NewServeMux()
	mux.Handle("/api/v1/instruments", portfolioHandler)
	mux.Handle("/api/v1/instruments/", portfolioHandler)
	mux.Handle("/v1/instruments", portfolioHandler)
	mux.Handle("/v1/instruments/", portfolioHandler)
	mux.Handle("/api/v1/portfolios", portfolioHandler)
	mux.Handle("/api/v1/portfolios/", portfolioHandler)
	mux.Handle("/v1/portfolios", portfolioHandler)
	mux.Handle("/v1/portfolios/", portfolioHandler)
	mux.Handle("/api/v1/accounts", portfolioHandler)
	mux.Handle("/api/v1/accounts/", dispatchPortfolioReconciliation(portfolioHandler, reconciliationHandler))
	mux.Handle("/v1/accounts", portfolioHandler)
	mux.Handle("/v1/accounts/", dispatchPortfolioReconciliation(portfolioHandler, reconciliationHandler))
	mux.Handle("/api/v1/snapshots", portfolioHandler)
	mux.Handle("/api/v1/snapshots/", portfolioHandler)
	mux.Handle("/v1/snapshots", portfolioHandler)
	mux.Handle("/v1/snapshots/", portfolioHandler)
	mux.Handle("/api/v1/imports/", importHandler)
	mux.Handle("/v1/imports/", importHandler)
	mux.Handle("/api/v1/quality/evaluate", qualityHandler)
	mux.Handle("/v1/quality/evaluate", qualityHandler)
	mux.Handle("/api/v1/valuations", valuationHandler)
	mux.Handle("/api/v1/valuations/", dispatchValuationReconciliation(valuationHandler, reconciliationHandler))
	mux.Handle("/v1/valuations", valuationHandler)
	mux.Handle("/v1/valuations/", dispatchValuationReconciliation(valuationHandler, reconciliationHandler))
	mux.Handle("/api/v1/reconciliations/", reconciliationHandler)
	mux.Handle("/v1/reconciliations/", reconciliationHandler)
	mux.Handle("/api/v1/risk/runs", riskHandler)
	mux.Handle("/api/v1/risk/runs/", riskHandler)
	mux.Handle("/v1/risk/runs", riskHandler)
	mux.Handle("/v1/risk/runs/", riskHandler)
	mux.Handle("/api/v1/decisions", journalHandler)
	mux.Handle("/api/v1/decisions/", journalHandler)
	mux.Handle("/v1/decisions", journalHandler)
	mux.Handle("/v1/decisions/", journalHandler)
	mux.Handle("/", timeline.New(application.Service{Queries: queries}))
	handler := mux
	server := newAPIServer(*listen, handler)
	fmt.Fprintf(os.Stdout, "atlasrisk api listening on %s\n", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

const (
	apiReadHeaderTimeout = 5 * time.Second
	apiReadTimeout       = 30 * time.Second
	apiWriteTimeout      = 30 * time.Second
	apiIdleTimeout       = 60 * time.Second
)

func newAPIServer(addr string, handler http.Handler) *http.Server {
	return newAPIServerWithTimeouts(addr, handler, apiReadHeaderTimeout, apiReadTimeout, apiWriteTimeout, apiIdleTimeout)
}

func newAPIServerWithTimeouts(addr string, handler http.Handler, readHeaderTimeout, readTimeout, writeTimeout, idleTimeout time.Duration) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// runMigrate applies the forward-only migrations to ATLASRISK_DATABASE_URL and
// returns the process exit code. Release installations run it before starting
// a new API version; the image ships the migrations under db/migrations.
func runMigrate(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", "db/migrations", "directory containing the Goose SQL migrations")
	timeout := flags.Duration("timeout", 10*time.Minute, "maximum time for the whole migration run")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	dsn := getenv("ATLASRISK_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(stderr, "ATLASRISK_DATABASE_URL is required")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := database.ApplyMigrations(ctx, dsn, *dir); err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "atlasrisk migrations applied")
	return 0
}

func dispatchPortfolioReconciliation(primary, reconciliation http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.Trim(r.URL.Path, "/"), "/reconciliation-tolerances") {
			reconciliation.ServeHTTP(w, r)
			return
		}
		primary.ServeHTTP(w, r)
	})
}

func dispatchValuationReconciliation(primary, reconciliation http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.Trim(r.URL.Path, "/"), "/reconciliations") {
			reconciliation.ServeHTTP(w, r)
			return
		}
		primary.ServeHTTP(w, r)
	})
}
