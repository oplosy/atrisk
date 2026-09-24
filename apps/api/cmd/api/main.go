package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/oplosy/atrisk/internal/buildinfo"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the API build information")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("api", version, runtime.Version()))
		return
	}

	fmt.Fprintln(os.Stdout, "atlasrisk api toolchain skeleton")
}
