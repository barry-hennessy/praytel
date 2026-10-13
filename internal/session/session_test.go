package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/session"
)

func newManager(t *testing.T, opts session.ManagerOptions) *session.Manager {
	t.Helper()
	m, err := session.NewManager(t.TempDir(), observability.LoggingTracer{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func waitTerminal(t *testing.T, m *session.Manager, id string) session.WaitResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var all session.WaitResult
	cursor := uint64(0)
	for {
		r, err := m.Wait(ctx, id, cursor, 10)
		if err != nil {
			t.Fatal(err)
		}
		all.Records = append(all.Records, r.Records...)
		all.State, all.Child, all.NextCursor = r.State, r.Child, r.NextCursor
		all.IncompleteReasons = r.IncompleteReasons
		cursor = r.NextCursor
		if r.State.Terminal() {
			return all
		}
	}
}

func TestShortRunAndIdempotentCursor(t *testing.T) {
	m := newManager(t, session.ManagerOptions{})
	id, err := m.Start(t.Context(), session.Config{Command: []string{"sh", "-c", "printf 'find-me\\n'"}, Query: `log{ log:body *= "find-me" } | matches`})
	if err != nil {
		t.Fatal(err)
	}
	r := waitTerminal(t, m, id)
	if len(r.Records) != 1 || !r.Child.Started || r.Child.ExitCode != 0 {
		t.Fatalf("result=%+v", r)
	}
	again, err := m.Wait(t.Context(), id, 0, 10)
	if err != nil || len(again.Records) != 1 || again.Records[0].TraceID != r.Records[0].TraceID {
		t.Fatalf("retry=%+v err=%v", again, err)
	}
}

func TestStartFailureIsTerminal(t *testing.T) {
	m := newManager(t, session.ManagerOptions{})
	id, err := m.Start(t.Context(), session.Config{Command: []string{filepath.Join(t.TempDir(), "missing")}, Query: `span{}`})
	if err != nil {
		t.Fatal(err)
	}
	r := waitTerminal(t, m, id)
	if r.State != session.StateFailed || r.Child.Started || r.Child.Error == "" ||
		len(r.IncompleteReasons) != 1 || r.IncompleteReasons[0] != "run_failed" {
		t.Fatalf("result=%+v", r)
	}
}

func TestStopLongRun(t *testing.T) {
	m := newManager(t, session.ManagerOptions{})
	id, err := m.Start(t.Context(), session.Config{Command: []string{"sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"}, Query: `log{}`})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, _, statusErr := m.Status(id)
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		if state == session.StateRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not start")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := m.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	state, _, _ := m.Status(id)
	if !state.Terminal() {
		t.Fatalf("state=%s", state)
	}
}

func TestValidationBeforeSideEffects(t *testing.T) {
	root := t.TempDir()
	m, err := session.NewManager(root, observability.LoggingTracer{}, session.ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, cfg := range []session.Config{
		{Command: []string{"true"}, Query: `span{}`, Env: map[string]string{"API_TOKEN": "secret"}},
		{Command: []string{"true"}, Query: `span{}`, Dir: "../"},
		{Command: []string{"true"}, Query: `span{ broken = }`},
	} {
		if _, err := m.Start(t.Context(), cfg); err == nil {
			t.Fatalf("accepted %#v", cfg)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(t.Context(), session.Config{Command: []string{"true"}, Query: `span{}`, Dir: "escape"}); err == nil {
		t.Fatal("accepted escaping symlink")
	}
}

func TestWaitTimeoutAndUnknown(t *testing.T) {
	m := newManager(t, session.ManagerOptions{})
	if _, err := m.Wait(t.Context(), "missing", 0, 1); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	id, err := m.Start(t.Context(), session.Config{Command: []string{"sh", "-c", "sleep 1"}, Query: `span{}`})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := m.Wait(ctx, id, 0, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestStartingRunEvictsOldestTerminalSessionAtCapacity(t *testing.T) {
	m := newManager(t, session.ManagerOptions{MaxSessions: 1})
	first, err := m.Start(t.Context(), session.Config{Command: []string{"true"}, Query: `span{}`})
	if err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, m, first)
	second, err := m.Start(t.Context(), session.Config{Command: []string{"true"}, Query: `span{}`})
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("reused session ID %q", second)
	}
	if _, _, err := m.Status(first); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("old terminal session was retained: %v", err)
	}
}
