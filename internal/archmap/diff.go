package archmap

// State is what happened to an entry point, node or call between two maps.
type State uint8

const (
	Unchanged State = iota
	Added
	Removed
	// Changed is a node or entry point present in both maps whose calls
	// (or, for an entry point, whose map) differ.
	Changed
)

// Diff compares two sets of fragments. Nodes and calls are compared across
// the union of each side's fragments; entry points by their own fragment.
type Diff struct {
	Old, New Map // each side's union
	Entries  map[Node]State
	Nodes    map[Node]State
	Calls    map[Call]State
}

// Compare diffs old against new.
func Compare(old, new Fragments) Diff {
	d := Diff{Old: old.Union(), New: new.Union(), Entries: map[Node]State{}, Nodes: map[Node]State{}, Calls: map[Call]State{}}

	for e, frag := range new {
		switch prev, ok := old[e]; {
		case !ok:
			d.Entries[e] = Added
		case !prev.Equal(frag):
			d.Entries[e] = Changed
		default:
			d.Entries[e] = Unchanged
		}
	}
	for e := range old {
		if _, ok := new[e]; !ok {
			d.Entries[e] = Removed
		}
	}

	for c := range d.New.Calls {
		d.Calls[c] = Unchanged
		if !d.Old.Calls[c] {
			d.Calls[c] = Added
		}
	}
	for c := range d.Old.Calls {
		if !d.New.Calls[c] {
			d.Calls[c] = Removed
		}
	}

	for n := range d.New.Nodes {
		d.Nodes[n] = Unchanged
		if !d.Old.Nodes[n] {
			d.Nodes[n] = Added
		}
	}
	for n := range d.Old.Nodes {
		if !d.New.Nodes[n] {
			d.Nodes[n] = Removed
		}
	}
	for c, s := range d.Calls {
		if s == Unchanged {
			continue
		}
		for _, n := range []Node{c.From, c.To} {
			if d.Nodes[n] == Unchanged {
				d.Nodes[n] = Changed
			}
		}
	}
	return d
}

// Changed reports whether the two sides differ at all.
func (d Diff) Changed() bool {
	for _, s := range d.Entries {
		if s != Unchanged {
			return true
		}
	}
	return false
}

func (s State) sign() string {
	switch s {
	case Added:
		return "+ "
	case Removed:
		return "- "
	case Changed:
		return "~ "
	}
	return "  "
}

// Lines lists every change, one per line, in file order: entry points
// whose map changed, then added and removed nodes, then added and removed
// calls. Each starts with +, - or ~.
func (d Diff) Lines() []string {
	var out []string
	for _, e := range sortedKeys(keys(d.Entries), compareNodes) {
		if s := d.Entries[e]; s != Unchanged {
			out = append(out, s.sign()+"entry "+nodeFields(e))
		}
	}
	for _, n := range sortedKeys(keys(d.Nodes), compareNodes) {
		if s := d.Nodes[n]; s == Added || s == Removed {
			out = append(out, s.sign()+NodeLine(n))
		}
	}
	for _, c := range sortedKeys(keys(d.Calls), compareCalls) {
		if s := d.Calls[c]; s != Unchanged {
			out = append(out, s.sign()+CallLine(c))
		}
	}
	return out
}

func keys[K comparable](m map[K]State) map[K]bool {
	out := make(map[K]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
