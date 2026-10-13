package args_test

import (
	"io"
	"reflect"
	"testing"

	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
	"github.com/barry-hennessy/test/sweet"
)

func TestParseRepeatableFocusExclude(t *testing.T) {
	factory := func(t *testing.T) struct{} {
		t.Helper()

		return struct{}{}
	}

	sweet.Run(t, "repeatable --focus and --exclude are collected", factory, func(t *testing.T, _ struct{}) {
		cfg, err := args.Parse([]string{"--focus", "orders", "--focus", "http", "--exclude", "health*", "--", "true"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Focus, []string{"orders", "http"}) {
			t.Fatalf("focus=%v", cfg.Focus)
		}
		if !reflect.DeepEqual(cfg.Exclude, []string{"health*"}) {
			t.Fatalf("exclude=%v", cfg.Exclude)
		}
	})
}

func TestParseTUI(t *testing.T) {
	cfg, err := args.Parse([]string{"--tui", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI {
		t.Fatalf("TUI was not enabled: %+v", cfg)
	}
}
