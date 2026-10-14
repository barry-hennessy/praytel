package args_test

import (
	"io"
	"testing"

	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
)

func TestParseTUI(t *testing.T) {
	cfg, err := args.Parse([]string{"--tui", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI {
		t.Fatalf("TUI was not enabled: %+v", cfg)
	}
}
