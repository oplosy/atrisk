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
	showVersion := flag.Bool("version", false, "print the collector build information")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("collector", version, runtime.Version()))
		return
	}

	fmt.Fprintln(os.Stdout, "atlasrisk collector toolchain skeleton")
}
