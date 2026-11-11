package query

// Path is the "path" view of a result: the selected spans with their
// ancestors and descendants, so a match is shown in its place in the tree.
func (t *Trace) Path(r Result) map[string]bool {
	keep := map[string]bool{}
	children := map[string][]string{}
	for id, sp := range t.spans {
		if sp.ParentID != "" {
			children[sp.ParentID] = append(children[sp.ParentID], id)
		}
	}
	walked := map[string]bool{}
	var down func(id string)
	down = func(id string) {
		if walked[id] {
			return
		}
		walked[id] = true
		keep[id] = true
		for _, c := range children[id] {
			down(c)
		}
	}
	for id := range r.Spans {
		if t.spans[id] == nil {
			continue
		}
		down(id)
		seen := map[string]bool{id: true}
		for p := t.spans[id].ParentID; p != "" && !seen[p] && t.spans[p] != nil; p = t.spans[p].ParentID {
			seen[p] = true
			keep[p] = true
		}
	}
	return keep
}
