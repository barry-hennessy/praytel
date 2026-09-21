package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/capture"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/ingest"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
	"github.com/barry-hennessy/praytel/internal/receiver"
)

const defaultJournalLimit = 1000

// State describes a session lifecycle state.
type State string

const (
	// StateStarting means the session is reserved but the child has not started.
	StateStarting State = "starting"
	// StateRunning means the child is running.
	StateRunning State = "running"
	// StateStopping means termination was requested and drain is in progress.
	StateStopping State = "stopping"
	// StateExited means the child exited and telemetry drain completed.
	StateExited State = "exited"
	// StateFailed means collection or child startup failed.
	StateFailed State = "failed"
)

// Terminal reports whether no more journal records can be produced.
func (s State) Terminal() bool { return s == StateExited || s == StateFailed }

// Config describes one command run. Dir, when set, is relative to the manager
// root. Env contains explicit additions only; sensitive variable names are rejected.
type Config struct {
	Command      []string
	Dir          string
	Env          map[string]string
	Query        string
	SettleAfter  time.Duration
	DrainIdle    time.Duration
	DrainTimeout time.Duration
	MaxSpans     int
	MaxLogLines  int
}

// ManagerOptions bounds resources shared by a Manager. Zero values select defaults.
type ManagerOptions struct {
	MaxActive      int
	MaxSessions    int
	JournalLimit   int
	MaxDetailLimit int
}

