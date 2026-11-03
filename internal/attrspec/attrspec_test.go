package attrspec_test

import (
	"testing"

	"github.com/barry-hennessy/praytel/internal/attrspec"
)

func TestSpec(t *testing.T) {
	cases := []struct {
		name string
		spec attrspec.Spec
		key  string
		want bool
	}{
		{"absent shows none", attrspec.Spec{}, "a", false},
		{"bare shows all", attrspec.All(), "a", true},
		{"only listed", must(t, "a b"), "a", true},
		{"only listed, other", must(t, "a b"), "c", false},
		{"exclude wins", must(t, "a b -a"), "a", false},
		{"include minus exclude", must(t, "http.* -http.url"), "http.method", true},
		{"include minus exclude, excluded", must(t, "http.* -http.url"), "http.url", false},
		{"exclude only shows rest", must(t, "-c"), "a", true},
		{"exclude only hides it", must(t, "-c"), "c", false},
	}
	for _, c := range cases {
		if got := c.spec.Match(c.key); got != c.want {
			t.Errorf("%s: Match(%q) = %v, want %v", c.name, c.key, got, c.want)
		}
	}
}

func TestParseRejectsBadPatterns(t *testing.T) {
	for _, s := range []string{"[", "-", "a -"} {
		if _, err := attrspec.Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", s)
		}
	}
}

func must(t *testing.T, s string) attrspec.Spec {
	t.Helper()
	sp, err := attrspec.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}
