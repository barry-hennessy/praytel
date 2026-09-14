package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	exit := flag.Int("exit", 0, "exit code")
	stdout := flag.String("stdout", "", "bytes to write to stdout")
	stderr := flag.String("stderr", "", "bytes to write to stderr")
	marker := flag.String("marker", "", "create this file on start")
	printOtel := flag.Bool("print-otel-env", false, "print OTEL_EXPORTER_OTLP_* assignments")
	flag.Parse()

	if *marker != "" {
		if err := os.WriteFile(*marker, []byte("ran\n"), 0o600); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	fmt.Fprint(os.Stdout, *stdout)
	fmt.Fprint(os.Stderr, *stderr)

	if *printOtel {
		var lines []string
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "OTEL_EXPORTER_OTLP_") {
				lines = append(lines, e)
			}
		}
		sort.Strings(lines)
		for _, line := range lines {
			fmt.Fprintln(os.Stdout, line)
		}
	}

	os.Exit(*exit)
}
