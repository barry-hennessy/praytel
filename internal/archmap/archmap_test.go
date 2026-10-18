package archmap

import (
	"bytes"
	"flag"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var updateGolden = flag.Bool("update", false, "rewrite the snapshots in testdata")

func span(trace, id, parent, service, name string) domain.SpanUpserted {
	return domain.SpanUpserted{TraceID: trace, SpanID: id, ParentSpanID: parent, Component: service, Name: name}
}

// shopEvents is one test's trace through a small shop.
func shopEvents(trace, test string) []domain.Event {
	return []domain.Event{
		span(trace, "1", "", "tests", test),
		span(trace, "2", "1", "web", "Orders.Create"),
		span(trace, "3", "2", "checkout", "Orders.Place"),
		span(trace, "4", "3", "payments", "Gateway.Charge"),
		span(trace, "5", "4", "payments", "Gateway.Charge"), // a retry: no self-call
		span(trace, "6", "3", "checkout", "Orders.Repo.Save"),
	}
}

func record(t *testing.T, events ...domain.Event) Fragments {
	t.Helper()
	r := NewRecorder()
	for _, ev := range events {
		r.Observe(ev)
	}
	r.Observe(domain.DrainComplete{})
	frags, err := r.Fragments()
	if err != nil {
		t.Fatal(err)
	}
	return frags
}

func TestFormatIsCanonical(t *testing.T) {
	frags := record(t, shopEvents("t1", "TestVisa")...)
	got, err := Format(frags[Node{"tests", "TestVisa"}])
	if err != nil {
		t.Fatal(err)
	}
	want := `praytel-map v1
entry tests TestVisa

node checkout Orders.Place
node checkout Orders.Repo.Save
node payments Gateway.Charge
node tests TestVisa
node web Orders.Create

call checkout Orders.Place -> checkout Orders.Repo.Save
call checkout Orders.Place -> payments Gateway.Charge
call tests TestVisa -> web Orders.Create
call web Orders.Create -> checkout Orders.Place
`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// The whole point of a map is that the same behaviour gives the same
// bytes, however the run's events happened to arrive.
func TestEventOrderDoesNotChangeTheMap(t *testing.T) {
	events := append(shopEvents("t1", "TestVisa"), shopEvents("t2", "TestAmex")...)
	want := formatAll(t, record(t, events...))
	rng := rand.New(rand.NewSource(1))
	for range 50 {
		shuffled := slices.Clone(events)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := formatAll(t, record(t, shuffled...)); got != want {
			t.Fatalf("shuffled events gave\n%s\nwant\n%s", got, want)
		}
	}
}

func formatAll(t *testing.T, frags Fragments) string {
	t.Helper()
	var b strings.Builder
	for _, e := range sortedKeys(keysOf(frags), compareNodes) {
		data, err := Format(frags[e])
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

func TestFieldsThatNeedItAreQuotedAndReadBack(t *testing.T) {
	odd := []Node{
		{"web", "GET /orders/{id}"},
		{"", "->"},
		{`say "hi"`, "tab\there"},
		{"plain", "Name.With.Dots"},
	}
	m := New()
	m.Entries[odd[0]] = true
	for i, n := range odd {
		m.Nodes[n] = true
		if i > 0 {
			m.Calls[Call{odd[0], n}] = true
		}
	}
	data, err := Format(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `node web "GET /orders/{id}"`) || !strings.Contains(string(data), "node plain Name.With.Dots") {
		t.Fatalf("unexpected quoting:\n%s", data)
	}
	back, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(m) {
		t.Fatalf("round trip changed the map:\n%s", data)
	}
}

func TestParseRejectsWhatItDoesNotUnderstand(t *testing.T) {
	for _, in := range []string{
		"",
		"praytel-map v2\n",
		"praytel-map v1\nnode only-one-field\n",
		"praytel-map v1\ncall a b => c d\n",
		"praytel-map v1\nedge a b -> c d\n",
	} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Fatalf("parsed %q", in)
		}
	}
}

func TestFileNamesStayInsideTheDirectoryAndApart(t *testing.T) {
	seen := map[string]Node{}
	for _, e := range []Node{
		{"tests", "TestX/sub"},
		{"tests", "TestX%2Fsub"},
		{"..", ".."},
		{"", ".hidden"},
		{"a b", "c"},
		{"tests", "TestX"},
	} {
		name := FileName(e)
		parts := strings.Split(name, string(filepath.Separator))
		if len(parts) != 2 || strings.HasPrefix(parts[0], ".") || strings.HasPrefix(parts[1], ".") {
			t.Fatalf("%v: unsafe file name %q", e, name)
		}
		if prev, ok := seen[name]; ok {
			t.Fatalf("%v and %v share the file name %q", prev, e, name)
		}
		seen[name] = e
	}
}

func TestRecorderRefusesIncompleteRuns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []domain.Event
		want   string
	}{
		{"nothing arrived", []domain.Event{domain.DrainComplete{}}, "no traces arrived"},
		{"drain cut short", append(shopEvents("t1", "T"), domain.DrainComplete{TimedOut: true}), "--drain-timeout"},
		{"shed", append(shopEvents("t1", "T"), domain.ForestShed{}, domain.DrainComplete{}), "shed or evicted"},
		{"evicted", append(shopEvents("t1", "T"), domain.ForestEvicted{TraceID: "t1"}, domain.DrainComplete{}), "shed or evicted"},
		{"unknown parent", []domain.Event{span("t1", "2", "1", "web", "Orders.Create"), domain.DrainComplete{}}, "never arrived"},
		{"never drained", shopEvents("t1", "T"), "did not finish draining"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRecorder()
			for _, ev := range tc.events {
				r.Observe(ev)
			}
			_, err := r.Fragments()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestRecorderIgnoresSyntheticSpansAndUsesLateParents(t *testing.T) {
	frags := record(t,
		domain.SpanUpserted{TraceID: "t1", SpanID: "orphan", Component: "x", Name: "orphans", Synthetic: true},
		span("t1", "2", "", "web", "Orders.Create"),
		span("t1", "1", "", "tests", "TestLate"),
		domain.SpanParentLinked{TraceID: "t1", SpanID: "2", ParentSpanID: "1"},
	)
	if len(frags) != 1 {
		t.Fatalf("got %d entry points, want only TestLate's: %v", len(frags), frags)
	}
	m := frags[Node{"tests", "TestLate"}]
	if !m.Calls[Call{Node{"tests", "TestLate"}, Node{"web", "Orders.Create"}}] || m.Nodes[Node{"x", "orphans"}] {
		t.Fatalf("unexpected map %+v", m)
	}
}

func TestCompareListsEveryChangeOnce(t *testing.T) {
	old := record(t, append(shopEvents("t1", "TestVisa"), shopEvents("t2", "TestGone")...)...)
	changed := append(shopEvents("t1", "TestVisa")[:4], span("t1", "7", "3", "payments", "Fraud.Score"))
	new := record(t, append(changed, shopEvents("t3", "TestNew")...)...)

	got := strings.Join(Compare(old, new).Lines(), "\n")
	want := `- entry tests TestGone
+ entry tests TestNew
~ entry tests TestVisa
+ node payments Fraud.Score
- node tests TestGone
+ node tests TestNew
+ call checkout Orders.Place -> payments Fraud.Score
- call tests TestGone -> web Orders.Create
+ call tests TestNew -> web Orders.Create`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if d := Compare(old, old); d.Changed() || len(d.Lines()) != 0 {
		t.Fatalf("a map differs from itself: %v", d.Lines())
	}
}

func TestWriteTouchesOnlyWhatChanged(t *testing.T) {
	dir := t.TempDir()
	first := record(t, append(shopEvents("t1", "TestA"), shopEvents("t2", "TestB")...)...)
	if w, err := Write(dir, first); err != nil || len(w) != 2 || !w[0].Created {
		t.Fatalf("first write: %+v, %v", w, err)
	}
	other := filepath.Join(dir, FileName(Node{"tests", "TestB"}))
	before, _ := os.Stat(other)

	// A run of only TestA, which changed, leaves TestB's file alone.
	changed := record(t, append(shopEvents("t1", "TestA")[:4], span("t1", "7", "3", "payments", "Fraud.Score"))...)
	w, err := Write(dir, changed)
	if err != nil || len(w) != 1 || w[0].Created || !w[0].Diff.Changed() {
		t.Fatalf("second write: %+v, %v", w, err)
	}
	after, _ := os.Stat(other)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a file for an entry point that did not run was rewritten")
	}

	if w, err := Write(dir, changed); err != nil || len(w) != 0 {
		t.Fatalf("rewriting the same map changed %d files: %v", len(w), err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || !loaded[Node{"tests", "TestA"}].Equal(changed[Node{"tests", "TestA"}]) {
		t.Fatalf("loaded %v", loaded)
	}
}

// The snapshots in testdata are what a reviewer sees; compare them
// verbatim. After a deliberate change, regenerate with
// `go test ./internal/archmap -update` and read the diff as a picture.
func TestRenderSnapshots(t *testing.T) {
	old := record(t, shopEvents("t1", "TestVisa")...)
	changed := append(shopEvents("t1", "TestVisa")[:4], span("t1", "7", "3", "payments", "Fraud.Score"))
	new := record(t, changed...)
	plain := lipgloss.NewRenderer(&bytes.Buffer{})
	plain.SetColorProfile(termenv.Ascii)

	for name, render := range map[string]func(*bytes.Buffer) error{
		"map.txt":      func(b *bytes.Buffer) error { return RenderMap(b, plain, Text, old.Union()) },
		"diff.txt":     func(b *bytes.Buffer) error { return RenderDiff(b, plain, Text, Compare(old, new)) },
		"diff.mermaid": func(b *bytes.Buffer) error { return RenderDiff(b, plain, Mermaid, Compare(old, new)) },
	} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := render(&b); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name)
			if *updateGolden {
				if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if b.String() != string(want) {
				t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
			}
		})
	}
}