// ChildResult is the terminal process outcome. Error is lifecycle diagnostic text,
// never child output.
type ChildResult struct {
	Started  bool   `json:"started"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// WaitResult is a replayable page of matching projection results.
type WaitResult struct {
	NextCursor        uint64
	Records           []projection.Result
	State             State
	Child             ChildResult
	Returned          int
	Matched           int
	Truncated         bool
	IncompleteReasons []projection.IncompleteReason
}

// ErrNotFound indicates an unknown session ID.
var ErrNotFound = errors.New("session: not found")

// ErrCursorExpired indicates that a cursor predates the retained journal.
var ErrCursorExpired = errors.New("session: cursor expired")

// ValidationError reports invalid configuration before any side effect occurs.
type ValidationError struct{ Field, Problem string }

// Error implements the error interface.
func (e *ValidationError) Error() string { return "session: invalid " + e.Field + ": " + e.Problem }

// CursorExpiredError reports the oldest cursor that can still be retried.
type CursorExpiredError struct{ Cursor, Oldest uint64 }

// Error implements the error interface.
func (e *CursorExpiredError) Error() string {
	return fmt.Sprintf("%v: cursor %d; oldest %d", ErrCursorExpired, e.Cursor, e.Oldest)
}

// Unwrap returns [ErrCursorExpired] for errors.Is.
func (e *CursorExpiredError) Unwrap() error { return ErrCursorExpired }

// Manager owns all sessions rooted beneath one absolute working directory.
type Manager struct {
	root     string
	lt       observability.LoggingTracer
	opts     ManagerOptions
	mu       sync.RWMutex
	sessions map[string]*run
	closed   bool
	active   int
	seq      atomic.Uint64
}

type entry struct {
	cursor uint64
	result projection.Result
}
type run struct {
	id       string
	sequence uint64
	cfg      Config
	q        *query.Query
	p        *projection.Projection
	dir      string
	mu       sync.Mutex
	changed  chan struct{}
	state    State
	child    ChildResult
	journal  []entry
	next     uint64
	cmd      *exec.Cmd
	stopOnce sync.Once
	done     chan struct{}
	m        *Manager
}

func (m *Manager) removeOldestTerminal() bool {
	var oldest *run
	for _, candidate := range m.sessions {
		candidate.mu.Lock()
		terminal := candidate.state.Terminal()
		candidate.mu.Unlock()
		if terminal && (oldest == nil || candidate.sequence < oldest.sequence) {
			oldest = candidate
		}
	}
	if oldest == nil {
		return false
	}
	delete(m.sessions, oldest.id)
	return true
}

// NewManager constructs a manager. Root must be an existing absolute directory.
func NewManager(root string, lt observability.LoggingTracer, opts ManagerOptions) (*Manager, error) {
	if !filepath.IsAbs(root) {
		return nil, &ValidationError{"root", "must be absolute"}
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return nil, &ValidationError{"root", err.Error()}
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return nil, &ValidationError{"root", "must be an existing directory"}
	}
	if opts.MaxActive <= 0 {
		opts.MaxActive = 8
	}
	if opts.MaxSessions <= 0 {
		opts.MaxSessions = 100
	}
	if opts.JournalLimit <= 0 {
		opts.JournalLimit = defaultJournalLimit
	}
	if opts.MaxDetailLimit <= 0 {
		opts.MaxDetailLimit = 1000
	}
	return &Manager{root: real, lt: lt.Name("session"), opts: opts, sessions: map[string]*run{}}, nil
}

func (m *Manager) validate(c Config) (*query.Query, string, error) {
	if len(c.Command) == 0 || strings.TrimSpace(c.Command[0]) == "" {
		return nil, "", &ValidationError{"command", "must not be empty"}
	}
	for _, arg := range c.Command {
		if strings.ContainsRune(arg, 0) {
			return nil, "", &ValidationError{"command", "contains NUL"}
		}
	}
	if strings.TrimSpace(c.Query) == "" {
		return nil, "", &ValidationError{"query", "must not be empty"}
	}
	q, err := query.Check(c.Query)
	if err != nil {
		return nil, "", &ValidationError{"query", err.Error()}
	}
	for k, v := range c.Env {
		u := strings.ToUpper(k)
		if k == "" || strings.ContainsAny(k, "=\x00") {
			return nil, "", &ValidationError{"environment", "invalid key"}
		}
		if strings.ContainsRune(v, 0) {
			return nil, "", &ValidationError{"environment", "value contains NUL"}
		}
		for _, word := range []string{"TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL"} {
			if strings.Contains(u, word) {
				return nil, "", &ValidationError{"environment", "sensitive key " + k}
			}
		}
	}
	d := m.root
	if c.Dir != "" {
		if filepath.IsAbs(c.Dir) {
			return nil, "", &ValidationError{"dir", "must be relative"}
		}
		candidate := filepath.Join(m.root, c.Dir)
		real, e := filepath.EvalSymlinks(candidate)
		if e != nil {
			return nil, "", &ValidationError{"dir", e.Error()}
		}
		rel, e := filepath.Rel(m.root, real)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, "", &ValidationError{"dir", "escapes root"}
		}
		st, e := os.Stat(real)
		if e != nil || !st.IsDir() {
			return nil, "", &ValidationError{"dir", "must be a directory"}
		}
		d = real
	}
	if c.SettleAfter < 0 || c.DrainIdle < 0 || c.DrainTimeout < 0 {
		return nil, "", &ValidationError{"duration", "must not be negative"}
	}
	return q, d, nil
}

// Start validates config, reserves a session, and starts its child asynchronously.
func (m *Manager) Start(ctx context.Context, c Config) (string, error) {
	q, dir, err := m.validate(c)
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", errors.New("session: manager closed")
	}
	if m.active >= m.opts.MaxActive {
		return "", &ValidationError{"active sessions", "limit reached"}
	}
	for len(m.sessions) >= m.opts.MaxSessions && m.removeOldestTerminal() {
	}
	if len(m.sessions) >= m.opts.MaxSessions {
		return "", &ValidationError{"sessions", "limit reached"}
	}
	sequence := m.seq.Add(1)
	id := fmt.Sprintf("s-%d", sequence)
	r := &run{id: id, sequence: sequence, cfg: c, q: q, p: projection.New(), dir: dir, changed: make(chan struct{}), state: StateStarting, done: make(chan struct{}), m: m}
	m.sessions[id] = r
	m.active++
	go r.execute()
	return id, nil
}

func (r *run) notify()          { close(r.changed); r.changed = make(chan struct{}) }
func (r *run) setState(s State) { r.mu.Lock(); r.state = s; r.notify(); r.mu.Unlock() }
func (r *run) append(results []projection.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range results {
		r.next++
		r.journal = append(r.journal, entry{r.next, x})
	}
	if n := len(r.journal) - r.m.opts.JournalLimit; n > 0 {
		r.journal = append([]entry(nil), r.journal[n:]...)
	}
	if len(results) > 0 {
		r.notify()
	}
}

func childEnv(extra map[string]string, injected []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra)+len(injected))
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		_, replaced := extra[k]
		if !replaced && !strings.HasPrefix(strings.ToUpper(k), "OTEL_EXPORTER_OTLP_") {
			env = append(env, e)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return append(env, injected...)
}

func (r *run) execute() {
	defer func() { r.m.mu.Lock(); r.m.active--; r.m.mu.Unlock(); close(r.done) }()
	r.executePipeline()
}

func (r *run) executePipeline() {
	pipe, err := ingest.New(r.m.lt, correlator.Drain{Idle: r.cfg.DrainIdle, Timeout: r.cfg.DrainTimeout}, correlator.Limits{MaxSpans: r.cfg.MaxSpans, MaxLogLines: r.cfg.MaxLogLines}, receiver.Ports{})
	if err != nil {
		r.fail(false, 1, err)
		return
	}
	events, corr, rec := pipe.Events, pipe.Corr, pipe.Rec
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	serveDone := make(chan struct{})
	go func() { defer close(serveDone); _ = rec.Serve(serveCtx) }()
	corrDone := make(chan struct{})
	go func() { defer close(corrDone); _ = corr.Run(context.Background()) }()
	projectDone := make(chan struct{})
	go r.project(events, projectDone)

	outR, outW, err := os.Pipe()
	if err != nil {
		cancelServe()
		corr.Stop()
		<-corrDone
		<-projectDone
		<-serveDone
		r.fail(false, 1, err)
		return
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		cancelServe()
		corr.Stop()
		<-corrDone
		<-projectDone
		<-serveDone
		r.fail(false, 1, err)
		return
	}
	var capWG sync.WaitGroup
	capWG.Add(2)
	gate := capture.NewWarnGate(0)
	go func() {
		defer capWG.Done()
		defer outR.Close()
		_ = capture.Copy(context.Background(), r.m.lt, domain.SourceStdout, outR, corr.Logs(), gate)
	}()
	go func() {
		defer capWG.Done()
		defer errR.Close()
		_ = capture.Copy(context.Background(), r.m.lt, domain.SourceStderr, errR, corr.Logs(), gate)
	}()

	cmd := exec.Command(r.cfg.Command[0], r.cfg.Command[1:]...)
	cmd.Dir = r.dir
	cmd.Env = childEnv(r.cfg.Env, rec.ChildEnv())
	cmd.Stdout = outW
	cmd.Stderr = errW
	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()
	if err = cmd.Start(); err != nil {
		outW.Close()
		errW.Close()
		capWG.Wait()
		corr.Stop()
		<-corrDone
		<-projectDone
		rec.Drop()
		cancelServe()
		<-serveDone
		r.fail(false, 1, err)
		return
	}
	r.mu.Lock()
	r.child.Started = true
	stopping := r.state == StateStopping
	if !stopping {
		r.state = StateRunning
	}
	r.notify()
	r.mu.Unlock()
	if stopping {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	err = cmd.Wait()
	outW.Close()
	errW.Close()
	capWG.Wait()
	corr.Stop()
	<-corrDone
	<-projectDone
	rec.Drop()
	cancelServe()
	<-serveDone
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
		err = nil
	} else if err != nil {
		code = 1
	}
	r.mu.Lock()
	r.child.ExitCode = code
	if err != nil {
		r.child.Error = err.Error()
	}
	r.state = StateExited
	r.notify()
	r.mu.Unlock()
}

func (r *run) project(events <-chan domain.Event, done chan<- struct{}) {
	defer close(done)
	interval := 25 * time.Millisecond
	if r.cfg.SettleAfter > 0 && r.cfg.SettleAfter/2 < interval {
		interval = r.cfg.SettleAfter / 2
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case e, ok := <-events:
			if !ok {
				r.append(r.p.FinalResults(r.q, time.Now()))
				return
			}
			r.p.Observe(e, time.Now())
		case now := <-t.C:
			r.append(r.p.ReadySettled(r.q, now, r.cfg.SettleAfter))
		}
	}
}

func (r *run) fail(started bool, code int, err error) {
	r.mu.Lock()
	r.child = ChildResult{Started: started, ExitCode: code, Error: err.Error()}
	r.state = StateFailed
	r.notify()
	r.mu.Unlock()
}

func (m *Manager) get(id string) (*run, error) {
	m.mu.RLock()
	r := m.sessions[id]
	m.mu.RUnlock()
	if r == nil {
		return nil, ErrNotFound
	}
	return r, nil
}

// Wait blocks until journal records after cursor are available, the session is
// terminal, or ctx ends. Reusing a cursor returns the same retained records.
func (m *Manager) Wait(ctx context.Context, id string, cursor uint64, limit int) (WaitResult, error) {
	if limit <= 0 {
		return WaitResult{}, &ValidationError{"limit", "must be positive"}
	}
	r, err := m.get(id)
	if err != nil {
		return WaitResult{}, err
	}
	for {
		r.mu.Lock()
		oldest := uint64(0)
		if len(r.journal) > 0 {
			oldest = r.journal[0].cursor - 1
		}
		if cursor < oldest {
			r.mu.Unlock()
			return WaitResult{}, &CursorExpiredError{cursor, oldest}
		}
		var records []projection.Result
		next := cursor
		matched := 0
		reasons := map[projection.IncompleteReason]bool{}
		for _, e := range r.journal {
			if e.cursor > cursor {
				matched++
				if len(records) < limit {
					records = append(records, e.result)
					next = e.cursor
				}
				for _, x := range e.result.IncompleteReasons {
					reasons[x] = true
				}
			}
		}
		state, child, ch := r.state, r.child, r.changed
		if len(records) > 0 || state.Terminal() {
			if state == StateFailed {
				reasons[projection.IncompleteRunFailed] = true
			}
			rs := make([]projection.IncompleteReason, 0, len(reasons))
			for x := range reasons {
				rs = append(rs, x)
			}
			sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
			out := WaitResult{next, records, state, child, len(records), matched, matched > len(records), rs}
			r.mu.Unlock()
			return out, nil
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return WaitResult{}, ctx.Err()
		case <-ch:
		}
	}
}

// Status returns the current lifecycle and child outcome without waiting.
func (m *Manager) Status(id string) (State, ChildResult, error) {
	r, err := m.get(id)
	if err != nil {
		return "", ChildResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.child, nil
}

// Query returns the canonical checked query associated with a session.
func (m *Manager) Query(id string) (string, error) {
	r, err := m.get(id)
	if err != nil {
		return "", err
	}
	return r.q.String(), nil
}

// Stop sends SIGTERM to a running child and allows its configured drain to complete.
func (m *Manager) Stop(ctx context.Context, id string) error {
	r, err := m.get(id)
	if err != nil {
		return err
	}
	r.stopOnce.Do(func() {
		r.mu.Lock()
		if !r.state.Terminal() {
			r.state = StateStopping
			r.notify()
		}
		cmd := r.cmd
		r.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops all children and waits for session cleanup.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	runs := make([]*run, 0, len(m.sessions))
	for _, r := range m.sessions {
		runs = append(runs, r)
	}
	m.mu.Unlock()
	for _, r := range runs {
		r.stopOnce.Do(func() {
			r.mu.Lock()
			if !r.state.Terminal() {
				r.state = StateStopping
				r.notify()
			}
			cmd := r.cmd
			r.mu.Unlock()
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		})
	}
	for _, r := range runs {
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			r.mu.Lock()
			if r.cmd != nil && r.cmd.Process != nil {
				_ = r.cmd.Process.Kill()
			}
			r.mu.Unlock()
			<-r.done
		}
	}
	return nil
}

func (m *Manager) capped(limit int) error {
	if limit <= 0 || limit > m.opts.MaxDetailLimit {
		return &ValidationError{"limit", fmt.Sprintf("must be between 1 and %d", m.opts.MaxDetailLimit)}
	}
	return nil
}

// TraceTopology delegates to the session projection with the manager detail cap.
func (m *Manager) TraceTopology(id, traceID string, limit int) (projection.Topology, error) {
	if e := m.capped(limit); e != nil {
		return projection.Topology{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return projection.Topology{}, e
	}
	return r.p.TraceTopology(traceID, limit)
}

// SpanDetails delegates to the session projection with the manager detail cap.
func (m *Manager) SpanDetails(id, traceID string, ids []string, limit int) ([]projection.SpanDetail, projection.Page, error) {
	if e := m.capped(limit); e != nil {
		return nil, projection.Page{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return nil, projection.Page{}, e
	}
	return r.p.SpanDetails(traceID, ids, limit)
}

// Logs delegates to the session projection with the manager detail cap.
func (m *Manager) Logs(id, traceID string, ids []string, limit int) ([]projection.LogDetail, projection.Page, error) {
	if e := m.capped(limit); e != nil {
		return nil, projection.Page{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return nil, projection.Page{}, e
	}
	return r.p.Logs(traceID, ids, limit)
}
