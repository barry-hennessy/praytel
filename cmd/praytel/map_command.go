package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/barry-hennessy/praytel/internal/archmap"
	"github.com/charmbracelet/lipgloss"
)

const mapUsage = "usage: praytel map [--format text|mermaid] [OLD] NEW"

// runMapCommand draws one map, or the difference between two, and returns
// the exit status diff(1) would: 0 for no difference, 1 for a difference,
// 2 for trouble.
func runMapCommand(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("praytel map", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, mapUsage) }
	format := fs.String("format", "text", "output format: text or mermaid")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	out := archmap.Output(*format)
	if out != archmap.Text && out != archmap.Mermaid {
		fmt.Fprintf(stderr, "praytel map: invalid --format %q\n%s\n", *format, mapUsage)
		return 2
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, mapUsage)
		return 2
	}

	var maps []archmap.Fragments
	for _, path := range fs.Args() {
		frags, err := archmap.Load(path)
		if err != nil {
			fmt.Fprintf(stderr, "praytel map: %v\n", err)
			return 2
		}
		maps = append(maps, frags)
	}

	// Colour only when stdout is a terminal and NO_COLOR is unset.
	r := lipgloss.NewRenderer(stdout)

	if len(maps) == 1 {
		if err := archmap.RenderMap(stdout, r, out, maps[0].Union()); err != nil {
			fmt.Fprintf(stderr, "praytel map: %v\n", err)
			return 2
		}
		return 0
	}

	d := archmap.Compare(maps[0], maps[1])
	if err := archmap.RenderDiff(stdout, r, out, d); err != nil {
		fmt.Fprintf(stderr, "praytel map: %v\n", err)
		return 2
	}
	if d.Changed() {
		return 1
	}
	return 0
}
