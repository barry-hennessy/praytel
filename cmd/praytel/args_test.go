package main

import (
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/barry-hennessy/test/sweet"
)

func TestParseRepeatableFocusExclude(t *testing.T) {
	factory := func(t *testing.T) struct{} {
		t.Helper()

		return struct{}{}
	}

	sweet.Run(t, "repeatable --focus and --exclude are collected", factory, func(t *testing.T, _ struct{}) {
		cfg, err := parseArgs([]string{"--focus", "orders", "--focus", "http", "--exclude", "health*", "--", "true"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.focus, []string{"orders", "http"}) {
			t.Fatalf("focus=%v", cfg.focus)
		}
		if !reflect.DeepEqual(cfg.exclude, []string{"health*"}) {
			t.Fatalf("exclude=%v", cfg.exclude)
		}
	})
}

func TestParseTUI(t *testing.T) {
	cfg, err := parseArgs([]string{"--tui", "--query", `span{ span:status = error }`, "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.tui || cfg.query == "" {
		t.Fatalf("typed-query TUI was not enabled: %+v", cfg)
	}
}

func TestParseQueryOutput(t *testing.T) {
	cfg, err := parseArgs([]string{"--query", `span{ span:status = error }`, "--format", "ndjson", "--emit", "settled", "--settle-after", "10ms", "--limit", "7", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.query == "" || cfg.format != "ndjson" || cfg.emit != "settled" || cfg.settleAfter != 10*time.Millisecond || cfg.resultLimit != 7 {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestParseQueryShorthand(t *testing.T) {
	cfg, err := parseArgs([]string{"-q", `span{ span:status = error }`, "--", "true"}, io.Discard)
	if err != nil || cfg.query == "" {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
}
