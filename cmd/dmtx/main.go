package main

import (
	"os"
	"runtime/debug"

	"github.com/johndauphine/dmtx/internal/api"
	"github.com/johndauphine/dmtx/internal/app"
)

const defaultGCPercent = 200

// configureGCForTransfer favors throughput for DMTX's bounded, streaming
// migration workload. The transfer runtime accounts for retained page memory,
// so collecting at every one-heap-growth interval spends CPU without reducing
// the admitted working set. Operators that set GOGC retain complete control.
func configureGCForTransfer() {
	if _, configured := os.LookupEnv("GOGC"); configured {
		return
	}
	debug.SetGCPercent(defaultGCPercent)
}

func main() {
	configureGCForTransfer()
	// serve is intercepted here rather than routed through app.Execute. It is
	// not a migration command that several surfaces share - it is what creates
	// a surface - and internal/api consumes internal/app, so routing it through
	// the seam would make app import its own surface.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve", "webui", "gui":
			os.Exit(api.RunCommand(os.Args[2:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
}
