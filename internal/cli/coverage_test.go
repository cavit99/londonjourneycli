package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
	"github.com/cavit99/londonjourneycli/internal/output"
	"github.com/cavit99/londonjourneycli/internal/tfl"
)

func writeTestSkill(t *testing.T, root, name, skillMD, manifest string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunMetaUsageAndGlobalParsing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{name: "implicit help", args: nil, code: exitcode.OK, want: "londonjourneycli - executable contracts"},
		{name: "explicit help", args: []string{"help"}, code: exitcode.OK, want: "tfl journey"},
		{name: "version", args: []string{"version"}, code: exitcode.OK, want: version},
		{name: "unknown", args: []string{"bogus"}, code: exitcode.Usage, want: "unknown command"},
		{name: "missing skills dir", args: []string{"--skills-dir"}, code: exitcode.Usage, want: "--skills-dir requires a value"},
		{name: "missing output", args: []string{"--output"}, code: exitcode.Usage, want: "--output requires a value"},
		{name: "output without json", args: []string{"--output=status", "version"}, code: exitcode.Usage, want: "--output requires --json"},
		{name: "envelope without json", args: []string{"--envelope", "version"}, code: exitcode.Usage, want: "--envelope requires --json"},
		{name: "missing timeout", args: []string{"--timeout"}, code: exitcode.Usage, want: "--timeout requires a duration"},
		{name: "bad timeout", args: []string{"--timeout=not-a-duration", "list"}, code: exitcode.Usage, want: "invalid duration"},
		{name: "tfl usage", args: []string{"tfl"}, code: exitcode.Usage, want: "usage: londonjourneycli tfl"},
		{name: "tfl unknown", args: []string{"tfl", "bogus"}, code: exitcode.Usage, want: "unknown tfl command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errb := runCLI(t, tc.args...)
			if code != tc.code {
				t.Fatalf("code=%d out=%q err=%q", code, out, errb)
			}
			if !strings.Contains(out+errb, tc.want) {
				t.Fatalf("expected %q in out=%q err=%q", tc.want, out, errb)
			}
		})
	}
}

