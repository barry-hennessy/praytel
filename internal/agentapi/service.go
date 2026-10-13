package agentapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/barry-hennessy/praytel/internal/agentapi/internal/telemetry"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/session"
)

// SchemaVersion is the machine-contract version returned by all operations.
const SchemaVersion = 1

// Service implements agent operations over a session manager.
type Service struct{ sessions *session.Manager }

// New constructs a service backed by sessions.
func New(sessions *session.Manager) *Service { return &Service{sessions: sessions} }

// StartInput starts one command. Environment contains explicit non-secret
// additions; the inherited environment is never returned.
type StartInput struct {
	Command          []string          `json:"command" jsonschema:"command and arguments to execute"`
	WorkingDirectory string            `json:"working_directory,omitempty" jsonschema:"allowed directory relative to the MCP server root"`
	Environment      map[string]string `json:"environment,omitempty" jsonschema:"explicit non-secret environment additions"`
	Query            string            `json:"query" jsonschema:"Praytel trace and log query"`
	SettleMS         int64             `json:"settle_ms,omitempty" jsonschema:"quiet milliseconds before a closed trace is settled"`
	DrainIdleMS      int64             `json:"drain_idle_ms,omitempty" jsonschema:"post-exit idle drain milliseconds"`
	DrainTimeoutMS   int64             `json:"drain_timeout_ms,omitempty" jsonschema:"post-exit hard drain cap milliseconds"`
	MaxSpans         int               `json:"max_spans,omitempty"`
	MaxLogLines      int               `json:"max_log_lines,omitempty"`
}

// StartOutput identifies an asynchronously running session.
type StartOutput struct {
	SchemaVersion  int           `json:"schema_version"`
	RunID          string        `json:"run_id"`
	RunState       session.State `json:"run_state"`
	NextCursor     uint64        `json:"next_cursor"`
	CanonicalQuery string        `json:"canonical_query"`
}

// Start validates input and starts its command.
func (s *Service) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	for name, n := range map[string]int64{"settle_ms": in.SettleMS, "drain_idle_ms": in.DrainIdleMS, "drain_timeout_ms": in.DrainTimeoutMS} {
		if n < 0 {
			return StartOutput{}, fmt.Errorf("%s must not be negative", name)
		}
	}
	id, err := s.sessions.Start(ctx, session.Config{
		Command: in.Command, Dir: in.WorkingDirectory, Env: in.Environment, Query: in.Query,
		SettleAfter: time.Duration(in.SettleMS) * time.Millisecond,
		DrainIdle:   time.Duration(in.DrainIdleMS) * time.Millisecond, DrainTimeout: time.Duration(in.DrainTimeoutMS) * time.Millisecond,
		MaxSpans: in.MaxSpans, MaxLogLines: in.MaxLogLines,
	})
	if err != nil {
		return StartOutput{}, err
	}
	canonical, err := s.sessions.Query(id)
	if err != nil {
		return StartOutput{}, err
	}
	return StartOutput{SchemaVersion: SchemaVersion, RunID: id, RunState: session.StateStarting, CanonicalQuery: canonical}, nil
}

// WaitInput waits from an idempotent journal cursor.
type WaitInput struct {
	RunID     string `json:"run_id"`
	Cursor    uint64 `json:"cursor"`
	TimeoutMS int64  `json:"timeout_ms,omitempty" jsonschema:"bounded wait in milliseconds; defaults to 30000"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum matching revisions returned; defaults to 20"`
}

// Match is compact evidence for one matching trace revision.
type Match struct {
	TraceID           string                        `json:"trace_id"`
	CanonicalQuery    string                        `json:"canonical_query"`
	Revision          uint64                        `json:"revision"`
	State             projection.State              `json:"state"`
	MatchedAt         time.Time                     `json:"matched_at"`
	Evidence          projection.Evidence           `json:"evidence"`
	IncompleteReasons []projection.IncompleteReason `json:"incomplete_reasons,omitempty"`
}

// WaitOutput is a bounded page of matching revisions and current run state.
type WaitOutput struct {
	SchemaVersion     int                           `json:"schema_version"`
	RunID             string                        `json:"run_id"`
	RunState          session.State                 `json:"run_state"`
	NextCursor        uint64                        `json:"next_cursor"`
	TimedOut          bool                          `json:"timed_out"`
	Child             session.ChildResult           `json:"child"`
	Returned          int                           `json:"returned"`
	Matched           int                           `json:"matched"`
	Truncated         bool                          `json:"truncated"`
	IncompleteReasons []projection.IncompleteReason `json:"incomplete_reasons,omitempty"`
	Items             []Match                       `json:"items"`
}

