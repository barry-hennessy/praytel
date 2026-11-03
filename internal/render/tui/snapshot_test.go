package tui

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
)

// snapNode is one span of a fixture, with parent an index into the fixture
// (-1 for a root).
type snapNode struct {
	parent    int
	component string
	name      string
	status    domain.SpanStatus
	attrs     map[string]string
}

// fixtureEvents returns the events for a fixture with random ids and
// timestamps, in a random order, ending in DrainComplete.
func fixtureEvents(nodes []snapNode, rng *rand.Rand) []domain.Event {
	id := func() string { return fmt.Sprintf("%016x", rng.Uint64()) }
	trace := map[int]string{}
	ids := make([]string, len(nodes))
	for i := range nodes {
		ids[i] = id()
	}
	var evs []domain.Event
	for i, n := range nodes {
		// The trace is that of the span's root.
		r := i
		for nodes[r].parent >= 0 {
			r = nodes[r].parent
		}
		if trace[r] == "" {
			trace[r] = id()
		}
		var attrs []domain.Attr
		for k, v := range n.attrs {
			attrs = append(attrs, domain.Attr{Key: k, Value: domain.StringValue(v)})
		}
		rng.Shuffle(len(attrs), func(a, b int) { attrs[a], attrs[b] = attrs[b], attrs[a] })
		parent := ""
		if n.parent >= 0 {
			parent = ids[n.parent]
		}
		start := time.Unix(int64(rng.Intn(1000)), 0)
		evs = append(evs,
			domain.SpanUpserted{
				TraceID: trace[r], SpanID: ids[i], ParentSpanID: parent,
				Name: n.name, Component: n.component, Status: n.status,
				StartTime: start, EndTime: start.Add(time.Duration(rng.Intn(1e6))),
				Attributes: attrs,
			},
			domain.SpanClosed{TraceID: trace[r], SpanID: ids[i]},
		)
		if parent != "" {
			evs = append(evs, domain.SpanParentLinked{TraceID: trace[r], SpanID: ids[i], ParentSpanID: parent})
		}
	}
	rng.Shuffle(len(evs), func(a, b int) { evs[a], evs[b] = evs[b], evs[a] })
	return append(evs, domain.DrainComplete{})
}

func snap(evs []domain.Event, opts SnapshotOptions) SnapshotResult {
	ch := make(chan domain.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return Snapshot(ch, opts)
}

var allViews = SnapshotOptions{Tree: true, Graph: true, Attrs: attrspec.All()}

// A suite under test: two tests (roots carrying test.name), each with
// sibling calls, two of them identical, and a call into a shared component.
var suiteFixture = []snapNode{
	{parent: -1, component: "test", name: "TestA", attrs: map[string]string{"test.name": "TestA"}},
	{parent: 0, component: "api", name: "GET /users", attrs: map[string]string{"http.method": "GET", "http.url": "x"}},
	{parent: 1, component: "db", name: "query", attrs: map[string]string{"db.statement": "select 1"}},
	{parent: 0, component: "api", name: "GET /users", attrs: map[string]string{"http.method": "GET", "http.url": "x"}},
	{parent: 3, component: "db", name: "query", attrs: map[string]string{"db.statement": "select 1"}},
	{parent: 0, component: "api", name: "POST /users", status: domain.SpanStatusError},
	{parent: -1, component: "test", name: "TestB", attrs: map[string]string{"test.name": "TestB"}},
	{parent: 6, component: "api", name: "GET /users", attrs: map[string]string{"http.method": "GET", "http.url": "x"}},
	{parent: 7, component: "db", name: "query", attrs: map[string]string{"db.statement": "select 2"}},
	{parent: 8, component: "api", name: "audit"},
}

func TestSnapshotIgnoresArrivalOrderIdsAndTimes(t *testing.T) {
	want := snap(fixtureEvents(suiteFixture, rand.New(rand.NewSource(1))), allViews)
	if len(want.Problems) != 0 {
		t.Fatalf("fixture is incomplete: %v", want.Problems)
	}
	for seed := int64(2); seed < 60; seed++ {
		got := snap(fixtureEvents(suiteFixture, rand.New(rand.NewSource(seed))), allViews)
		if got.Text != want.Text {
			t.Fatalf("seed %d differs\n--- got\n%s--- want\n%s", seed, got.Text, want.Text)
		}
	}
}

func TestSnapshotOfRandomForestsIgnoresArrivalOrder(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		nodes := make([]snapNode, 1+rng.Intn(25))
		for i := range nodes {
			nodes[i] = snapNode{
				parent:    -1,
				component: []string{"a", "b", "c"}[rng.Intn(3)],
				name:      []string{"x", "y", "z"}[rng.Intn(3)],
				attrs:     map[string]string{"k": fmt.Sprint(rng.Intn(2))},
			}
			if i > 0 && rng.Intn(4) > 0 {
				nodes[i].parent = rng.Intn(i)
			}
		}
		want := snap(fixtureEvents(nodes, rng), allViews)
		for i := 0; i < 10; i++ {
			if got := snap(fixtureEvents(nodes, rng), allViews); got.Text != want.Text {
				t.Fatalf("forest %d differs\n--- got\n%s--- want\n%s", seed, got.Text, want.Text)
			}
		}
	}
}

func TestSnapshotRendersIdenticalSiblingsTwice(t *testing.T) {
	nodes := []snapNode{
		{parent: -1, component: "t", name: "root"},
		{parent: 0, component: "a", name: "call"},
		{parent: 0, component: "a", name: "call"},
	}
	got := snap(fixtureEvents(nodes, rand.New(rand.NewSource(3))), SnapshotOptions{Tree: true})
	if n := strings.Count(got.Text, "a.call"); n != 2 {
		t.Fatalf("want both siblings, got %d\n%s", n, got.Text)
	}
}

