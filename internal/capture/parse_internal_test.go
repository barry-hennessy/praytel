package capture

import (
	"strings"
	"sync"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

const (
	zapLine   = "2026-10-14T16:41:49.412+0200\tINFO\tcustomer/server.go:64\tHTTP request received\t{\"service\": \"customer\", \"trace_id\": \"45c443abeab18a13a0c65bb3c8b4a5d8\", \"span_id\": \"1234567890abcdef\", \"method\": \"GET\"}"
	plainLine = "Starting HTTP server on 0.0.0.0:8080 with a reasonably long plain text message"
	jsonLine  = `{"level":"info","ts":1.7e9,"msg":"HTTP request received","service":"customer","trace_id":"45c443abeab18a13a0c65bb3c8b4a5d8","span_id":"1234567890abcdef","method":"GET"}`
	lfLine    = `time=2026-01-01T00:00:00Z level=INFO msg="HTTP request received" service=customer trace_id=45c443abeab18a13a0c65bb3c8b4a5d8 span_id=1234567890abcdef method=GET`
	consoleLn = `Sep 18 11:09:23.508 WRN energy/appointment.go:164 appointment service using stub adaptors trace_id=abc123 span_id=def456`
)

// corpus holds lines of every shape the parsers see, including the awkward
// ones, for the order-independence and fuzz tests.
var corpus = []string{
	"",
	plainLine,
	jsonLine,
	lfLine,
	consoleLn,
	zapLine,
	strings.ReplaceAll(zapLine, "\t", " "),
	"2026-10-14\tINFO\tmain.go:1\tgot {1 2} and {\"x\": 1}\t{\"trace_id\": \"t\", \"req\": {\"span_id\": \"nested\"}, \"span_id\": \"s\"}",
	`{"msg":"camel","traceId":"t","spanId":"s"}`,
	`{"message":"ecs","trace.id":"t","span.id":"s"}`,
	`msg=a trace_id=x {"trace_id":"y"}`,
	`payload={"trace_id":"x"}`,
	`{"a":1} {"trace_id":"x"}`,
	`config loaded {"port": 8080}`,
	`{"trace_id": 7}`,
	"{not json",
	"[1,2]",
	"[1,",
	"{}",
	"{",
	strings.Repeat(`x {"`, 50) + "}",
}

func TestParseJSONLine(t *testing.T) {
	cases := []struct {
		line          string
		trace, span   string
		recognised    bool
		malformed     bool
		attrsContains string
	}{
		{line: jsonLine, trace: "45c443abeab18a13a0c65bb3c8b4a5d8", span: "1234567890abcdef", recognised: true, attrsContains: "method"},
		{line: `{"traceId":"t","spanId":"s"}`, trace: "t", span: "s", recognised: true},
		{line: `{"traceID":"t","spanID":"s"}`, trace: "t", span: "s", recognised: true},
		{line: `{"trace.id":"t","span.id":"s"}`, trace: "t", span: "s", recognised: true},
		{line: `{"trace_id":"first","traceId":"second"}`, trace: "first", recognised: true},
		{line: `{"msg":"no ids","n":1}`, recognised: true, attrsContains: "n"},
		{line: `{"trace_id": 7}`, recognised: true},
		{line: `{"req":{"trace_id":"nested"}}`, recognised: true},
		{line: "[1,2]", recognised: true},
		{line: "{not json", malformed: true},
		{line: `{"a":1 "b":2}`, malformed: true},
		{line: `{"a":1} {"trace_id":"x"}`, malformed: true},
		{line: "[1,", malformed: true},
		{line: plainLine},
		{line: zapLine},
	}
	for _, tc := range cases {
		r := parseJSONLine(tc.line)
		checkResult(t, tc.line, r, tc.trace, tc.span, tc.recognised, tc.malformed, tc.attrsContains)
	}
}

func TestParseTrailingObject(t *testing.T) {
	cases := []struct {
		line          string
		trace, span   string
		recognised    bool
		attrsContains string
	}{
		{line: zapLine, trace: "45c443abeab18a13a0c65bb3c8b4a5d8", span: "1234567890abcdef", recognised: true, attrsContains: "service"},
		{line: strings.ReplaceAll(zapLine, "\t", " "), trace: "45c443abeab18a13a0c65bb3c8b4a5d8", span: "1234567890abcdef", recognised: true},
		{line: "INFO\tmsg with {braces} inside\t{\"trace_id\": \"t\"}", trace: "t", recognised: true},
		{line: "INFO\tgot {\"x\": 1} back\t{\"span_id\": \"s\"}", span: "s", recognised: true},
		{line: "INFO\tm\t{\"trace_id\": \"t\", \"req\": {\"span_id\": \"nested\"}}", trace: "t", recognised: true},
		{line: `{"a":1} {"trace_id":"x"}`, trace: "x", recognised: true},
		{line: `response: {"traceId": "t"}`, trace: "t", recognised: true},
		{line: `config loaded {"port": 8080}`},
		{line: `payload={"trace_id":"x"}`},
		{line: jsonLine},
		{line: `INFO msg {"trace_id": "t"} trailing`},
		{line: `INFO msg {"trace_id": "t"`},
		{line: plainLine},
		{line: strings.Repeat(`x {"`, maxTrailingAttempts) + ` {"trace_id":"t"}`},
	}
	for _, tc := range cases {
		r := parseTrailingObject(tc.line)
		checkResult(t, tc.line, r, tc.trace, tc.span, tc.recognised, false, tc.attrsContains)
		if tc.recognised && r.covered >= len(tc.line) {
			t.Errorf("%q: covered=%d, want less than the line", tc.line, r.covered)
		}
	}
}

func TestParseLogfmtLine(t *testing.T) {
	cases := []struct {
		line          string
		trace, span   string
		recognised    bool
		attrsContains string
	}{
		{line: lfLine, trace: "45c443abeab18a13a0c65bb3c8b4a5d8", span: "1234567890abcdef", recognised: true, attrsContains: "service"},
		{line: consoleLn, trace: "abc123", span: "def456", recognised: true},
		{line: `msg=ok traceId=t spanId=s`, trace: "t", span: "s", recognised: true},
		{line: `msg=ok trace_id=aaa`, trace: "aaa", recognised: true},
		{line: `trace_id= span_id=s`, span: "s", recognised: true},
		{line: `trace_id=x "quoted"=key`, trace: "x", recognised: true},
		{line: "foo=bar"},
		{line: plainLine},
		{line: jsonLine},
		{line: zapLine},
	}
	for _, tc := range cases {
		r := parseLogfmtLine(tc.line)
		checkResult(t, tc.line, r, tc.trace, tc.span, tc.recognised, false, tc.attrsContains)
	}

	full := parseLogfmtLine(lfLine)
	broken := parseLogfmtLine(`trace_id=x "quoted"=key`)
	if full.covered != len(lfLine) || broken.covered >= len(`trace_id=x "quoted"=key`) {
		t.Errorf("covered full=%d broken=%d", full.covered, broken.covered)
	}
}

func checkResult(t *testing.T, line string, r result, trace, span string, recognised, malformed bool, attr string) {
	t.Helper()

	if r.traceID != trace || r.spanID != span {
		t.Errorf("%q: ids=(%q,%q) want (%q,%q)", line, r.traceID, r.spanID, trace, span)
	}
	if (r.covered > 0) != recognised {
		t.Errorf("%q: covered=%d want recognised=%v", line, r.covered, recognised)
	}
	if r.malformed != malformed {
		t.Errorf("%q: malformed=%v want %v", line, r.malformed, malformed)
	}
	if attr != "" && !hasAttr(r.attrs, attr) {
		t.Errorf("%q: attrs %v lack %q", line, r.attrs, attr)
	}
}

func hasAttr(attrs []domain.Attr, key string) bool {
	for _, a := range attrs {
		if a.Key == key {
			return true
		}
	}

	return false
}

func TestCombine(t *testing.T) {
	a := result{fields: fields{traceID: "a"}, covered: 10}
	b := result{fields: fields{traceID: "b"}, covered: 10}
	small := result{fields: fields{traceID: "small"}, covered: 4}
	broken := result{malformed: true}
	cases := []struct {
		name string
		in   []result
		want result
	}{
		{"nothing recognised", []result{{}, {}}, result{}},
		{"most coverage wins", []result{small, a}, a},
		{"equal results agree", []result{a, a, small}, a},
		{"equal coverage disagreeing is ambiguous", []result{a, b, small}, result{}},
		{"malformed only when nothing recognised", []result{broken, small}, small},
		{"malformed survives when alone", []result{broken, {}}, broken},
	}
	for _, tc := range cases {
		for _, in := range permutations(tc.in) {
			if got := combine(in); !got.same(tc.want) || got.covered != tc.want.covered || got.malformed != tc.want.malformed {
				t.Errorf("%s: combine(%v)=%+v want %+v", tc.name, in, got, tc.want)
			}
		}
	}
}

func TestParserOrderDoesNotMatter(t *testing.T) {
	for _, line := range corpus {
		assertOrderFree(t, line)
	}
}

func TestParsersRunConcurrently(t *testing.T) {
	pool := newParserPool()
	defer pool.close()

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for _, line := range corpus {
				want := parseSerial(line)
				if got := parseConcurrent(line); !got.same(want) || got.malformed != want.malformed {
					t.Errorf("%q: concurrent %+v, serial %+v", line, got, want)
				}
				if got := pool.parse(line); !got.same(want) || got.malformed != want.malformed {
					t.Errorf("%q: pool %+v, serial %+v", line, got, want)
				}
			}
		})
	}
	wg.Wait()
}