func TestSkillCommandFormatsAndFailurePaths(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "demo", "---\nname: demo\ndescription: Demo transport helper\n---\n", "name: demo\ndescription: Demo manifest\nownerDomain: transport\ncronSafe: true\ntriggers: [route to office]\nrequires:\n  env: [LONDONJOURNEYCLI_TEST_MISSING_ENV]\ncommands:\n  - name: echo\n    exec: [echo, hello]\n  - name: bad-timeout\n    exec: [echo, no]\n    timeout: nope\ntests:\n  - name: ok\n    command: [echo, ok]\n  - name: fail\n    command: [sh, -c, 'echo fail >&2; exit 7']\n")
	writeTestSkill(t, root, "plain", "No frontmatter\n", "")

	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{name: "list human", args: []string{"--skills-dir", root, "list"}, code: exitcode.OK, want: "Demo transport helper"},
		{name: "list plain", args: []string{"--skills-dir", root, "--plain", "list"}, code: exitcode.OK, want: "demo\t"},
		{name: "search human", args: []string{"--skills-dir", root, "search", "office"}, code: exitcode.OK, want: "demo"},
		{name: "search plain", args: []string{"--skills-dir", root, "--plain", "search", "transport"}, code: exitcode.OK, want: "Demo transport helper"},
		{name: "search json", args: []string{"--skills-dir", root, "--json", "search", "transport"}, code: exitcode.OK, want: "\"name\": \"demo\""},
		{name: "search usage", args: []string{"--skills-dir", root, "search"}, code: exitcode.Usage, want: "usage: londonjourneycli search"},
		{name: "show plain", args: []string{"--skills-dir", root, "--plain", "show", "demo"}, code: exitcode.OK, want: "transport\ttrue"},
		{name: "show json", args: []string{"--skills-dir", root, "--json", "show", "demo"}, code: exitcode.OK, want: "\"ownerDomain\": \"transport\""},
		{name: "show json envelope", args: []string{"--skills-dir", root, "--json", "--envelope", "show", "demo"}, code: exitcode.OK, want: "\"schemaVersion\": \"1.0\""},
		{name: "show json envelope command", args: []string{"--skills-dir", root, "--json", "--envelope", "show", "demo"}, code: exitcode.OK, want: "\"command\": ["},
		{name: "show usage", args: []string{"--skills-dir", root, "show"}, code: exitcode.Usage, want: "usage: londonjourneycli show"},
		{name: "show not found", args: []string{"--skills-dir", root, "show", "missing"}, code: exitcode.NoData, want: "skill not found"},
		{name: "lint json", args: []string{"--skills-dir", root, "--json", "lint"}, code: exitcode.OK, want: "missing description"},
		{name: "doctor json missing env", args: []string{"--skills-dir", root, "--json", "doctor", "demo"}, code: exitcode.Config, want: "LONDONJOURNEYCLI_TEST_MISSING_ENV"},
		{name: "doctor envelope missing env", args: []string{"--skills-dir", root, "--json", "--envelope", "doctor", "demo"}, code: exitcode.Config, want: "\"ok\": false"},
		{name: "doctor no manifest", args: []string{"--skills-dir", root, "doctor", "plain"}, code: exitcode.OK, want: "optional manifest not present"},
		{name: "doctor not found", args: []string{"--skills-dir", root, "doctor", "missing"}, code: exitcode.NoData, want: "skill not found"},
		{name: "run missing command", args: []string{"--skills-dir", root, "run", "demo", "missing"}, code: exitcode.NoData, want: "command not found"},
		{name: "run missing manifest", args: []string{"--skills-dir", root, "run", "plain", "x"}, code: exitcode.NoData, want: "skill or manifest not found"},
		{name: "run bad timeout", args: []string{"--skills-dir", root, "run", "demo", "bad-timeout"}, code: exitcode.Config, want: "invalid timeout"},
		{name: "test json failure", args: []string{"--skills-dir", root, "--json", "test", "demo"}, code: exitcode.Generic, want: "\"childExitCode\": 7"},
		{name: "test envelope failure", args: []string{"--skills-dir", root, "--json", "--envelope", "test", "demo"}, code: exitcode.Generic, want: "\"ok\": false"},
		{name: "test usage", args: []string{"--skills-dir", root, "test"}, code: exitcode.Usage, want: "usage: londonjourneycli test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errb := runCLI(t, tc.args...)
			if code != tc.code {
				t.Fatalf("code=%d out=%q err=%q", code, out, errb)
			}
			if !strings.Contains(out+errb, tc.want) {
				t.Fatalf("expected %q in out=%q err=%q", tc.want, out, errb)
			}
		})
	}
}

