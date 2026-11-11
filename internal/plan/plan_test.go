package plan_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/plan"
)

func stream() plan.Output {
	return plan.Output{View: plan.View{Kind: plan.ViewStream}, Emit: plan.Each, Sink: plan.Sink{Kind: plan.SinkStdout}}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		edit func(*plan.Output)
		want string // "" means valid
	}{
		"stream":            {func(*plan.Output) {}, ""},
		"unknown view":      {func(o *plan.Output) { o.View.Kind = "pie" }, `unknown view "pie"`},
		"unknown trigger":   {func(o *plan.Output) { o.Emit = "never" }, `unknown trigger "never"`},
		"unknown sink":      {func(o *plan.Output) { o.Sink.Kind = "fax" }, `unknown sink "fax"`},
		"unknown stage":     {func(o *plan.Output) { o.Stages = []plan.Stage{{Kind: "x"}} }, `unknown stage "x"`},
		"bad glob":          {func(o *plan.Output) { o.Stages = []plan.Stage{{Kind: plan.StageAttrs, Globs: "["}} }, "stage attrs"},
		"file without path": {func(o *plan.Output) { o.Sink.Kind = plan.SinkFile }, "needs a path"},
		"tui without tick":  {func(o *plan.Output) { o.Sink.Kind = plan.SinkTUI }, "needs the trigger tick"},
		"snapshot each": {func(o *plan.Output) {
			o.View = plan.View{Kind: plan.ViewSnapshot, Sections: []string{plan.SectionTree}}
		}, "view snapshot cannot have the trigger each"},
		"snapshot no sections": {func(o *plan.Output) {
			o.View = plan.View{Kind: plan.ViewSnapshot}
			o.Emit = plan.Final
		}, "needs sections"},
		"snapshot bad section": {func(o *plan.Output) {
			o.View = plan.View{Kind: plan.ViewSnapshot, Sections: []string{"pie"}}
			o.Emit = plan.Final
		}, `unknown snapshot section "pie"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := stream()
			c.edit(&o)
			err := plan.Plan{Outputs: []plan.Output{o}}.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("error %v, want it to contain %q", err, c.want)
			}
		})
	}

	if err := (plan.Plan{}).Validate(); err == nil {
		t.Fatal("an empty plan is valid")
	}
}

// Explain is JSON that reads back to the same plan.
func TestExplainRoundTrips(t *testing.T) {
	p := plan.Plan{Outputs: []plan.Output{stream(), {
		Name:   "snap",
		Stages: []plan.Stage{{Kind: plan.StageAttrs, Globs: "a -b"}},
		View:   plan.View{Kind: plan.ViewSnapshot, Sections: []string{plan.SectionTree}, Logs: true},
		Emit:   plan.Final,
		Sink:   plan.Sink{Kind: plan.SinkFile, Path: "out.txt", Exit: true},
	}}}
	var back plan.Plan
	if err := json.Unmarshal(p.Explain(), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, p) {
		t.Fatalf("round trip\n got %+v\nwant %+v", back, p)
	}
}
