package main_test

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/test/sweet"
)

var (
	praytelBin string
	childBin   string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "praytel-wrapper-")
	if err != nil {
		panic(err)
	}

	praytelBin = filepath.Join(dir, "praytel")
	childBin = filepath.Join(dir, "child")

	build := func(out, pkg string) {
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			_ = os.RemoveAll(dir)
			panic(err)
		}
	}

	build(praytelBin, ".")
	build(childBin, "./testdata/child")

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type wrapDeps struct {
	dir string
}

func wrapFactory(t *testing.T) wrapDeps {
	t.Helper()

	return wrapDeps{dir: t.TempDir()}
}

func TestTrueExitsZero(t *testing.T) {
	sweet.Run(t, "praytel -- true exits 0", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", "true")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestFalseExitsOne(t *testing.T) {
	sweet.Run(t, "praytel -- false exits 1", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", "false")
		if res.exit != 1 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestMissingBinaryDoesNotClaimChildRan(t *testing.T) {
	sweet.Run(t, "missing binary is a start failure", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", filepath.Join(d.dir, "no-such-praytel-child"))
		if res.exit == 0 {
			t.Fatal("expected non-zero exit")
		}

		low := strings.ToLower(res.stderr)
		if strings.Contains(low, "child ran") || strings.Contains(low, "child exited") {
			t.Fatalf("claimed a child ran: %q", res.stderr)
		}
		if !strings.Contains(low, "start child") && !strings.Contains(low, "not found") && !strings.Contains(low, "executable") {
			t.Fatalf("stderr should describe exec failure, got %q", res.stderr)
		}
		if res.stdout != "" {
			t.Fatalf("diagnostics leaked to stdout: %q", res.stdout)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestMissingSeparatorIsUsage(t *testing.T) {
	sweet.Run(t, "missing -- is usage and non-zero", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "true")
		if res.exit == 0 {
			t.Fatal("expected non-zero exit")
		}
		if res.stdout != "" {
			t.Fatalf("diagnostics leaked to stdout: %q", res.stdout)
		}
		if !strings.Contains(res.stderr, "usage:") {
			t.Fatalf("stderr=%q", res.stderr)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestHelperChildExitAndStdio(t *testing.T) {
	sweet.Run(t, "helper child exit and append-only stream", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-exit", "42", "-stdout", "out-bytes", "-stderr", "err-bytes")
		if res.exit != 42 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "out-bytes") != 1 {
			t.Fatalf("stdout should contain out-bytes once: %q", res.stdout)
		}
		if strings.Count(res.stdout, "err-bytes") != 1 {
			t.Fatalf("child stderr should appear on the stream: %q", res.stdout)
		}
		if !strings.Contains(res.stdout, "stderr") {
			t.Fatalf("stderr source missing from stream: %q", res.stdout)
		}
		if strings.Contains(res.stderr, "err-bytes") {
			t.Fatalf("child stderr leaked as raw passthrough: %q", res.stderr)
		}
		if strings.Contains(res.stdout, "praytel") || strings.Contains(res.stdout, "component") {
			t.Fatalf("praytel diagnostics on stdout: %q", res.stdout)
		}
		if strings.Contains(res.stdout, "\x1b[") {
			t.Fatalf("pipe should be monochrome: %q", res.stdout)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestPlainTextDoesNotWarn(t *testing.T) {
	sweet.Run(t, "unstructured non-JSON does not warn", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-stdout", "just a line")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "just a line") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}
		low := strings.ToLower(res.stderr)
		if strings.Contains(low, "malformed") {
			t.Fatalf("plain text warned: %q", res.stderr)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestMalformedJSONWarnsOnStderr(t *testing.T) {
	sweet.Run(t, "broken JSON warns on stderr and still streams the line", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-stdout", "{not-json")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "{not-json") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}
		if !strings.Contains(res.stderr, "malformed json") {
			t.Fatalf("expected malformed json warning, stderr=%q", res.stderr)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestNoColorStillExitsZero(t *testing.T) {
	sweet.Run(t, "NO_COLOR is monochrome and keeps the child exit", wrapFactory, func(t *testing.T, d wrapDeps) {
		t.Setenv("NO_COLOR", "1")
		res := runPraytel(t, d.dir, "--", childBin, "-stdout", "mono-line")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "mono-line") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}
		if strings.Contains(res.stdout, "\x1b[") {
			t.Fatalf("NO_COLOR still coloured: %q", res.stdout)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestChildSeesOTELExporterEnv(t *testing.T) {
	sweet.Run(t, "child OTEL env points at bound ports", wrapFactory, func(t *testing.T, d wrapDeps) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://example.invalid:9")
		t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://example.invalid:9")

		grpcPort := mustFreePort(t)
		httpPort := mustFreePort(t)
		res := runPraytelOn(t, d.dir, grpcPort, httpPort, "--", childBin, "-print-otel-env")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}

		assertOTELEnvPorts(t, res.stdout, grpcPort, httpPort)
		if strings.Contains(res.stdout, "example.invalid") {
			t.Fatalf("parent OTEL exporter env leaked:\n%s", res.stdout)
		}
		if strings.Contains(res.stdout, "METRIC") {
			t.Fatalf("metrics exporter env presented to child:\n%s", res.stdout)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestFlagPortsBindAndAppearInChildEnv(t *testing.T) {
	sweet.Run(t, "--grpc-port and --http-port appear in child env", wrapFactory, func(t *testing.T, d wrapDeps) {
		grpcPort := mustFreePort(t)
		httpPort := mustFreePort(t)
		res := runPraytelExact(t, d.dir,
			"--grpc-port", strconv.Itoa(grpcPort),
			"--http-port", strconv.Itoa(httpPort),
			"--", childBin, "-print-otel-env",
		)
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}

		assertOTELEnvPorts(t, res.stdout, grpcPort, httpPort)
		assertNoStrayFiles(t, d.dir)
	})
}

func TestOccupiedDefaultPortDoesNotExecChild(t *testing.T) {
	sweet.Run(t, "occupied 4317 without override does not exec", wrapFactory, func(t *testing.T, d wrapDeps) {
		ln, err := net.Listen("tcp", fmtListen(receiver.DefaultGRPCPort))
		if err != nil {
			t.Logf("4317 already occupied (%v); bind should still fail", err)
		} else {
			t.Cleanup(func() { _ = ln.Close() })
		}

		marker := filepath.Join(d.dir, "child-ran")
		res := runPraytelExact(t, d.dir, "--", childBin, "-marker", marker)
		if res.exit == 0 {
			t.Fatal("expected non-zero exit")
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("child was executed")
		}
		if res.stdout != "" {
			t.Fatalf("diagnostics leaked to stdout: %q", res.stdout)
		}

		low := strings.ToLower(res.stderr)
		if !strings.Contains(low, "bind") && !strings.Contains(low, "listen") && !strings.Contains(low, "address already in use") {
			t.Fatalf("stderr should describe bind failure, got %q", res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestWrapperHasNoOTelSDKImport(t *testing.T) {
	sweet.Run(t, "command package does not import the OTel SDK", wrapFactory, func(t *testing.T, _ wrapDeps) {
		cmd := exec.Command("go", "list", "-f", "{{range .Imports}}{{println .}}{{end}}", ".")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}

		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "go.opentelemetry.io/otel") {
				t.Fatalf("wrapper imported OTel SDK %q", line)
			}
		}
	})
}

type procResult struct {
	exit   int
	stdout string
	stderr string
}

func runPraytel(t *testing.T, dir string, args ...string) procResult {
	t.Helper()

	return runPraytelOn(t, dir, mustFreePort(t), mustFreePort(t), args...)
}

func runPraytelOn(t *testing.T, dir string, grpcPort, httpPort int, args ...string) procResult {
	t.Helper()

	prefixed := append([]string{
		"--grpc-port", strconv.Itoa(grpcPort),
		"--http-port", strconv.Itoa(httpPort),
	}, args...)

	return runPraytelExact(t, dir, prefixed...)
}

func runPraytelExact(t *testing.T, dir string, args ...string) procResult {
	t.Helper()

	cmd := exec.Command(praytelBin, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := procResult{
		exit:   0,
		stdout: stdout.String(),
		stderr: stderr.String(),
	}
	if err == nil {
		return res
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.exit = ee.ExitCode()
		return res
	}

	t.Fatalf("run praytel: %v stderr=%q", err, stderr.String())
	return res
}

func assertNoStrayFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("stray files after run: %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}

	return out
}

func mustFreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return port
}

func fmtListen(port int) string {
	return "127.0.0.1:" + strconv.Itoa(port)
}

func assertOTELEnvPorts(t *testing.T, stdout string, grpcPort, httpPort int) {
	t.Helper()

	if !strings.Contains(stdout, strconv.Itoa(grpcPort)) {
		t.Fatalf("grpc port %d missing from child env:\n%s", grpcPort, stdout)
	}
	if !strings.Contains(stdout, strconv.Itoa(httpPort)) {
		t.Fatalf("http port %d missing from child env:\n%s", httpPort, stdout)
	}
	if !strings.Contains(stdout, "OTEL_EXPORTER_OTLP_") {
		t.Fatalf("expected OTEL_EXPORTER_OTLP_* in child env:\n%s", stdout)
	}
}