func TestCLIFormattingHelpers(t *testing.T) {
	if got := appendCSV("", "bus"); got != "bus" {
		t.Fatalf("append empty=%q", got)
	}
	if got := appendCSV("bus", "tube"); got != "bus,tube" {
		t.Fatalf("append existing=%q", got)
	}
	if got := canonicalJourneyPreference("fastest"); got != "LeastTime" {
		t.Fatalf("journey preference=%q", got)
	}
	if got := canonicalJourneyPreference("least-walking"); got != "LeastWalking" {
		t.Fatalf("journey preference=%q", got)
	}
	if got := canonicalJourneyPreference("custom"); got != "custom" {
		t.Fatalf("journey preference=%q", got)
	}
	if got := canonicalWalkingSpeed(""); got != "" {
		t.Fatalf("walking speed=%q", got)
	}
	if got := canonicalWalkingSpeed("average"); got != "Average" {
		t.Fatalf("walking speed=%q", got)
	}
	if got := canonicalWalkingSpeed("fast"); got != "Fast" {
		t.Fatalf("walking speed=%q", got)
	}
	if got := canonicalCyclePreference("take-on-transport"); got != "TakeOnTransport" {
		t.Fatalf("cycle preference=%q", got)
	}
	if got := canonicalCyclePreference("cyclehire"); got != "CycleHire" {
		t.Fatalf("cycle preference=%q", got)
	}
	if got := queryModes("all"); got != nil {
		t.Fatalf("query modes all=%v", got)
	}
	if got := queryModes("bus,tube"); len(got) != 2 || got[1] != "tube" {
		t.Fatalf("query modes csv=%v", got)
	}
	if got := roundMinutes(89); got != 1 {
		t.Fatalf("round minutes=%d", got)
	}
	redacted := redactCommand([]string{"tfl", "watch-arrival", "--openclaw-target", "+15555550123", "-openclaw-target=+15555550124", "--gateway-token=secret"})
	if strings.Contains(strings.Join(redacted, " "), "+15555550123") || strings.Contains(strings.Join(redacted, " "), "+15555550124") || strings.Contains(strings.Join(redacted, " "), "secret") {
		t.Fatalf("command was not redacted: %v", redacted)
	}
	if got := hhmm("not-an-iso-time"); got != "not-an-iso-time" {
		t.Fatalf("hhmm fallback=%q", got)
	}
	if got := resolvedStopLabel(&resolvedStop{ID: "490", Name: " Stop A "}, "fallback"); got != " Stop A " {
		t.Fatalf("resolved label=%q", got)
	}
	if got := resolvedStopLabel(&resolvedStop{ID: "490"}, "fallback"); got != "490" {
		t.Fatalf("resolved id label=%q", got)
	}
	if got := resolvedStopLabel(nil, "fallback"); got != "fallback" {
		t.Fatalf("nil label=%q", got)
	}
}

func TestWriteNextArrivalResultFormats(t *testing.T) {
	arrivalTime := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	next := tfl.Arrival{LineName: "43", DestinationName: "Friern Barnet", StationName: "London Bridge", ExpectedArrival: arrivalTime, TimeToStation: 120}
	result := nextArrivalResult{
		Status:       "ok",
		Message:      "Next 43 is due.",
		Line:         "43",
		ResolvedStop: &resolvedStop{ID: "490000139R", Name: "London Bridge"},
		Next:         &next,
		Arrivals:     []tfl.Arrival{next},
	}
	for _, tc := range []struct {
		name   string
		format output.Format
		want   string
	}{
		{name: "human", format: output.Human, want: "Next 43 is due."},
		{name: "plain", format: output.Plain, want: "ok\tNext 43 is due.\t43\t490000139R\tLondon Bridge\t43\tFriern Barnet\tLondon Bridge\t2026-05-18T12:30:00Z\t120"},
		{name: "json", format: output.JSON, want: "\"status\": \"ok\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			writeNextArrivalResult(globals{format: tc.format}, &out, &errb, result)
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("expected %q in %q", tc.want, out.String())
			}
		})
	}
}

