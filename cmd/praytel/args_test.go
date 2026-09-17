package main

import (
	"io"
	"reflect"
	"testing"

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
	cfg, err := parseArgs([]string{"--tui", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.tui {
		t.Fatal("--tui was not enabled")
	}
}