func TestSnapshotAttributesFollowTheSpec(t *testing.T) {
	spec := func(s string) attrspec.Spec {
		sp, err := attrspec.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	cases := []struct {
		name    string
		attrs   attrspec.Spec
		has     []string
		hasNot  []string
		ordered bool
	}{
		{"absent", attrspec.Spec{}, nil, []string{"http.method", "http.url", "db.statement"}, false},
		{"all", attrspec.All(), []string{"http.method", "http.url"}, nil, true},
		{"glob minus", spec("http.* -http.url"), []string{"http.method"}, []string{"http.url", "db.statement"}, false},
	}
	for _, c := range cases {
		out := snap(fixtureEvents(suiteFixture, rand.New(rand.NewSource(1))), SnapshotOptions{Tree: true, Attrs: c.attrs}).Text
		for _, s := range c.has {
			if !strings.Contains(out, s) {
				t.Errorf("%s: missing %q\n%s", c.name, s, out)
			}
		}
		for _, s := range c.hasNot {
			if strings.Contains(out, s) {
				t.Errorf("%s: unexpected %q\n%s", c.name, s, out)
			}
		}
		if c.ordered && strings.Index(out, "http.method") > strings.Index(out, "http.url") {
			t.Errorf("%s: attributes not sorted by key\n%s", c.name, out)
		}
	}
}

func TestSnapshotHasNoDurationsColourOrTrailingSpace(t *testing.T) {
	out := snap(fixtureEvents(suiteFixture, rand.New(rand.NewSource(1))), allViews).Text
	if strings.Contains(out, "\x1b") {
		t.Errorf("has escape codes:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("trailing space in %q", l)
		}
	}
	if strings.Contains(out, "µs") || strings.Contains(out, "ms") {
		t.Errorf("has a duration:\n%s", out)
	}
	if !strings.HasPrefix(out, "# praytel-snapshot v1\n## tree\n") || !strings.Contains(out, "\n## graph\n") {
		t.Errorf("bad framing:\n%s", out)
	}
}

func TestSnapshotGoldens(t *testing.T) {
	got := snap(fixtureEvents(suiteFixture, rand.New(rand.NewSource(1))), allViews)
	path := filepath.Join("testdata", "snapshot", "suite.txt")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got.Text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != string(want) {
		t.Fatalf("snapshot differs from %s\n--- got\n%s--- want\n%s", path, got.Text, want)
	}
}

func TestSnapshotGate(t *testing.T) {
	span := func(id, parent string) domain.SpanUpserted {
		return domain.SpanUpserted{TraceID: "t", SpanID: id, ParentSpanID: parent, Name: id, Component: "c"}
	}
	closed := func(id string) domain.SpanClosed { return domain.SpanClosed{TraceID: "t", SpanID: id} }
	ok := []domain.Event{span("a", ""), closed("a"), domain.DrainComplete{}}

	cases := []struct {
		name string
		evs  []domain.Event
		want string // substring of the problems; empty means none
	}{
		{"complete", ok, ""},
		{"no drain", ok[:2], "drain"},
		{"evicted", append([]domain.Event{domain.ForestEvicted{TraceID: "x"}}, ok...), "evicted"},
		{"shed", append([]domain.Event{domain.ForestShed{}}, ok...), "shed"},
		{"placeholder", append([]domain.Event{domain.LogReceived{
			ID: "1", DeclaredTraceID: "t", DeclaredSpanID: "ghost", Body: "x",
		}}, ok...), "never arrived"},
		{"unclosed", []domain.Event{span("a", ""), domain.DrainComplete{}}, "never ended"},
		{"missing parent", []domain.Event{
			span("a", "gone"), closed("a"), domain.SpanParentLinked{TraceID: "t", SpanID: "a", ParentSpanID: "gone"},
			domain.DrainComplete{},
		}, "parent that never arrived"},
		{"empty", []domain.Event{domain.DrainComplete{}}, "no spans"},
	}
	for _, c := range cases {
		res := snap(c.evs, allViews)
		joined := strings.Join(res.Problems, "; ")
		switch {
		case c.want == "" && len(res.Problems) != 0:
			t.Errorf("%s: unexpected problems: %s", c.name, joined)
		case c.want != "" && !strings.Contains(joined, c.want):
			t.Errorf("%s: problems %q lack %q", c.name, joined, c.want)
		}
	}
}

func TestSnapshotDropsSyntheticSpansUnlessLogsAreWanted(t *testing.T) {
	evs := []domain.Event{
		domain.SpanUpserted{TraceID: "t", SpanID: "a", Name: "real", Component: "c"},
		domain.SpanClosed{TraceID: "t", SpanID: "a"},
		domain.SpanUpserted{TraceID: "t", SpanID: "s", Name: "stdout", Component: "child", Synthetic: true},
		domain.LogReceived{ID: "1", Source: domain.SourceStdout, TraceID: "t", SpanID: "s", Body: "hello"},
		domain.DrainComplete{},
	}
	without := snap(evs, SnapshotOptions{Tree: true})
	if strings.Contains(without.Text, "child") || strings.Contains(without.Text, "hello") || len(without.Problems) != 0 {
		t.Errorf("synthetic span leaked: %v\n%s", without.Problems, without.Text)
	}
	if len(without.Captured) != 1 || without.Captured[0].Body != "hello" {
		t.Errorf("captured = %v", without.Captured)
	}
	with := snap(evs, SnapshotOptions{Tree: true, Logs: true})
	if !strings.Contains(with.Text, "hello") || len(with.Problems) != 0 {
		t.Errorf("logs missing: %v\n%s", with.Problems, with.Text)
	}
}