// Wait long-polls for matches or a terminal session. Timeout is a successful,
// explicitly marked result so protocol clients need not interpret tool errors.
func (s *Service) Wait(ctx context.Context, in WaitInput) (WaitOutput, error) {
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 30_000
	}
	if in.TimeoutMS < 1 || in.TimeoutMS > 300_000 {
		return WaitOutput{}, errors.New("timeout_ms must be between 1 and 300000")
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutMS)*time.Millisecond)
	defer cancel()
	r, err := s.sessions.Wait(waitCtx, in.RunID, in.Cursor, in.Limit)
	if errors.Is(err, context.DeadlineExceeded) {
		state, child, statusErr := s.sessions.Status(in.RunID)
		if statusErr != nil {
			return WaitOutput{}, statusErr
		}
		return WaitOutput{SchemaVersion: SchemaVersion, RunID: in.RunID, RunState: state, NextCursor: in.Cursor, TimedOut: true, Child: child, Items: []Match{}}, nil
	}
	if err != nil {
		return WaitOutput{}, err
	}
	canonical, err := s.sessions.Query(in.RunID)
	if err != nil {
		return WaitOutput{}, err
	}
	items := make([]Match, len(r.Records))
	for i, item := range r.Records {
		items[i] = Match{item.TraceID, canonical, item.Revision, item.State, item.MatchedAt, item.Evidence, item.IncompleteReasons}
	}
	return WaitOutput{SchemaVersion, in.RunID, r.State, r.NextCursor, false, r.Child, r.Returned, r.Matched, r.Truncated, r.IncompleteReasons, items}, nil
}

// TopologyInput requests a compact trace skeleton.
type TopologyInput struct {
	RunID   string `json:"run_id"`
	TraceID string `json:"trace_id"`
	Limit   int    `json:"limit,omitempty"`
}

// TopologyOutput returns depth-first spans without attributes or log bodies.
type TopologyOutput struct {
	SchemaVersion int                       `json:"schema_version"`
	RunID         string                    `json:"run_id"`
	TraceID       string                    `json:"trace_id"`
	Returned      int                       `json:"returned"`
	Matched       int                       `json:"matched"`
	Truncated     bool                      `json:"truncated"`
	Spans         []projection.TopologySpan `json:"spans"`
}

// Topology returns a bounded structural view of a trace.
func (s *Service) Topology(_ context.Context, in TopologyInput) (TopologyOutput, error) {
	if in.Limit == 0 {
		in.Limit = 100
	}
	t, err := s.sessions.TraceTopology(in.RunID, in.TraceID, in.Limit)
	if err != nil {
		return TopologyOutput{}, err
	}
	return TopologyOutput{SchemaVersion, in.RunID, in.TraceID, t.Page.Returned, t.Page.Matched, t.Page.Truncated, t.Spans}, nil
}

// DetailsInput requests rich details for explicit span IDs.
type DetailsInput struct {
	RunID   string   `json:"run_id"`
	TraceID string   `json:"trace_id"`
	SpanIDs []string `json:"span_ids" jsonschema:"explicit suspicious span IDs; maximum 20"`
}

// Attribute is an ordered typed telemetry attribute.
type Attribute = telemetry.Attribute

// Value is a JSON-safe typed telemetry value.
type Value = telemetry.Value

// Scope identifies the instrumentation library that emitted a record.
type Scope = telemetry.Scope

// SpanEvent is one timestamped event recorded by a span.
type SpanEvent = telemetry.SpanEvent

// SpanLink identifies another span context related to a span.
type SpanLink = telemetry.SpanLink

// SpanDetails is rich detail for one explicitly requested span.
type SpanDetails struct {
	ID            string      `json:"span_id"`
	ParentID      string      `json:"parent_span_id,omitempty"`
	Name          string      `json:"name"`
	Kind          string      `json:"kind"`
	Status        string      `json:"status"`
	StatusMessage string      `json:"status_message,omitempty"`
	DurationNS    int64       `json:"duration_ns"`
	Closed        bool        `json:"closed"`
	Synthetic     bool        `json:"synthetic"`
	Start         time.Time   `json:"start"`
	End           time.Time   `json:"end"`
	Resource      []Attribute `json:"resource,omitempty"`
	Scope         Scope       `json:"scope"`
	Attributes    []Attribute `json:"attributes,omitempty"`
	Events        []SpanEvent `json:"events,omitempty"`
	Links         []SpanLink  `json:"links,omitempty"`
}

// DetailsOutput is a bounded rich span response.
type DetailsOutput struct {
	SchemaVersion int           `json:"schema_version"`
	RunID         string        `json:"run_id"`
	TraceID       string        `json:"trace_id"`
	Returned      int           `json:"returned"`
	Matched       int           `json:"matched"`
	Truncated     bool          `json:"truncated"`
	Spans         []SpanDetails `json:"spans"`
}

