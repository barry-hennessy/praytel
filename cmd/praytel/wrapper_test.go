package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/test/sweet"
	"github.com/go-logfmt/logfmt"
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

	build := func(out, pkg, workDir string) {
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Dir = workDir
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			_ = os.RemoveAll(dir)
			panic(err)
		}
	}

	build(praytelBin, ".", "")
	build(childBin, ".", "./testdata/child")

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

func TestGRPCExportingChildJoinsJSONLog(t *testing.T) {
	sweet.Run(t, "gRPC span hydrate appears on the stream with the JSON log", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-export-grpc-span", "-span-name", "child-work")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}

		joined := false
		for _, line := range strings.Split(res.stdout, "\n") {
			brace := strings.Index(line, "{")
			if brace < 0 || !strings.Contains(line[brace:], `"msg":"grpc-span"`) {
				continue
			}

			traceID, spanID := jsonTraceSpan(t, line)
			prefix := line[:brace]
			if !strings.Contains(prefix, "child-work") {
				continue
			}
			if !strings.Contains(prefix, traceID) || !strings.Contains(prefix, spanID) {
				t.Fatalf("stream prefix missing joined ids: %q", line)
			}
			joined = true
		}
		if !joined {
			t.Fatalf("expected hydrated stream line for child-work, stdout=%q stderr=%q", res.stdout, res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestGRPCExportingChildJoinsLogfmtLog(t *testing.T) {
	sweet.Run(t, "gRPC span hydrate appears on the stream with the logfmt log", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-export-grpc-span-logfmt", "-span-name", "child-work")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}

		joined := false
		for _, line := range strings.Split(res.stdout, "\n") {
			if !strings.Contains(line, "msg=grpc-span") && !strings.Contains(line, `msg="grpc-span"`) {
				continue
			}

			traceID, spanID := logfmtTraceSpan(t, line)
			bodyStart := strings.Index(line, "level=INFO")
			if bodyStart < 0 {
				bodyStart = strings.Index(line, "msg=")
			}
			if bodyStart < 0 {
				t.Fatalf("no logfmt body in %q", line)
			}
			prefix := line[:bodyStart]
			if !strings.Contains(prefix, "child-work") {
				continue
			}
			if !strings.Contains(prefix, traceID) || !strings.Contains(prefix, spanID) {
				t.Fatalf("stream prefix missing joined ids: %q", line)
			}
			joined = true
		}
		if !joined {
			t.Fatalf("expected hydrated stream line for child-work, stdout=%q stderr=%q", res.stdout, res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestHTTPExportingChildJoinsJSONLog(t *testing.T) {
	sweet.Run(t, "HTTP span hydrate appears on the stream with the JSON log", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-export-http-span", "-span-name", "http-work")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}

		joined := false
		for _, line := range strings.Split(res.stdout, "\n") {
			brace := strings.Index(line, "{")
			if brace < 0 || !strings.Contains(line[brace:], `"msg":"http-span"`) {
				continue
			}

			traceID, spanID := jsonTraceSpan(t, line)
			prefix := line[:brace]
			if !strings.Contains(prefix, "http-work") {
				continue
			}
			if !strings.Contains(prefix, traceID) || !strings.Contains(prefix, spanID) {
				t.Fatalf("stream prefix missing joined ids: %q", line)
			}
			joined = true
		}
		if !joined {
			t.Fatalf("expected hydrated stream line for http-work, stdout=%q stderr=%q", res.stdout, res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestStdoutAndOTLPLogBothAppear(t *testing.T) {
	sweet.Run(t, "stdout and OTLP copies of the same body both appear", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--", childBin, "-stdout", "twin-line\n", "-export-otlp-log", "twin-line", "-service-name", "helper-svc")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}

		if strings.Count(res.stdout, "twin-line") < 2 {
			t.Fatalf("expected both copies, stdout=%q stderr=%q", res.stdout, res.stderr)
		}

		var sawStdout, sawOTLP bool
		for _, line := range strings.Split(res.stdout, "\n") {
			if !strings.Contains(line, "twin-line") {
				continue
			}
			if strings.HasPrefix(line, "stdout ") {
				sawStdout = true
			}
			if strings.HasPrefix(line, "otlp ") {
				sawOTLP = true
			}
		}
		if !sawStdout || !sawOTLP {
			t.Fatalf("want stdout and otlp sources, stdout=%q", res.stdout)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestLateFlushWithinIdleJoins(t *testing.T) {
	sweet.Run(t, "detached span after child exit joins within idle window", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir,
			"--drain-idle", "250ms",
			"--drain-timeout", "2s",
			"--", childBin, "-exit", "0", "-detach-export-after", "40ms", "-span-name", "late-work",
		)
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}

		joined := false
		for _, line := range strings.Split(res.stdout, "\n") {
			brace := strings.Index(line, "{")
			if brace < 0 || !strings.Contains(line[brace:], `"msg":"grpc-span"`) {
				continue
			}

			traceID, spanID := jsonTraceSpan(t, line)
			prefix := line[:brace]
			if !strings.Contains(prefix, "late-work") {
				continue
			}
			if !strings.Contains(prefix, traceID) || !strings.Contains(prefix, spanID) {
				t.Fatalf("stream prefix missing joined ids: %q", line)
			}
			joined = true
		}
		if !joined {
			t.Fatalf("expected hydrated late span, stdout=%q stderr=%q", res.stdout, res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestHardCapReturnsWhileOTLPArrives(t *testing.T) {
	sweet.Run(t, "hard cap returns even if a grandchild keeps exporting", wrapFactory, func(t *testing.T, d wrapDeps) {
		start := time.Now()
		res := runPraytel(t, d.dir,
			"--drain-idle", "2s",
			"--drain-timeout", "80ms",
			"--", childBin, "-exit", "7", "-keep-exporting",
		)
		elapsed := time.Since(start)
		if res.exit != 7 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}
		if elapsed > 1500*time.Millisecond {
			t.Fatalf("praytel hung for %s stderr=%q", elapsed, res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestPostDrainExportWarnsAndReturns(t *testing.T) {
	sweet.Run(t, "late OTLP after drain warns and does not hang", wrapFactory, func(t *testing.T, d wrapDeps) {
		start := time.Now()
		res := runPraytel(t, d.dir,
			"--drain-idle", "15ms",
			"--drain-timeout", "80ms",
			"--", childBin, "-exit", "3", "-keep-exporting",
		)
		elapsed := time.Since(start)
		if res.exit != 3 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", res.exit, res.stderr, res.stdout)
		}
		if elapsed > 1500*time.Millisecond {
			t.Fatalf("praytel hung for %s stderr=%q", elapsed, res.stderr)
		}
		if !strings.Contains(strings.ToLower(res.stderr), "dropping otlp after drain") {
			t.Fatalf("expected drop warning, stderr=%q", res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestDrainDoesNotChangeChildExit(t *testing.T) {
	sweet.Run(t, "drain wait keeps the child exit code", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir,
			"--drain-idle", "20ms",
			"--drain-timeout", "100ms",
			"--", childBin, "-exit", "42", "-stdout", "after-drain-exit",
		)
		if res.exit != 42 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "after-drain-exit") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestFocusHidesUntaggedLines(t *testing.T) {
	sweet.Run(t, "--focus that misses orphan hides captured lines", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--focus", "orders", "--", childBin, "-stdout", "noise-line")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Contains(res.stdout, "noise-line") {
			t.Fatalf("unfocused orphan leaked: %q", res.stdout)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestFocusOrphanStillStreams(t *testing.T) {
	sweet.Run(t, "--focus orphan still prints untagged lines", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--focus", "orphan", "--", childBin, "-stdout", "kept-line")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "kept-line") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}
		assertNoStrayFiles(t, d.dir)
	})
}

func TestShedWarnsAndPreservesChildExit(t *testing.T) {
	sweet.Run(t, "shed warns on stderr and keeps the child exit code", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir,
			"--max-log-lines", "1",
			"--", "sh", "-c", "printf 'keep-line\ndrop-line\n'; exit 42",
		)
		if res.exit != 42 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if strings.Count(res.stdout, "keep-line") != 1 {
			t.Fatalf("stdout=%q", res.stdout)
		}
		if strings.Contains(res.stdout, "drop-line") {
			t.Fatalf("shed line printed on stream: %q", res.stdout)
		}
		if strings.Contains(res.stdout, "shed") {
			t.Fatalf("shed warning leaked onto stdout: %q", res.stdout)
		}
		if !strings.Contains(res.stderr, "shed newest inbound data") {
			t.Fatalf("expected shed warning on stderr, got %q", res.stderr)
		}

		assertNoStrayFiles(t, d.dir)
	})
}

func TestSignalToPraytelReachesChild(t *testing.T) {
	cases := []struct {
		name     string
		sig      os.Signal
		wantJSON string
	}{
		{"SIGINT", os.Interrupt, `"signal":"interrupt"`},
		{"SIGTERM", syscall.SIGTERM, `"signal":"terminated"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sweet.Run(t, "signal to praytel is forwarded and child exit is kept", wrapFactory, func(t *testing.T, d wrapDeps) {
				marker := filepath.Join(d.dir, "ready")
				cmd := exec.Command(praytelBin,
					"--grpc-port", strconv.Itoa(mustFreePort(t)),
					"--http-port", strconv.Itoa(mustFreePort(t)),
					"--drain-idle", "5ms",
					"--drain-timeout", "200ms",
					"--", childBin, "-wait-signal", "-exit", "42", "-marker", marker,
				)
				cmd.Dir = d.dir

				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Start(); err != nil {
					t.Fatalf("start praytel: %v", err)
				}

				waitForFile(t, marker)
				if err := cmd.Process.Signal(tc.sig); err != nil {
					t.Fatalf("signal praytel: %v", err)
				}

				err := waitCmd(t, cmd, 10*time.Second)
				res := procResult{stdout: stdout.String(), stderr: stderr.String()}
				if err == nil {
					t.Fatalf("expected child exit 42, got 0 stdout=%q stderr=%q", res.stdout, res.stderr)
				}

				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("wait praytel: %v stderr=%q", err, res.stderr)
				}
				if ee.ExitCode() != 42 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", ee.ExitCode(), res.stdout, res.stderr)
				}

				got, readErr := os.ReadFile(marker)
				if readErr != nil {
					t.Fatalf("read marker: %v", readErr)
				}
				if !strings.Contains(string(got), tc.wantJSON) {
					t.Fatalf("child should report forwarded %s, marker=%q stdout=%q stderr=%q", tc.name, got, res.stdout, res.stderr)
				}

				if err := os.Remove(marker); err != nil {
					t.Fatalf("remove marker: %v", err)
				}
				assertNoStrayFiles(t, d.dir)
			})
		})
	}
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

	prefixed := []string{
		"--grpc-port", strconv.Itoa(grpcPort),
		"--http-port", strconv.Itoa(httpPort),
	}
	if !hasDrainFlags(args) {
		prefixed = append(prefixed, "--drain-idle", "5ms", "--drain-timeout", "200ms")
	}
	prefixed = append(prefixed, args...)

	return runPraytelExact(t, dir, prefixed...)
}

func hasDrainFlags(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--drain-idle" || a == "--drain-timeout" || strings.HasPrefix(a, "--drain-idle=") || strings.HasPrefix(a, "--drain-timeout=") {
			return true
		}
	}

	return false
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

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", path)
}

func waitCmd(t *testing.T, cmd *exec.Cmd, d time.Duration) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(d):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out after %s waiting for praytel", d)
		return nil
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

func logfmtTraceSpan(t *testing.T, line string) (traceID, spanID string) {
	t.Helper()

	for _, start := range []string{"level=", "msg=", "trace_id="} {
		i := strings.Index(line, start)
		if i < 0 {
			continue
		}

		dec := logfmt.NewDecoder(strings.NewReader(line[i:]))
		if !dec.ScanRecord() {
			continue
		}

		var tid, sid string
		for dec.ScanKeyval() {
			key := string(dec.Key())
			val := string(dec.Value())
			switch key {
			case "trace_id":
				tid = val
			case "span_id":
				sid = val
			}
		}
		if tid != "" && sid != "" {
			return tid, sid
		}
	}

	t.Fatalf("missing logfmt ids in %q", line)

	return "", ""
}

func jsonTraceSpan(t *testing.T, line string) (traceID, spanID string) {
	t.Helper()

	i := strings.Index(line, "{")
	if i < 0 {
		t.Fatalf("no json object in %q", line)
	}

	var meta struct {
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
	if err := json.Unmarshal([]byte(line[i:]), &meta); err != nil {
		t.Fatalf("parse json in %q: %v", line, err)
	}
	if meta.TraceID == "" || meta.SpanID == "" {
		t.Fatalf("missing ids in %q", line)
	}

	return meta.TraceID, meta.SpanID
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