func TestTFLCommandUsageEdges(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{name: "stop info missing stop", args: []string{"tfl", "stop-info"}, code: exitcode.Usage, want: "--stop is required"},
		{name: "stop search missing query", args: []string{"tfl", "stop-search"}, code: exitcode.Usage, want: "usage: londonjourneycli tfl stop-search"},
		{name: "nearby missing coordinates", args: []string{"tfl", "nearby-stops"}, code: exitcode.Usage, want: "--lat and --lon are required"},
		{name: "nearby bad radius", args: []string{"tfl", "nearby-stops", "--lat", "51", "--lon", "-0.1", "--radius", "0"}, code: exitcode.Usage, want: "--radius must be > 0"},
		{name: "nearby bad limit", args: []string{"tfl", "nearby-stops", "--lat", "51", "--lon", "-0.1", "--limit", "0"}, code: exitcode.Usage, want: "--limit must be > 0"},
		{name: "stop search missing flag value", args: []string{"tfl", "stop-search", "London", "--mode"}, code: exitcode.Usage, want: "--mode requires a value"},
		{name: "stop search bad max results", args: []string{"tfl", "stop-search", "London", "--max-results", "bad"}, code: exitcode.Usage, want: "invalid syntax"},
		{name: "stop search negative max results", args: []string{"tfl", "stop-search", "London", "--max-results", "-1"}, code: exitcode.Usage, want: "--max-results must be >= 0"},
		{name: "arrivals missing stop", args: []string{"tfl", "arrivals"}, code: exitcode.Usage, want: "--stop is required"},
		{name: "next arrival missing line", args: []string{"tfl", "next-arrival", "--stop", "490"}, code: exitcode.Usage, want: "--line is required"},
		{name: "next arrival missing stop and query", args: []string{"tfl", "next-arrival", "--line", "43"}, code: exitcode.Usage, want: "provide exactly one"},
		{name: "next arrival bad limit", args: []string{"tfl", "next-arrival", "--stop", "490", "--line", "43", "--limit", "0"}, code: exitcode.Usage, want: "--limit must be > 0"},
		{name: "next arrival bad search limit", args: []string{"tfl", "next-arrival", "--query", "London", "--line", "43", "--search-limit", "0"}, code: exitcode.Usage, want: "--search-limit must be > 0"},
		{name: "journey missing endpoints", args: []string{"tfl", "journey", "--from", "home"}, code: exitcode.Usage, want: "--from and --to are required"},
		{name: "watch missing line", args: []string{"tfl", "watch-arrival", "--stop", "490", "--dry-run"}, code: exitcode.Usage, want: "--line is required"},
		{name: "watch missing stop and query", args: []string{"tfl", "watch-arrival", "--line", "43", "--dry-run"}, code: exitcode.Usage, want: "provide exactly one"},
		{name: "watch bad search limit", args: []string{"tfl", "watch-arrival", "--query", "London", "--line", "43", "--search-limit", "0", "--dry-run"}, code: exitcode.Usage, want: "--search-limit must be > 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errb := runCLI(t, tc.args...)
			if code != tc.code {
				t.Fatalf("code=%d out=%q err=%q", code, out, errb)
			}
			if !strings.Contains(out+errb, tc.want) {
				t.Fatalf("expected %q in out=%q err=%q", tc.want, out, errb)
			}
		})
	}
}

func TestStructuredTfLErrorOutput(t *testing.T) {
	ambiguous := &tfl.APIError{StatusCode: 300, Message: "disambiguation required", Disambiguation: &tfl.DisambiguationResult{ToLocationDisambiguation: &tfl.Disambiguation{DisambiguationOptions: []tfl.DisambiguationOption{{ParameterValue: "1000109"}}}}}
	var out, errb bytes.Buffer
	code, ok := writeStructuredTfLError(globals{format: output.JSON}, &out, &errb, ambiguous)
	if !ok || code != exitcode.Usage || !strings.Contains(out.String(), "\"status\": \"ambiguous\"") {
		t.Fatalf("ambiguous output code=%d ok=%t out=%q err=%q", code, ok, out.String(), errb.String())
	}
	out.Reset()
	code, ok = writeStructuredTfLError(globals{format: output.JSON}, &out, &errb, &tfl.APIError{StatusCode: 500, Message: "bad gateway"})
	if !ok || code != exitcode.Network || !strings.Contains(out.String(), "\"status\": \"api_error\"") {
		t.Fatalf("api output code=%d ok=%t out=%q", code, ok, out.String())
	}
	if _, ok := writeStructuredTfLError(globals{format: output.Human}, &out, &errb, ambiguous); ok {
		t.Fatal("human format should not write structured errors")
	}
	if code := tflErrorExitCode(ambiguous); code != exitcode.Usage {
		t.Fatalf("ambiguous exit=%d", code)
	}
}