// Details returns full fields only for explicitly requested span IDs.
func (s *Service) Details(_ context.Context, in DetailsInput) (DetailsOutput, error) {
	if len(in.SpanIDs) == 0 || len(in.SpanIDs) > 20 {
		return DetailsOutput{}, errors.New("span_ids must contain between 1 and 20 IDs")
	}
	items, page, err := s.sessions.SpanDetails(in.RunID, in.TraceID, in.SpanIDs, 20)
	if err != nil {
		return DetailsOutput{}, err
	}
	out := make([]SpanDetails, len(items))
	for i, item := range items {
		out[i] = SpanDetails{
			ID: item.ID, ParentID: item.ParentID, Name: item.Name, Kind: item.Kind,
			Status: item.Status, StatusMessage: item.StatusMessage, DurationNS: item.Duration,
			Closed: item.Closed, Synthetic: item.Synthetic, Start: item.Start, End: item.End,
			Resource: telemetry.ToAttributes(item.Resource.Attributes), Scope: telemetry.ToScope(item.Scope),
			Attributes: telemetry.ToAttributes(item.Attributes), Events: telemetry.ToEvents(item.Events), Links: telemetry.ToLinks(item.Links),
		}
	}
	return DetailsOutput{SchemaVersion, in.RunID, in.TraceID, page.Returned, page.Matched, page.Truncated, out}, nil
}

// LogsInput requests explicit joined log IDs.
type LogsInput struct {
	RunID   string   `json:"run_id"`
	TraceID string   `json:"trace_id"`
	LogIDs  []string `json:"log_ids" jsonschema:"explicit matching log IDs; maximum 50"`
}

// LogDetails is rich detail for one explicitly requested log.
type LogDetails struct {
	ID                string      `json:"log_id"`
	TraceID           string      `json:"trace_id"`
	SpanID            string      `json:"span_id,omitempty"`
	Body              string      `json:"body"`
	Source            string      `json:"source"`
	SeverityText      string      `json:"severity_text,omitempty"`
	Timestamp         time.Time   `json:"timestamp,omitempty"`
	ObservedTimestamp time.Time   `json:"observed_timestamp,omitempty"`
	SeverityNumber    int32       `json:"severity_number"`
	BodyValue         Value       `json:"body_value"`
	Resource          []Attribute `json:"resource,omitempty"`
	Scope             Scope       `json:"scope"`
	Attributes        []Attribute `json:"attributes,omitempty"`
}

// LogsOutput is a bounded rich log response.
type LogsOutput struct {
	SchemaVersion int          `json:"schema_version"`
	RunID         string       `json:"run_id"`
	TraceID       string       `json:"trace_id"`
	Returned      int          `json:"returned"`
	Matched       int          `json:"matched"`
	Truncated     bool         `json:"truncated"`
	Logs          []LogDetails `json:"logs"`
}

// Logs returns full fields only for explicitly requested log IDs.
func (s *Service) Logs(_ context.Context, in LogsInput) (LogsOutput, error) {
	if len(in.LogIDs) == 0 || len(in.LogIDs) > 50 {
		return LogsOutput{}, errors.New("log_ids must contain between 1 and 50 IDs")
	}
	items, page, err := s.sessions.Logs(in.RunID, in.TraceID, in.LogIDs, 50)
	if err != nil {
		return LogsOutput{}, err
	}
	out := make([]LogDetails, len(items))
	for i, item := range items {
		out[i] = LogDetails{
			ID: item.ID, TraceID: item.TraceID, SpanID: item.SpanID, Body: item.Body,
			Source: string(item.Source), SeverityText: item.SeverityText, Timestamp: item.Timestamp,
			ObservedTimestamp: item.ObservedTimestamp, SeverityNumber: item.SeverityNumber,
			BodyValue: telemetry.ToValue(item.BodyValue), Resource: telemetry.ToAttributes(item.Resource.Attributes),
			Scope: telemetry.ToScope(item.Scope), Attributes: telemetry.ToAttributes(item.Attributes),
		}
	}
	return LogsOutput{SchemaVersion, in.RunID, in.TraceID, page.Returned, page.Matched, page.Truncated, out}, nil
}

// StopInput identifies a run to stop and drain.
type StopInput struct {
	RunID string `json:"run_id"`
}

// StopOutput reports the terminal child outcome.
type StopOutput struct {
	SchemaVersion int                 `json:"schema_version"`
	RunID         string              `json:"run_id"`
	RunState      session.State       `json:"run_state"`
	Child         session.ChildResult `json:"child"`
}

// Stop sends SIGTERM and waits for the configured drain.
func (s *Service) Stop(ctx context.Context, in StopInput) (StopOutput, error) {
	if err := s.sessions.Stop(ctx, in.RunID); err != nil {
		return StopOutput{}, err
	}
	state, child, err := s.sessions.Status(in.RunID)
	return StopOutput{SchemaVersion, in.RunID, state, child}, err
}

// Instructions is the concise agent playbook served by the CLI documentation
// command and suitable for inclusion in an agent skill.
const Instructions = `Use Praytel progressively:
1. start_run with a focused query and bounded limits.
2. wait_for_matches until items arrive, the run is terminal, or timed_out is true.
3. inspect trace topology before requesting span or log details.
4. request details only for evidence IDs.
5. stop long-running runs when verification is complete.

Do not claim that telemetry is absent unless run_state is exited or failed,
truncated is false, and incomplete_reasons is empty.`