func FuzzParseLine(f *testing.F) {
	for _, line := range corpus {
		f.Add(line)
	}
	f.Fuzz(func(t *testing.T, line string) {
		got := parseLine(domain.SourceStdout, line)
		if got.Body.String() != ansi.Strip(line) {
			t.Fatalf("body %q, want the stripped line", got.Body.String())
		}
		assertOrderFree(t, strings.TrimSpace(ansi.Strip(line)))
	})
}

// assertOrderFree runs the parsers over line in every order and checks that
// combine picks the same result each time.
func assertOrderFree(t *testing.T, line string) {
	t.Helper()

	if line == "" {
		return
	}
	var results []result
	for _, p := range parsers {
		results = append(results, p(line))
	}
	want := combine(results)
	for _, in := range permutations(results) {
		if got := combine(in); !got.same(want) || got.covered != want.covered || got.malformed != want.malformed {
			t.Fatalf("%q: order changed the result: %+v vs %+v", line, got, want)
		}
	}
}

func permutations[T any](xs []T) [][]T {
	if len(xs) <= 1 {
		return [][]T{append([]T(nil), xs...)}
	}
	var out [][]T
	for i := range xs {
		rest := append(append([]T(nil), xs[:i]...), xs[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]T{xs[i]}, p...))
		}
	}

	return out
}

