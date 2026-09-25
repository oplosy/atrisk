package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	apiportfolio "github.com/oplosy/atrisk/apps/api/handlers/portfolio"
	apiquality "github.com/oplosy/atrisk/apps/api/handlers/quality"
	"github.com/oplosy/atrisk/apps/api/handlers/timeline"
	applicationportfolio "github.com/oplosy/atrisk/internal/application/portfolio"
	appquality "github.com/oplosy/atrisk/internal/application/quality"
	application "github.com/oplosy/atrisk/internal/application/timeline"
	"github.com/oplosy/atrisk/internal/buildinfo"
	"github.com/oplosy/atrisk/internal/platform/database"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the API build information")
	listen := flag.String("listen", ":8080", "HTTP listen address")
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
	mux.Handle("/api/v1/accounts/", portfolioHandler)
	mux.Handle("/v1/accounts", portfolioHandler)
	mux.Handle("/v1/accounts/", portfolioHandler)
	mux.Handle("/api/v1/snapshots", portfolioHandler)
	mux.Handle("/api/v1/snapshots/", portfolioHandler)
	mux.Handle("/v1/snapshots", portfolioHandler)
	mux.Handle("/v1/snapshots/", portfolioHandler)
	mux.Handle("/api/v1/quality/evaluate", qualityHandler)
	mux.Handle("/v1/quality/evaluate", qualityHandler)
	mux.Handle("/", timeline.New(application.Service{Queries: queries}))
	handler := mux
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stdout, "atlasrisk api listening on %s\n", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
