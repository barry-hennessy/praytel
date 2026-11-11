package starlark

import (
	"fmt"
	"strings"

	sl "go.starlark.net/starlark"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/query"
)

// table(selection, columns = [...]) is the table view: one row for each
// selected span, with the fields of its columns ("span:name", "test.name",
// "resource.service.name", "span:duration", …).
func tableFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var sv sl.Value
	var cols *sl.List
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "selection", &sv, "columns", &cols); err != nil {
		return nil, err
	}
	e, err := asSelection(sv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	var columns []string
	for c := range cols.Elements() {
		s, ok := sl.AsString(c)
		if !ok {
			return nil, fmt.Errorf("%s: a column is a string, not a %s", b.Name(), c.Type())
		}
		if err := query.CheckColumn(s); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		columns = append(columns, s)
	}
	q := query.Format(e)
	return &part{
		view: &plan.View{Kind: plan.ViewTable, Select: q, Columns: columns},
		desc: []string{fmt.Sprintf("table(%s, columns = [%s])", q, strings.Join(columns, ", "))},
	}, nil
}