// The runners below exist to measure whether running the parsers at the same
// time is faster than one after another; see BenchmarkParserRunners.

func parseSerial(line string) result {
	var results [len(parsers)]result
	for i, p := range parsers {
		results[i] = p(line)
	}

	return combine(results[:])
}

// parseConcurrent starts one goroutine per parser for each line.
func parseConcurrent(line string) result {
	var results [len(parsers)]result
	var wg sync.WaitGroup
	for i, p := range parsers {
		wg.Go(func() { results[i] = p(line) })
	}
	wg.Wait()

	return combine(results[:])
}

// parserPool keeps one long-lived goroutine per parser and hands each line to
// all of them, so the cost measured is the hand-off rather than goroutine
// start-up.
type parserPool struct {
	in [len(parsers)]chan poolJob
}

type poolJob struct {
	line string
	out  *result
	wg   *sync.WaitGroup
}

func newParserPool() *parserPool {
	p := &parserPool{}
	for i, parse := range parsers {
		p.in[i] = make(chan poolJob)
		go func() {
			for job := range p.in[i] {
				*job.out = parse(job.line)
				job.wg.Done()
			}
		}()
	}

	return p
}

func (p *parserPool) parse(line string) result {
	var results [len(parsers)]result
	var wg sync.WaitGroup
	wg.Add(len(parsers))
	for i := range p.in {
		p.in[i] <- poolJob{line: line, out: &results[i], wg: &wg}
	}
	wg.Wait()

	return combine(results[:])
}

func (p *parserPool) close() {
	for _, c := range p.in {
		close(c)
	}
}

var benchLines = map[string]string{
	"plain":   plainLine,
	"json":    jsonLine,
	"logfmt":  lfLine,
	"console": consoleLn,
	"zap":     zapLine,
}

func BenchmarkParserRunners(b *testing.B) {
	pool := newParserPool()
	defer pool.close()

	runners := []struct {
		name string
		run  func(string) result
	}{
		{"serial", parseSerial},
		{"goroutine-per-parser", parseConcurrent},
		{"worker-pool", pool.parse},
	}
	for _, r := range runners {
		for name, line := range benchLines {
			b.Run(r.name+"/"+name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					r.run(line)
				}
			})
		}
	}
}

// BenchmarkParseLine measures the whole per-line cost capture pays, ANSI
// stripping included.
func BenchmarkParseLine(b *testing.B) {
	for name, line := range benchLines {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				parseLine(domain.SourceStdout, line)
			}
		})
	}
}

func BenchmarkEachParser(b *testing.B) {
	named := map[string]parser{"json": parseJSONLine, "trailing": parseTrailingObject, "logfmt": parseLogfmtLine}
	for pname, p := range named {
		for lname, line := range benchLines {
			b.Run(pname+"/"+lname, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					p(line)
				}
			})
		}
	}
}
