package args_test

import (
	"io"
	"reflect"
	"testing"
	"time"

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
	cfg, err := args.Parse([]string{"--tui", "--query", `span{ span:status = error }`, "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI || cfg.Query == "" {
		t.Fatalf("typed-query TUI was not enabled: %+v", cfg)
	}
}

func TestParseQueryOutput(t *testing.T) {
	cfg, err := args.Parse([]string{"--query", `span{ span:status = error }`, "--format", "ndjson", "--emit", "settled", "--settle-after", "10ms", "--limit", "7", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Query == "" || cfg.Format != "ndjson" || cfg.Emit != "settled" || cfg.SettleAfter != 10*time.Millisecond || cfg.ResultLimit != 7 {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestParseQueryShorthand(t *testing.T) {
	cfg, err := args.Parse([]string{"-q", `span{ span:status = error }`, "--", "true"}, io.Discard)
	if err != nil || cfg.Query == "" {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
}
