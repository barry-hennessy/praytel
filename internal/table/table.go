// Package table is the table view: one row for each span a query selects,
// with the fields of its columns. Rows are sorted, so a table does not
// depend on arrival order and can be kept as a golden file.
package table

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
	"github.com/barry-hennessy/praytel/internal/view"
)

// View is a table.
type View struct {
	sel     query.Expr
	columns []string
	fields  []query.Field
	ndjson  bool
}

var _ view.View = (*View)(nil)

// New compiles a table over the spans sel selects. With ndjson the rows
// are JSON objects; otherwise tab-separated values under a header line.
func New(sel string, columns []string, ndjson bool) (*View, error) {
	e, err := query.Parse(sel)
	if err != nil {
		return nil, err
	}
	v := &View{sel: e, columns: columns, ndjson: ndjson}
	for _, c := range columns {
		if err := query.CheckColumn(c); err != nil {
			return nil, err
		}
		f, _ := query.ParseField(c, query.KindSpan)
		v.fields = append(v.fields, f)
	}
	return v, nil
}

// Observe does nothing: a table reads the projection.
func (v *View) Observe(domain.Event, projection.Reader) {}

// Render makes the table of what r holds.
func (v *View) Render(r projection.Reader, _ view.Viewport) []string {
	var rows [][]string
	for _, t := range query.TracesOf(r) {
		for id := range query.Eval(v.sel, t).Spans {
			sp := t.Span(id)
			row := make([]string, len(v.fields))
			for i, f := range v.fields {
				row[i], _ = query.FieldValue(sp, t, f)
			}
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(a, b []string) int { return slices.Compare(a, b) })

	if v.ndjson {
		lines := make([]string, 0, len(rows))
		for _, row := range rows {
			obj := map[string]string{}
			for i, c := range v.columns {
				obj[c] = row[i]
			}
			b, _ := json.Marshal(obj)
			lines = append(lines, string(b))
		}
		return lines
	}
	lines := []string{strings.Join(v.columns, "\t")}
	for _, row := range rows {
		for i := range row {
			row[i] = strings.NewReplacer("\t", " ", "\n", " ").Replace(row[i])
		}
		lines = append(lines, strings.Join(row, "\t"))
	}
	return lines
}
