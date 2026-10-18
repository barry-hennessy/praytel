package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const visaMap = `praytel-map v1
entry tests TestVisa

node checkout Orders.Place
node tests TestVisa

call tests TestVisa -> checkout Orders.Place
`

func writeMapFile(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "tests", "TestVisa.map")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMapCommandExitsLikeDiff(t *testing.T) {
	old, same, changed := t.TempDir(), t.TempDir(), t.TempDir()
	writeMapFile(t, old, visaMap)
	writeMapFile(t, same, visaMap)
	writeMapFile(t, changed, visaMap+"call checkout Orders.Place -> payments Fraud.Score\n")

	for _, tc := range []struct {
		name string
		args []string
		code int
		out  string
	}{
		{"one map draws it", []string{old}, 0, "checkout.Orders.Place"},
		{"same maps", []string{old, same}, 0, "no changes"},
		{"different maps", []string{old, changed}, 1, "+ call checkout Orders.Place -> payments Fraud.Score"},
		{"mermaid", []string{"--format", "mermaid", old, changed}, 1, "==>"},
		{"missing map", []string{old, filepath.Join(old, "nope")}, 2, ""},
		{"bad format", []string{"--format", "svg", old}, 2, ""},
		{"no maps", nil, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runMapCommand(tc.args, &stdout, &stderr); code != tc.code {
				t.Fatalf("exit %d, want %d; stderr: %s", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.out) {
				t.Fatalf("stdout has no %q:\n%s", tc.out, stdout.String())
			}
		})
	}
}

func TestMapCommandRejectsAMalformedFile(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "praytel-map v1\nnonsense\n")
	var stdout, stderr bytes.Buffer
	if code := runMapCommand([]string{dir}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "line 2") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}
