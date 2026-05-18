package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
	"github.com/cavit99/londonjourneycli/internal/notify"
	"github.com/cavit99/londonjourneycli/internal/output"
	"github.com/cavit99/londonjourneycli/internal/skill"
	"github.com/cavit99/londonjourneycli/internal/tfl"
)

const version = "0.3.1"

type globals struct {
	format     output.Format
	envelope   bool
	command    []string
	outputPath string
	skillsDir  string
	timeout    time.Duration
	noInput    bool
	verbose    bool
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	g, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Usage
	}
	if g.outputPath != "" && g.format != output.JSON {
		fmt.Fprintln(stderr, "--output requires --json")
		return exitcode.Usage
	}
	if g.envelope && g.format != output.JSON {
		fmt.Fprintln(stderr, "--envelope requires --json")
		return exitcode.Usage
	}
	if g.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		printHelp(stdout)
		return exitcode.OK
	}
	g.command = redactCommand(rest)
	switch rest[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return exitcode.OK
	case "list":
		return cmdList(ctx, g, rest[1:], stdout, stderr)
	case "search":
		return cmdSearch(ctx, g, rest[1:], stdout, stderr)
	case "show":
		return cmdShow(ctx, g, rest[1:], stdout, stderr)
	case "lint":
		return cmdLint(ctx, g, rest[1:], stdout, stderr)
	case "doctor":
		return cmdDoctor(ctx, g, rest[1:], stdout, stderr)
	case "run":
		return cmdRun(ctx, g, rest[1:], stdout, stderr)
	case "test":
		return cmdTest(ctx, g, rest[1:], stdout, stderr)
	case "tfl":
		return cmdTFL(ctx, g, rest[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", rest[0])
		return exitcode.Usage
	}
}

func parseGlobals(args []string) (globals, []string, error) {
	g := globals{format: output.Human, timeout: 30 * time.Second}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			return g, args[i+1:], nil
		case "--json":
			g.format = output.JSON
		case "--envelope":
			g.envelope = true
		case "--plain":
			g.format = output.Plain
		case "--no-input":
			g.noInput = true
		case "--output":
			i++
			if i >= len(args) {
				return g, nil, errors.New("--output requires a value")
			}
			g.outputPath = args[i]
		case "-v", "--verbose":
			g.verbose = true
		case "--skills-dir":
			i++
			if i >= len(args) {
				return g, nil, errors.New("--skills-dir requires a value")
			}
			g.skillsDir = args[i]
		case "--timeout":
			i++
			if i >= len(args) {
				return g, nil, errors.New("--timeout requires a duration")
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return g, nil, err
			}
			g.timeout = d
		default:
			if strings.HasPrefix(arg, "--skills-dir=") {
				g.skillsDir = strings.TrimPrefix(arg, "--skills-dir=")
				continue
			}
			if strings.HasPrefix(arg, "--timeout=") {
				d, err := time.ParseDuration(strings.TrimPrefix(arg, "--timeout="))
				if err != nil {
					return g, nil, err
				}
				g.timeout = d
				continue
			}
			if strings.HasPrefix(arg, "--output=") {
				g.outputPath = strings.TrimPrefix(arg, "--output=")
				continue
			}
			return g, args[i:], nil
		}
	}
	return g, nil, nil
}

func redactCommand(args []string) []string {
	redacted := append([]string(nil), args...)
	for i := 0; i < len(redacted); i++ {
		name, inlineValue, hasInlineValue := splitFlag(redacted[i])
		switch name {
		case "openclaw-target", "gateway-token":
			if hasInlineValue {
				redacted[i] = inlineValue + "[redacted]"
				continue
			}
			if i+1 < len(redacted) {
				redacted[i+1] = "[redacted]"
			}
		}
	}
	return redacted
}

func splitFlag(arg string) (name, prefix string, hasValue bool) {
	trimmed := strings.TrimLeft(arg, "-")
	if trimmed == arg || trimmed == "" {
		return "", "", false
	}
	if before, _, ok := strings.Cut(trimmed, "="); ok {
		return before, arg[:len(arg)-len(trimmed)] + before + "=", true
	}
	return trimmed, "", false
}

func skillRoots(g globals) []string {
	if g.skillsDir != "" {
		return []string{g.skillsDir}
	}
	var roots []string
	if env := os.Getenv("LONDONJOURNEYCLI_SKILLS_DIR"); env != "" {
		roots = append(roots, filepath.SplitList(env)...)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Join(cwd, "skills"))
		if filepath.Base(cwd) == "skills" {
			roots = append(roots, cwd)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".config", "londonjourneycli", "skills"))
		roots = append(roots, filepath.Join(home, ".local", "share", "londonjourneycli", "skills"))
	}
	return roots
}

func discover(g globals) ([]skill.Skill, error) {
	return skill.Discover(skillRoots(g))
}

func writeJSON(g globals, stdout, stderr io.Writer, value any) int {
	return writeJSONWithOK(g, stdout, stderr, value, true, false)
}

func writeJSONPreservingPayload(g globals, stdout, stderr io.Writer, value any) int {
	return writeJSONWithOK(g, stdout, stderr, value, false, true)
}

func writeJSONWithOK(g globals, stdout, stderr io.Writer, value any, ok bool, preserveOnProjectionError bool) int {
	if g.outputPath != "" {
		projected, err := output.Project(value, g.outputPath)
		if err != nil {
			if preserveOnProjectionError {
				if err := output.WriteJSON(stdout, envelopeJSON(g, value, ok)); err != nil {
					fmt.Fprintln(stderr, err)
					return exitcode.Generic
				}
				return exitcode.OK
			}
			fmt.Fprintln(stderr, err)
			return exitcode.NoData
		}
		value = projected
	}
	if err := output.WriteJSON(stdout, envelopeJSON(g, value, ok)); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	return exitcode.OK
}

func envelopeJSON(g globals, value any, ok bool) any {
	if !g.envelope {
		return value
	}
	return map[string]any{
		"ok":            ok,
		"schemaVersion": "1.0",
		"command":       g.command,
		"requestedAt":   time.Now().UTC().Format(time.RFC3339),
		"data":          value,
	}
}

func cmdList(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	switch g.format {
	case output.JSON:
		return writeJSON(g, stdout, stderr, skills)
	case output.Plain:
		var rows [][]string
		for _, s := range skills {
			rows = append(rows, []string{s.Name, s.Path, s.Description})
		}
		_ = output.WritePlainRows(stdout, rows)
	default:
		for _, s := range skills {
			fmt.Fprintf(stdout, "%-24s %s\n", s.Name, s.Description)
		}
	}
	return exitcode.OK
}

func cmdSearch(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: londonjourneycli search <query>")
		return exitcode.Usage
	}
	query := strings.ToLower(strings.Join(args, " "))
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	var matches []skill.Skill
	for _, s := range skills {
		hay := strings.ToLower(s.Name + " " + s.Description)
		if s.Manifest != nil {
			hay += " " + strings.ToLower(strings.Join(s.Manifest.Triggers, " "))
		}
		if strings.Contains(hay, query) {
			matches = append(matches, s)
		}
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, matches)
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, s := range matches {
			rows = append(rows, []string{s.Name, s.Path, s.Description})
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	for _, s := range matches {
		fmt.Fprintf(stdout, "%-24s %s\n", s.Name, s.Description)
	}
	return exitcode.OK
}

func cmdShow(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: londonjourneycli show <skill>")
		return exitcode.Usage
	}
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	s, ok := skill.Find(skills, args[0])
	if !ok {
		fmt.Fprintf(stderr, "skill not found: %s\n", args[0])
		return exitcode.NoData
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, s)
	}
	if g.format == output.Plain {
		owner := ""
		cronSafe := ""
		if s.Manifest != nil {
			owner = s.Manifest.OwnerDomain
			cronSafe = strconv.FormatBool(s.Manifest.CronSafe)
		}
		_ = output.WritePlainRows(stdout, [][]string{{s.Name, s.Path, s.Description, owner, cronSafe}})
		return exitcode.OK
	}
	fmt.Fprintf(stdout, "%s\n%s\n\nPath: %s\n", s.Name, s.Description, s.Path)
	if s.Manifest != nil {
		fmt.Fprintf(stdout, "Owner: %s\nCron safe: %t\n", s.Manifest.OwnerDomain, s.Manifest.CronSafe)
	}
	return exitcode.OK
}

func cmdLint(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	issues := skill.Lint(skills)
	status := exitcode.OK
	for _, issue := range issues {
		if issue.Severity == "error" {
			status = exitcode.Generic
			break
		}
	}
	if g.format == output.JSON {
		if code := writeJSONWithOK(g, stdout, stderr, issues, status == exitcode.OK, false); code != exitcode.OK {
			return code
		}
	} else if g.format == output.Plain {
		var rows [][]string
		for _, issue := range issues {
			rows = append(rows, []string{issue.Severity, issue.Skill, issue.Path, issue.Message})
		}
		_ = output.WritePlainRows(stdout, rows)
	} else if len(issues) == 0 {
		fmt.Fprintln(stdout, "lint clean")
	} else {
		for _, issue := range issues {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", issue.Severity, issue.Skill, issue.Message)
		}
	}
	return status
}

func cmdDoctor(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: londonjourneycli doctor <skill>")
		return exitcode.Usage
	}
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	s, ok := skill.Find(skills, args[0])
	if !ok {
		fmt.Fprintf(stderr, "skill not found: %s\n", args[0])
		return exitcode.NoData
	}
	checks := skill.Doctor(s)
	status := exitcode.OK
	for _, c := range checks {
		if !c.OK {
			status = exitcode.Config
			break
		}
	}
	if g.format == output.JSON {
		if code := writeJSONWithOK(g, stdout, stderr, checks, status == exitcode.OK, false); code != exitcode.OK {
			return code
		}
	} else {
		for _, c := range checks {
			checkStatus := "ok"
			if !c.OK {
				checkStatus = "fail"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", checkStatus, c.Name, c.Message)
		}
	}
	return status
}

func cmdRun(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: londonjourneycli run <skill> <command> [args...]")
		return exitcode.Usage
	}
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	s, ok := skill.Find(skills, args[0])
	if !ok || s.Manifest == nil {
		fmt.Fprintf(stderr, "skill or manifest not found: %s\n", args[0])
		return exitcode.NoData
	}
	c, ok := s.Manifest.Command(args[1])
	if !ok {
		fmt.Fprintf(stderr, "command not found: %s\n", args[1])
		return exitcode.NoData
	}
	argv := append([]string{}, c.Exec...)
	argv = append(argv, args[2:]...)
	timeout, err := parseOptionalDuration(c.Timeout)
	if err != nil {
		fmt.Fprintf(stderr, "invalid timeout for command %s: %v\n", c.Name, err)
		return exitcode.Config
	}
	return runArgv(ctx, argv, runOptions{Timeout: timeout, NoInput: g.noInput}, stdout, stderr)
}

func cmdTest(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: londonjourneycli test <skill>")
		return exitcode.Usage
	}
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	s, ok := skill.Find(skills, args[0])
	if !ok || s.Manifest == nil {
		fmt.Fprintf(stderr, "skill or manifest not found: %s\n", args[0])
		return exitcode.NoData
	}
	if g.format == output.JSON || g.format == output.Plain {
		var results []testResult
		status := exitcode.OK
		for _, tc := range s.Manifest.Tests {
			result := runArgvCapture(ctx, tc.Command, runOptions{NoInput: g.noInput})
			results = append(results, testResult{
				Name:          tc.Name,
				Command:       tc.Command,
				ExitCode:      result.Code,
				ChildExitCode: result.ChildExitCode,
				Stdout:        result.Stdout,
				Stderr:        result.Stderr,
				OK:            result.Code == exitcode.OK,
			})
			if result.Code != exitcode.OK && status == exitcode.OK {
				status = result.Code
			}
		}
		if g.format == output.Plain {
			var rows [][]string
			for _, result := range results {
				rows = append(rows, []string{
					result.Name,
					strconv.FormatBool(result.OK),
					strconv.Itoa(result.ExitCode),
					strconv.Itoa(result.ChildExitCode),
					result.Stdout,
					result.Stderr,
				})
			}
			_ = output.WritePlainRows(stdout, rows)
			return status
		}
		if code := writeJSONWithOK(g, stdout, stderr, results, status == exitcode.OK, false); code != exitcode.OK {
			return code
		}
		return status
	}
	for _, tc := range s.Manifest.Tests {
		fmt.Fprintf(stderr, "test %s\n", tc.Name)
		if code := runArgv(ctx, tc.Command, runOptions{NoInput: g.noInput}, stdout, stderr); code != exitcode.OK {
			return code
		}
	}
	return exitcode.OK
}

type runOptions struct {
	Timeout time.Duration
	NoInput bool
}

type capturedRun struct {
	Code          int
	ChildExitCode int
	Stdout        string
	Stderr        string
}

type testResult struct {
	Name          string   `json:"name"`
	Command       []string `json:"command"`
	ExitCode      int      `json:"exitCode"`
	ChildExitCode int      `json:"childExitCode,omitempty"`
	Stdout        string   `json:"stdout,omitempty"`
	Stderr        string   `json:"stderr,omitempty"`
	OK            bool     `json:"ok"`
}

func parseOptionalDuration(value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

func runArgv(ctx context.Context, argv []string, opts runOptions, stdout, stderr io.Writer) int {
	return runArgvWithIO(ctx, argv, opts, stdout, stderr).Code
}

func runArgvCapture(ctx context.Context, argv []string, opts runOptions) capturedRun {
	var stdout, stderr bytes.Buffer
	result := runArgvWithIO(ctx, argv, opts, &stdout, &stderr)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	return result
}

func runArgvWithIO(ctx context.Context, argv []string, opts runOptions, stdout, stderr io.Writer) capturedRun {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "empty argv")
		return capturedRun{Code: exitcode.Usage}
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	if opts.NoInput {
		cmd.Env = append(os.Environ(), "LONDONJOURNEYCLI_NO_INPUT=1")
	}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fmt.Fprintf(stderr, "command timed out: %s\n", argv[0])
			return capturedRun{Code: exitcode.Generic}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return capturedRun{Code: exitcode.Generic, ChildExitCode: exitErr.ExitCode()}
		}
		fmt.Fprintln(stderr, err)
		return capturedRun{Code: exitcode.Generic}
	}
	return capturedRun{Code: exitcode.OK}
}

func cmdTFL(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: londonjourneycli tfl <status|disruptions|line-routes|nearby-stops|stop-search|stop-info|arrivals|next-arrival|journey|watch-arrival>")
		return exitcode.Usage
	}
	client := tfl.NewClient(os.Getenv("TFL_APP_KEY"))
	if base := os.Getenv("TFL_BASE_URL"); base != "" {
		client.BaseURL = base
	}
	switch args[0] {
	case "status":
		return tflStatus(ctx, g, client, args[1:], stdout, stderr)
	case "disruptions":
		return tflDisruptions(ctx, g, client, args[1:], stdout, stderr)
	case "line-routes":
		return tflLineRoutes(ctx, g, client, args[1:], stdout, stderr)
	case "nearby-stops":
		return tflNearbyStops(ctx, g, client, args[1:], stdout, stderr)
	case "stop-search":
		return tflStopSearch(ctx, g, client, args[1:], stdout, stderr)
	case "stop-info":
		return tflStopInfo(ctx, g, client, args[1:], stdout, stderr)
	case "arrivals":
		return tflArrivals(ctx, g, client, args[1:], stdout, stderr)
	case "next-arrival":
		return tflNextArrival(ctx, g, client, args[1:], stdout, stderr)
	case "journey":
		return tflJourney(ctx, g, client, args[1:], stdout, stderr)
	case "watch-arrival":
		return tflWatchArrival(ctx, g, client, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown tfl command %q\n", args[0])
		return exitcode.Usage
	}
}

func tflStatus(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	line := fs.String("line", "", "comma-separated TfL line IDs")
	mode := fs.String("mode", "", "comma-separated modes; default tube,dlr,elizabeth-line,overground,tram")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	statuses, err := client.LineStatus(ctx, csvArgs(*line), csvArgs(*mode))
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if statuses == nil {
		statuses = []tfl.LineStatus{}
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, statuses)
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, statusLine := range statuses {
			if len(statusLine.LineStatuses) == 0 {
				rows = append(rows, []string{statusLine.ID, statusLine.Name, statusLine.ModeName, "", "", ""})
				continue
			}
			for _, status := range statusLine.LineStatuses {
				rows = append(rows, []string{statusLine.ID, statusLine.Name, statusLine.ModeName, strconv.Itoa(status.StatusSeverity), status.StatusSeverityDescription, status.Reason})
			}
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	for _, statusLine := range statuses {
		if len(statusLine.LineStatuses) == 0 {
			fmt.Fprintf(stdout, "%s: status unavailable\n", statusLine.Name)
			continue
		}
		for _, status := range statusLine.LineStatuses {
			fmt.Fprintf(stdout, "%s: %s", statusLine.Name, status.StatusSeverityDescription)
			if status.Reason != "" {
				fmt.Fprintf(stdout, " - %s", status.Reason)
			}
			fmt.Fprintln(stdout)
		}
	}
	return exitcode.OK
}

func tflDisruptions(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("disruptions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	line := fs.String("line", "", "comma-separated TfL line IDs")
	mode := fs.String("mode", "", "comma-separated modes; default tube,dlr,elizabeth-line,overground,tram")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	disruptions, err := client.LineDisruptions(ctx, csvArgs(*line), csvArgs(*mode))
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if disruptions == nil {
		disruptions = []tfl.Disruption{}
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, disruptions)
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, d := range disruptions {
			rows = append(rows, []string{d.LineID, d.LineName, d.Category, d.Type, disruptionText(d)})
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	if len(disruptions) == 0 {
		fmt.Fprintln(stdout, "No active disruptions found.")
		return exitcode.OK
	}
	for _, d := range disruptions {
		label := d.LineName
		if label == "" {
			label = d.LineID
		}
		if label == "" {
			label = d.Category
		}
		fmt.Fprintf(stdout, "%s: %s\n", label, disruptionText(d))
	}
	return exitcode.OK
}

func tflLineRoutes(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("line-routes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	line := fs.String("line", "", "comma-separated TfL line IDs")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	lines := csvArgs(*line)
	if len(lines) == 0 {
		fmt.Fprintln(stderr, "--line is required")
		return exitcode.Usage
	}
	routes, err := client.LineRoutes(ctx, lines)
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if routes == nil {
		routes = []tfl.LineRoute{}
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, routes)
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, routeLine := range routes {
			for _, section := range routeLine.RouteSections {
				rows = append(rows, []string{routeLine.ID, routeLine.Name, routeLine.ModeName, section.Direction, section.OriginationName, section.DestinationName, section.Originator, section.Destination, section.ServiceType})
			}
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	if len(routes) == 0 {
		fmt.Fprintln(stdout, "No line routes found.")
		return exitcode.OK
	}
	for _, routeLine := range routes {
		fmt.Fprintf(stdout, "%s (%s)\n", routeLine.Name, routeLine.ModeName)
		for _, section := range routeLine.RouteSections {
			fmt.Fprintf(stdout, "  %s: %s -> %s\n", section.Direction, section.OriginationName, section.DestinationName)
		}
	}
	return exitcode.OK
}

func tflNearbyStops(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nearby-stops", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lat := fs.Float64("lat", 0, "latitude")
	lon := fs.Float64("lon", 0, "longitude")
	location := fs.String("location", "", "location text, map link, geo URI, or lat,lon")
	radius := fs.Int("radius", 500, "search radius in metres")
	mode := fs.String("mode", "bus", "comma-separated modes")
	stopTypes := fs.String("stop-type", "NaptanPublicBusCoachTram", "comma-separated TfL stop types")
	limit := fs.Int("limit", 10, "maximum stops")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	latSeen := flagSeen(fs, "lat")
	lonSeen := flagSeen(fs, "lon")
	if strings.TrimSpace(*location) != "" {
		if latSeen || lonSeen {
			fmt.Fprintln(stderr, "--location cannot be combined with --lat or --lon")
			return exitcode.Usage
		}
		parsedLat, parsedLon, err := parseLocationArg(*location)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitcode.Usage
		}
		*lat = parsedLat
		*lon = parsedLon
	} else if !latSeen || !lonSeen {
		fmt.Fprintln(stderr, "provide --location or both --lat and --lon")
		return exitcode.Usage
	}
	if err := validateLatLon(*lat, *lon); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Usage
	}
	if *radius <= 0 {
		fmt.Fprintln(stderr, "--radius must be > 0")
		return exitcode.Usage
	}
	if *limit <= 0 {
		fmt.Fprintln(stderr, "--limit must be > 0")
		return exitcode.Usage
	}
	stops, err := client.NearbyStops(ctx, tfl.NearbyStopOptions{Lat: *lat, Lon: *lon, Radius: *radius, Modes: csvArgs(*mode), StopTypes: csvArgs(*stopTypes), Limit: *limit})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if stops == nil {
		stops = []tfl.StopPoint{}
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, stops)
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, stop := range stops {
			rows = append(rows, []string{stop.ID, stop.CommonName, stop.Indicator, stop.StopLetter, fmt.Sprintf("%.0f", stopDistanceMeters(stop)), fmt.Sprintf("%.5f", stop.Lat), fmt.Sprintf("%.5f", stop.Lon), strings.Join(stop.Modes, ",")})
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	if len(stops) == 0 {
		fmt.Fprintln(stdout, "No nearby stops found.")
		return exitcode.OK
	}
	for _, stop := range stops {
		label := strings.TrimSpace(strings.Join([]string{stop.CommonName, stop.Indicator, stop.StopLetter}, " "))
		fmt.Fprintf(stdout, "%s\t%s\t%.0fm\n", stop.ID, label, stopDistanceMeters(stop))
	}
	return exitcode.OK
}

func stopDistanceMeters(stop tfl.StopPoint) float64 {
	if stop.Distance == nil {
		return 0
	}
	return *stop.Distance
}

var (
	coordPairPattern         = regexp.MustCompile(`([-+]?\d+(?:\.\d+)?)\s*,\s*([-+]?\d+(?:\.\d+)?)`)
	coordPairAnchoredPattern = regexp.MustCompile(`^\s*([-+]?\d+(?:\.\d+)?)\s*,\s*([-+]?\d+(?:\.\d+)?)\s*$`)
	latLabelPattern          = regexp.MustCompile(`(?i)\b(?:LocationLat|latitude|lat)\s*[:=]\s*([-+]?\d+(?:\.\d+)?)`)
	lonLabelPattern          = regexp.MustCompile(`(?i)\b(?:LocationLon|longitude|lon|lng)\s*[:=]\s*([-+]?\d+(?:\.\d+)?)`)
)

func parseLocationArg(value string) (float64, float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, 0, errors.New("--location cannot be empty")
	}
	if lat, lon, ok := parseLabelledLocation(trimmed); ok {
		return lat, lon, validateLatLon(lat, lon)
	}
	if lat, lon, ok := parseOpenClawLocationText(trimmed); ok {
		return lat, lon, validateLatLon(lat, lon)
	}
	if lat, lon, ok := parseMapURLLocation(trimmed); ok {
		return lat, lon, validateLatLon(lat, lon)
	}
	if lat, lon, ok := parseStrictCommaLocation(trimmed); ok {
		return lat, lon, validateLatLon(lat, lon)
	}
	if lat, lon, ok := parseSpaceLocation(trimmed); ok {
		return lat, lon, validateLatLon(lat, lon)
	}
	return 0, 0, fmt.Errorf("could not parse --location as coordinates, geo URI, or map link: %q", trimmed)
}

func parseLabelledLocation(value string) (float64, float64, bool) {
	latMatch := latLabelPattern.FindStringSubmatch(value)
	lonMatch := lonLabelPattern.FindStringSubmatch(value)
	if len(latMatch) < 2 || len(lonMatch) < 2 {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(latMatch[1], 64)
	lon, lonErr := strconv.ParseFloat(lonMatch[1], 64)
	if latErr != nil || lonErr != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func parseOpenClawLocationText(value string) (float64, float64, bool) {
	trimmed := strings.TrimSpace(value)
	for _, prefix := range []string{"📍", "🛰 Live location:"} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if idx := strings.Index(rest, "±"); idx >= 0 {
			rest = strings.TrimSpace(rest[:idx])
		}
		return parseStrictCommaLocation(rest)
	}
	return 0, 0, false
}

func parseMapURLLocation(value string) (float64, float64, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" {
		return 0, 0, false
	}
	if strings.EqualFold(parsed.Scheme, "geo") {
		for _, key := range []string{"q", "query"} {
			if raw := parsed.Query().Get(key); raw != "" {
				if lat, lon, ok := parseCommaLocation(raw); ok {
					return lat, lon, true
				}
			}
		}
		return parseCommaLocation(parsed.Opaque)
	}
	query := parsed.Query()
	for _, key := range []string{"q", "query", "ll", "center", "destination", "origin", "daddr", "saddr"} {
		if raw := query.Get(key); raw != "" {
			if lat, lon, ok := parseCommaLocation(raw); ok {
				return lat, lon, true
			}
		}
	}
	if !isKnownCoordinateMapHost(parsed.Host) {
		return 0, 0, false
	}
	decoded, err := url.QueryUnescape(value)
	if err == nil {
		if lat, lon, ok := parseCommaLocation(decoded); ok {
			return lat, lon, true
		}
	}
	return parseCommaLocation(value)
}

func isKnownCoordinateMapHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	return host == "maps.apple.com" ||
		strings.HasSuffix(host, ".maps.apple.com") ||
		host == "google.com" ||
		strings.HasSuffix(host, ".google.com") ||
		host == "goo.gl" ||
		strings.HasSuffix(host, ".goo.gl")
}

func parseStrictCommaLocation(value string) (float64, float64, bool) {
	match := coordPairAnchoredPattern.FindStringSubmatch(value)
	if len(match) < 3 {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(match[1], 64)
	lon, lonErr := strconv.ParseFloat(match[2], 64)
	if latErr != nil || lonErr != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func parseCommaLocation(value string) (float64, float64, bool) {
	match := coordPairPattern.FindStringSubmatch(value)
	if len(match) < 3 {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(match[1], 64)
	lon, lonErr := strconv.ParseFloat(match[2], 64)
	if latErr != nil || lonErr != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func parseSpaceLocation(value string) (float64, float64, bool) {
	if strings.ContainsAny(value, ":/?&=") {
		return 0, 0, false
	}
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSuffix(parts[0], ","), 64)
	lon, lonErr := strconv.ParseFloat(strings.TrimSuffix(parts[1], ","), 64)
	if latErr != nil || lonErr != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func validateLatLon(lat, lon float64) error {
	if lat < -90 || lat > 90 {
		return fmt.Errorf("latitude must be between -90 and 90, got %.6f", lat)
	}
	if lon < -180 || lon > 180 {
		return fmt.Errorf("longitude must be between -180 and 180, got %.6f", lon)
	}
	return nil
}

func flagSeen(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func tflStopInfo(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stop-info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stop := fs.String("stop", "", "TfL stop ID")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *stop == "" {
		fmt.Fprintln(stderr, "--stop is required")
		return exitcode.Usage
	}
	info, err := client.StopPoint(ctx, *stop)
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, info)
	}
	if g.format == output.Plain {
		rows := [][]string{{info.ID, info.CommonName, info.Indicator, info.StopLetter, fmt.Sprintf("%.5f", info.Lat), fmt.Sprintf("%.5f", info.Lon), strings.Join(info.Modes, ",")}}
		for _, child := range info.Children {
			rows = append(rows, []string{child.ID, child.CommonName, child.Indicator, child.StopLetter, fmt.Sprintf("%.5f", child.Lat), fmt.Sprintf("%.5f", child.Lon), strings.Join(child.Modes, ",")})
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", info.ID, info.CommonName, info.Indicator, info.StopLetter)
	for _, child := range info.Children {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", child.ID, child.CommonName, child.Indicator, child.StopLetter)
	}
	return exitcode.OK
}

func tflStopSearch(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	limit := 10
	maxResults := 0
	modes := ""
	lines := ""
	includeHubs := false
	var queryParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit", "--max-results", "--mode", "--line":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "%s requires a value\n", args[i])
				return exitcode.Usage
			}
			value := args[i+1]
			i++
			switch args[i-1] {
			case "--limit":
				n, err := strconv.Atoi(value)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return exitcode.Usage
				}
				limit = n
			case "--max-results":
				n, err := strconv.Atoi(value)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return exitcode.Usage
				}
				maxResults = n
			case "--mode":
				modes = appendCSV(modes, value)
			case "--line":
				lines = appendCSV(lines, value)
			}
		case "--include-hubs":
			includeHubs = true
		default:
			queryParts = append(queryParts, args[i])
		}
	}
	if limit < 0 {
		fmt.Fprintln(stderr, "--limit must be >= 0")
		return exitcode.Usage
	}
	if maxResults < 0 {
		fmt.Fprintln(stderr, "--max-results must be >= 0")
		return exitcode.Usage
	}
	if len(queryParts) == 0 {
		fmt.Fprintln(stderr, "usage: londonjourneycli tfl stop-search <query> [--mode bus,tube] [--line N] [--limit N]")
		return exitcode.Usage
	}
	resp, err := client.StopSearchWithOptions(ctx, strings.Join(queryParts, " "), tfl.StopSearchOptions{Modes: csvArgs(modes), Lines: csvArgs(lines), MaxResults: maxResults, IncludeHubs: includeHubs})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if limit < len(resp.Matches) {
		resp.Matches = resp.Matches[:limit]
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, resp)
	}
	for _, m := range resp.Matches {
		fmt.Fprintf(stdout, "%s\t%s\t%.5f\t%.5f\n", m.ID, m.Name, m.Lat, m.Lon)
	}
	return exitcode.OK
}

func tflArrivals(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("arrivals", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stop := fs.String("stop", "", "TfL stop ID")
	query := fs.String("query", "", "stop/station search query")
	line := fs.String("line", "", "line filter")
	towards := fs.String("towards", "", "towards/destination substring")
	direction := fs.String("direction", "", "line-arrivals direction: inbound, outbound, or all")
	destinationStop := fs.String("destination-stop", "", "TfL destination stop ID for line-arrivals filtering")
	mode := fs.String("mode", "bus", "stop-search modes for --query, or all")
	searchLimit := fs.Int("search-limit", 5, "maximum stop-search candidates for --query")
	limit := fs.Int("limit", 5, "maximum arrivals")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if (*stop == "") == (*query == "") {
		fmt.Fprintln(stderr, "provide exactly one of --stop or --query")
		return exitcode.Usage
	}
	if *limit < 0 {
		fmt.Fprintln(stderr, "--limit must be >= 0")
		return exitcode.Usage
	}
	if *query != "" && *limit == 0 {
		fmt.Fprintln(stderr, "--limit must be > 0 with --query")
		return exitcode.Usage
	}
	if *query != "" && *searchLimit <= 0 {
		fmt.Fprintln(stderr, "--search-limit must be > 0")
		return exitcode.Usage
	}
	found, err := findArrivals(ctx, client, arrivalLookup{
		StopID:          *stop,
		Query:           *query,
		Lines:           csvArgs(*line),
		Towards:         *towards,
		Direction:       *direction,
		DestinationStop: *destinationStop,
		SearchModes:     queryModes(*mode),
		SearchLimit:     *searchLimit,
	})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if !found.StopFound {
		result := nextArrivalResult{Status: "stop_not_found", Message: fmt.Sprintf("TfL found no stop matching %q.", *query), Line: *line, Query: found.Query, Candidates: found.Candidates, Arrivals: []tfl.Arrival{}}
		if code := writeNextArrivalResult(g, stdout, stderr, result); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}
	arrivals := found.Arrivals
	if *limit < len(arrivals) {
		arrivals = arrivals[:*limit]
	}
	if arrivals == nil {
		arrivals = []tfl.Arrival{}
	}
	if *query != "" && g.format == output.JSON {
		status := "no_data"
		message := fmt.Sprintf("TfL found %d candidate stops for %q, but no matching arrivals.", found.Candidates, found.Query)
		code := exitcode.NoData
		if len(arrivals) > 0 {
			status = "ok"
			message = fmt.Sprintf("TfL found %d arrivals from %s.", len(arrivals), resolvedStopLabel(found.Stop, found.Query))
			code = exitcode.OK
		}
		result := nextArrivalResult{Status: status, Message: message, Line: *line, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrivals: arrivals}
		if len(arrivals) > 0 {
			next := arrivals[0]
			result.Next = &next
		}
		if writeCode := writeNextArrivalResult(g, stdout, stderr, result); writeCode != exitcode.OK {
			return writeCode
		}
		return code
	}
	if *query != "" && len(arrivals) == 0 {
		if g.format == output.Plain {
			_ = output.WritePlainRows(stdout, [][]string{{"no_data", fmt.Sprintf("TfL found %d candidate stops for %q, but no matching arrivals.", found.Candidates, found.Query)}})
		} else {
			fmt.Fprintf(stdout, "TfL found %d candidate stops for %q, but no matching arrivals.\n", found.Candidates, found.Query)
		}
		return exitcode.NoData
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, arrivals)
	}
	for _, a := range arrivals {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%d min\t%s\n", a.LineName, a.DestinationName, a.PlatformName, roundMinutes(a.TimeToStation), a.ExpectedArrival.Local().Format("15:04"))
	}
	return exitcode.OK
}

type resolvedStop struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Lat      float64  `json:"lat,omitempty"`
	Lon      float64  `json:"lon,omitempty"`
	Modes    []string `json:"modes,omitempty"`
	ParentID string   `json:"topMostParentId,omitempty"`
}

type nextArrivalResult struct {
	Status       string        `json:"status"`
	Message      string        `json:"message"`
	Line         string        `json:"line,omitempty"`
	Query        string        `json:"query,omitempty"`
	Candidates   int           `json:"candidates,omitempty"`
	ResolvedStop *resolvedStop `json:"resolvedStop,omitempty"`
	Next         *tfl.Arrival  `json:"next,omitempty"`
	Arrivals     []tfl.Arrival `json:"arrivals"`
}

type tflErrorResult struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Error   *tfl.APIError `json:"error,omitempty"`
}

func writeStructuredTfLError(g globals, stdout, stderr io.Writer, err error) (int, bool) {
	if g.format != output.JSON {
		return 0, false
	}
	var apiErr *tfl.APIError
	if !errors.As(err, &apiErr) {
		if writeCode := writeJSONPreservingPayload(g, stdout, stderr, tflErrorResult{Status: "api_failed", Message: err.Error()}); writeCode != exitcode.OK {
			return writeCode, true
		}
		return exitcode.Network, true
	}
	status := "api_error"
	message := err.Error()
	code := exitcode.Network
	if apiErr.StatusCode == http.StatusMultipleChoices && apiErr.Disambiguation != nil {
		status = "ambiguous"
		message = "TfL needs a more specific place. Resolve the origin, destination, or via point to a postcode, station/stop ID, coordinates, or exact address, then retry."
		code = exitcode.Usage
	}
	if writeCode := writeJSONPreservingPayload(g, stdout, stderr, tflErrorResult{Status: status, Message: message, Error: apiErr}); writeCode != exitcode.OK {
		return writeCode, true
	}
	return code, true
}

func tflErrorExitCode(err error) int {
	var apiErr *tfl.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusMultipleChoices && apiErr.Disambiguation != nil {
		return exitcode.Usage
	}
	return exitcode.Network
}

func tflNextArrival(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("next-arrival", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stop := fs.String("stop", "", "TfL stop ID")
	query := fs.String("query", "", "stop/station search query")
	line := fs.String("line", "", "line filter")
	towards := fs.String("towards", "", "towards/destination substring")
	direction := fs.String("direction", "", "line-arrivals direction: inbound, outbound, or all")
	destinationStop := fs.String("destination-stop", "", "TfL destination stop ID for line-arrivals filtering")
	mode := fs.String("mode", "bus", "stop-search modes for --query, or all")
	searchLimit := fs.Int("search-limit", 5, "maximum stop-search candidates for --query")
	limit := fs.Int("limit", 3, "maximum arrivals")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *line == "" {
		fmt.Fprintln(stderr, "--line is required")
		return exitcode.Usage
	}
	if (*stop == "") == (*query == "") {
		fmt.Fprintln(stderr, "provide exactly one of --stop or --query")
		return exitcode.Usage
	}
	if *limit <= 0 {
		fmt.Fprintln(stderr, "--limit must be > 0")
		return exitcode.Usage
	}
	if *query != "" && *searchLimit <= 0 {
		fmt.Fprintln(stderr, "--search-limit must be > 0")
		return exitcode.Usage
	}

	found, err := findArrivals(ctx, client, arrivalLookup{
		StopID:          *stop,
		Query:           *query,
		Lines:           csvArgs(*line),
		Towards:         *towards,
		Direction:       *direction,
		DestinationStop: *destinationStop,
		SearchModes:     queryModes(*mode),
		SearchLimit:     *searchLimit,
	})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return tflErrorExitCode(err)
	}
	arrivals := found.Arrivals
	if *limit < len(found.Arrivals) {
		arrivals = found.Arrivals[:*limit]
	}
	if arrivals == nil {
		arrivals = []tfl.Arrival{}
	}
	result := nextArrivalResult{Status: "no_data", Line: *line, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrivals: arrivals}
	if !found.StopFound {
		result.Status = "stop_not_found"
		result.Message = fmt.Sprintf("TfL found no stop matching %q.", *query)
		if code := writeNextArrivalResult(g, stdout, stderr, result); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}
	stopLabel := "selected stop"
	if found.Stop != nil {
		stopLabel = found.Stop.Name
		if stopLabel == "" {
			stopLabel = found.Stop.ID
		}
	}
	if len(arrivals) == 0 {
		if found.Query != "" {
			result.Message = fmt.Sprintf("TfL found %d candidate stops for %q, but no matching %s arrivals.", found.Candidates, found.Query, *line)
		} else {
			result.Message = fmt.Sprintf("TfL shows no matching %s arrivals from %s.", *line, stopLabel)
		}
		if code := writeNextArrivalResult(g, stdout, stderr, result); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}
	next := arrivals[0]
	result.Status = "ok"
	result.Next = &next
	result.Message = fmt.Sprintf("Next %s from %s is about %d min away towards %s.", next.LineName, stopLabel, roundMinutes(next.TimeToStation), next.DestinationName)
	if code := writeNextArrivalResult(g, stdout, stderr, result); code != exitcode.OK {
		return code
	}
	return exitcode.OK
}

func tflJourney(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("journey", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "origin")
	to := fs.String("to", "", "destination")
	date := fs.String("date", "", "YYYYMMDD")
	when := fs.String("time", "", "HHmm")
	arriving := fs.Bool("arriving", false, "treat time as arrival time")
	via := fs.String("via", "", "optional via point")
	preference := fs.String("preference", "LeastTime", "LeastTime, LeastInterchange, or LeastWalking")
	modes := fs.String("mode", "", "comma-separated modes, e.g. tube,elizabeth-line,bus")
	accessibility := fs.String("accessibility", "", "comma-separated accessibility preferences")
	maxTransfer := fs.String("max-transfer-minutes", "", "maximum transfer walking minutes")
	maxWalking := fs.String("max-walking-minutes", "", "maximum journey walking minutes")
	walkingSpeed := fs.String("walking-speed", "", "Slow, Average, or Fast")
	cyclePreference := fs.String("cycle-preference", "", "TfL cycle preference")
	includeAlternatives := fs.Bool("include-alternatives", false, "include alternative public transport routes")
	alternativeWalking := fs.Bool("alternative-walking", false, "include alternative walking journey")
	alternativeCycle := fs.Bool("alternative-cycle", false, "include alternative cycling journey")
	realTime := fs.Bool("real-time", false, "request real-time live arrivals where available")
	betweenEntrances := fs.Bool("between-entrances", false, "include station entrance/platform routing")
	localOnly := fs.Bool("local-only", false, "disable TfL nationalSearch")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *from == "" || *to == "" {
		fmt.Fprintln(stderr, "--from and --to are required")
		return exitcode.Usage
	}
	resp, err := client.Journey(ctx, *from, *to, tfl.JourneyOptions{
		Date:                     *date,
		Time:                     *when,
		Arriving:                 *arriving,
		Via:                      *via,
		Preference:               canonicalJourneyPreference(*preference),
		Modes:                    csvArgs(*modes),
		AccessibilityPreferences: csvArgs(*accessibility),
		MaxTransferMinutes:       *maxTransfer,
		MaxWalkingMinutes:        *maxWalking,
		WalkingSpeed:             canonicalWalkingSpeed(*walkingSpeed),
		CyclePreference:          canonicalCyclePreference(*cyclePreference),
		IncludeAlternativeRoutes: *includeAlternatives,
		AlternativeWalking:       *alternativeWalking,
		AlternativeCycle:         *alternativeCycle,
		UseRealTimeLiveArrivals:  *realTime,
		RouteBetweenEntrances:    *betweenEntrances,
		LocalOnly:                *localOnly,
	})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return tflErrorExitCode(err)
	}
	if g.format == output.JSON {
		return writeJSON(g, stdout, stderr, resp)
	}
	if g.format == output.Plain {
		var rows [][]string
		for i, j := range resp.Journeys {
			if i >= 3 {
				break
			}
			option := strconv.Itoa(i + 1)
			rows = append(rows, []string{option, "journey", hhmm(j.StartDateTime), hhmm(j.ArrivalDateTime), strconv.Itoa(j.Duration), "", "", ""})
			for _, leg := range j.Legs {
				route := ""
				if len(leg.RouteOptions) > 0 {
					route = leg.RouteOptions[0].Name
				}
				rows = append(rows, []string{option, "leg", hhmm(leg.DepartureTime), hhmm(leg.ArrivalTime), strconv.Itoa(leg.Duration), strings.ToUpper(leg.Mode.Name), route, leg.DeparturePoint.CommonName + " -> " + leg.ArrivalPoint.CommonName})
			}
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	for i, j := range resp.Journeys {
		if i >= 3 {
			break
		}
		fmt.Fprintf(stdout, "Option %d: depart %s, arrive %s (%d min)\n", i+1, hhmm(j.StartDateTime), hhmm(j.ArrivalDateTime), j.Duration)
		for _, leg := range j.Legs {
			route := ""
			if len(leg.RouteOptions) > 0 {
				route = " [" + leg.RouteOptions[0].Name + "]"
			}
			fmt.Fprintf(stdout, "  %s %s%s: %s -> %s\n", hhmm(leg.DepartureTime), strings.ToUpper(leg.Mode.Name), route, leg.DeparturePoint.CommonName, leg.ArrivalPoint.CommonName)
		}
	}
	return exitcode.OK
}

type watchResult struct {
	Status            string        `json:"status"`
	Message           string        `json:"message"`
	Query             string        `json:"query,omitempty"`
	Candidates        int           `json:"candidates,omitempty"`
	ResolvedStop      *resolvedStop `json:"resolvedStop,omitempty"`
	Arrival           *tfl.Arrival  `json:"arrival,omitempty"`
	NextCheckAt       string        `json:"nextCheckAt,omitempty"`
	Notification      string        `json:"notification,omitempty"`
	NotificationOK    bool          `json:"notificationOk"`
	NotificationError string        `json:"notificationError,omitempty"`
}

func tflWatchArrival(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("watch-arrival", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stop := fs.String("stop", "", "TfL stop ID")
	query := fs.String("query", "", "stop/station search query")
	line := fs.String("line", "", "line filter")
	towards := fs.String("towards", "", "towards/destination substring")
	direction := fs.String("direction", "", "line-arrivals direction: inbound, outbound, or all")
	destinationStop := fs.String("destination-stop", "", "TfL destination stop ID for line-arrivals filtering")
	mode := fs.String("mode", "bus", "stop-search modes for --query, or all")
	searchLimit := fs.Int("search-limit", 5, "maximum stop-search candidates for --query")
	threshold := fs.Duration("threshold", 2*time.Minute, "notify threshold")
	channel := fs.String("openclaw-channel", "", "OpenClaw channel for notification")
	target := fs.String("openclaw-target", "", "OpenClaw target for notification")
	dryRun := fs.Bool("dry-run", false, "do not send notification")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *line == "" {
		fmt.Fprintln(stderr, "--line is required")
		return exitcode.Usage
	}
	if (*stop == "") == (*query == "") {
		fmt.Fprintln(stderr, "provide exactly one of --stop or --query")
		return exitcode.Usage
	}
	if *query != "" && *searchLimit <= 0 {
		fmt.Fprintln(stderr, "--search-limit must be > 0")
		return exitcode.Usage
	}
	if *threshold <= 0 {
		fmt.Fprintln(stderr, "--threshold must be > 0")
		return exitcode.Usage
	}

	sender := notify.OpenClaw{Channel: *channel, Target: *target, DryRun: *dryRun}
	if err := validateWatchDelivery(g, sender); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Config
	}
	found, err := findArrivals(ctx, client, arrivalLookup{
		StopID:          *stop,
		Query:           *query,
		Lines:           csvArgs(*line),
		Towards:         *towards,
		Direction:       *direction,
		DestinationStop: *destinationStop,
		SearchModes:     queryModes(*mode),
		SearchLimit:     *searchLimit,
	})
	if err != nil {
		msg := fmt.Sprintf("Live transport check failed: %v", err)
		notificationOK, sendErr := sendWatchNotification(ctx, sender, msg)
		if sendErr != nil {
			_ = writeWatchResult(g, stdout, stderr, watchResult{Status: "api_failed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: false, NotificationError: sendErr.Error()})
			fmt.Fprintln(stderr, sendErr)
			return exitcode.Generic
		}
		if code := writeWatchResult(g, stdout, stderr, watchResult{Status: "api_failed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: notificationOK}); code != exitcode.OK {
			return code
		}
		return exitcode.Network
	}
	if !found.StopFound {
		msg := fmt.Sprintf("Live transport update: TfL found no stop matching %q.", *query)
		notificationOK, err := sendWatchNotification(ctx, sender, msg)
		if err != nil {
			_ = writeWatchResult(g, stdout, stderr, watchResult{Status: "stop_not_found", Message: msg, Query: found.Query, Candidates: found.Candidates, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		if code := writeWatchResult(g, stdout, stderr, watchResult{Status: "stop_not_found", Message: msg, Query: found.Query, Candidates: found.Candidates, Notification: msg, NotificationOK: notificationOK}); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}
	arrivals := found.Arrivals
	if len(arrivals) == 0 {
		stopLabel := resolvedStopLabel(found.Stop, *stop)
		msg := fmt.Sprintf("Live transport update: TfL no longer shows a matching %s from %s.", *line, stopLabel)
		if found.Query != "" {
			msg = fmt.Sprintf("Live transport update: TfL found %d candidate stops for %q, but no matching %s arrivals.", found.Candidates, found.Query, *line)
		}
		notificationOK, err := sendWatchNotification(ctx, sender, msg)
		if err != nil {
			_ = writeWatchResult(g, stdout, stderr, watchResult{Status: "no_data", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		if code := writeWatchResult(g, stdout, stderr, watchResult{Status: "no_data", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: notificationOK}); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}

	next := arrivals[0]
	dueIn := time.Duration(next.TimeToStation) * time.Second
	if dueIn <= *threshold {
		msg := fmt.Sprintf("Transport heads-up: live TfL says the %s is about %d min away from %s.", next.LineName, roundMinutes(next.TimeToStation), next.StationName)
		notificationOK, err := sendWatchNotification(ctx, sender, msg)
		if err != nil {
			_ = writeWatchResult(g, stdout, stderr, watchResult{Status: "due", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		if code := writeWatchResult(g, stdout, stderr, watchResult{Status: "due", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, Notification: msg, NotificationOK: notificationOK}); code != exitcode.OK {
			return code
		}
		return exitcode.OK
	}

	nextCheck := next.ExpectedArrival.Add(-*threshold)
	msg := fmt.Sprintf("Transport update: live TfL says the %s has slipped to %s, about %d min away. Check again around %s.", next.LineName, next.ExpectedArrival.Local().Format("15:04"), roundMinutes(next.TimeToStation), nextCheck.Local().Format("15:04"))
	notificationOK, err := sendWatchNotification(ctx, sender, msg)
	if err != nil {
		_ = writeWatchResult(g, stdout, stderr, watchResult{Status: "delayed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, NextCheckAt: nextCheck.Format(time.RFC3339), Notification: msg, NotificationOK: false, NotificationError: err.Error()})
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	if code := writeWatchResult(g, stdout, stderr, watchResult{Status: "delayed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, NextCheckAt: nextCheck.Format(time.RFC3339), Notification: msg, NotificationOK: notificationOK}); code != exitcode.OK {
		return code
	}
	return exitcode.OK
}

type arrivalLookup struct {
	StopID          string
	Query           string
	Lines           []string
	Towards         string
	Direction       string
	DestinationStop string
	SearchModes     []string
	SearchLimit     int
}

type foundArrivals struct {
	Stop       *resolvedStop
	Arrivals   []tfl.Arrival
	Query      string
	Candidates int
	StopFound  bool
}

func findArrivals(ctx context.Context, client *tfl.Client, lookup arrivalLookup) (foundArrivals, error) {
	if lookup.StopID != "" {
		arrivals, err := client.LineArrivals(ctx, lookup.StopID, lookup.Lines, lookup.Direction, lookup.DestinationStop)
		if err != nil {
			return foundArrivals{Stop: stopFromID(lookup.StopID), StopFound: true}, err
		}
		return foundArrivals{Stop: stopFromID(lookup.StopID), Arrivals: tfl.FilterArrivals(arrivals, "", lookup.Towards), StopFound: true}, nil
	}
	search, err := client.StopSearchWithOptions(ctx, lookup.Query, tfl.StopSearchOptions{
		Modes:      lookup.SearchModes,
		Lines:      lookup.Lines,
		MaxResults: lookup.SearchLimit,
	})
	if err != nil {
		return foundArrivals{Query: lookup.Query}, err
	}
	found := foundArrivals{Query: lookup.Query, Candidates: len(search.Matches), StopFound: len(search.Matches) > 0}
	if len(search.Matches) == 0 {
		found.Arrivals = []tfl.Arrival{}
		return found, nil
	}
	found.Stop = stopFromMatch(search.Matches[0])
	var firstArrivals []tfl.Arrival
	var firstErr error
	for _, match := range search.Matches {
		candidate := stopFromMatch(match)
		arrivals, err := client.LineArrivals(ctx, match.ID, lookup.Lines, lookup.Direction, lookup.DestinationStop)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("check arrivals for %s: %w", resolvedStopLabel(candidate, match.ID), err)
			}
			continue
		}
		arrivals = tfl.FilterArrivals(arrivals, "", lookup.Towards)
		if firstArrivals == nil {
			firstArrivals = arrivals
		}
		if len(arrivals) > 0 {
			found.Stop = candidate
			found.Arrivals = arrivals
			return found, nil
		}
	}
	found.Arrivals = firstArrivals
	if firstErr != nil {
		return found, firstErr
	}
	return found, nil
}

func stopFromID(id string) *resolvedStop {
	return &resolvedStop{ID: id}
}

func stopFromMatch(match tfl.MatchedStop) *resolvedStop {
	return &resolvedStop{ID: match.ID, Name: match.Name, Lat: match.Lat, Lon: match.Lon, Modes: match.Modes, ParentID: match.ParentID}
}

func queryModes(value string) []string {
	if strings.EqualFold(strings.TrimSpace(value), "all") {
		return nil
	}
	return csvArgs(value)
}

func resolvedStopLabel(stop *resolvedStop, fallback string) string {
	if stop == nil {
		return fallback
	}
	if strings.TrimSpace(stop.Name) != "" {
		return stop.Name
	}
	if strings.TrimSpace(stop.ID) != "" {
		return stop.ID
	}
	return fallback
}

func disruptionText(d tfl.Disruption) string {
	for _, value := range []string{d.Description, d.Summary, d.ClosureText, d.Type, d.Category} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "Disruption reported"
}

func writeNextArrivalResult(g globals, stdout, stderr io.Writer, result nextArrivalResult) int {
	if g.format == output.JSON {
		return writeJSONWithOK(g, stdout, stderr, result, result.Status == "ok", result.Status != "ok")
	}
	if g.format == output.Plain {
		stopID := ""
		stopName := ""
		if result.ResolvedStop != nil {
			stopID = result.ResolvedStop.ID
			stopName = result.ResolvedStop.Name
		}
		nextLine := ""
		nextDestination := ""
		nextStation := ""
		nextTime := ""
		nextSeconds := ""
		if result.Next != nil {
			nextLine = result.Next.LineName
			nextDestination = result.Next.DestinationName
			nextStation = result.Next.StationName
			nextTime = result.Next.ExpectedArrival.Format(time.RFC3339)
			nextSeconds = strconv.Itoa(result.Next.TimeToStation)
		}
		_ = output.WritePlainRows(stdout, [][]string{{
			result.Status,
			result.Message,
			result.Line,
			stopID,
			stopName,
			nextLine,
			nextDestination,
			nextStation,
			nextTime,
			nextSeconds,
		}})
		return exitcode.OK
	}
	fmt.Fprintln(stdout, result.Message)
	return exitcode.OK
}

func sendWatchNotification(ctx context.Context, sender notify.OpenClaw, message string) (bool, error) {
	if err := sender.Send(ctx, message); err != nil {
		return false, err
	}
	return sender.Enabled() || sender.DryRun, nil
}

func validateWatchDelivery(g globals, sender notify.OpenClaw) error {
	partial := sender.Channel == "" != (sender.Target == "")
	if partial {
		return errors.New("--openclaw-channel and --openclaw-target must be provided together")
	}
	if g.noInput && !sender.Enabled() && !sender.DryRun {
		return errors.New("--no-input watch-arrival requires visible OpenClaw delivery or --dry-run")
	}
	return nil
}

func writeWatchResult(g globals, stdout, stderr io.Writer, result watchResult) int {
	if g.format == output.JSON {
		ok := (result.Status == "due" || result.Status == "delayed") && result.NotificationError == ""
		return writeJSONWithOK(g, stdout, stderr, result, ok, !ok)
	}
	if g.format == output.Plain {
		arrivalLine := ""
		arrivalDestination := ""
		arrivalStation := ""
		arrivalTime := ""
		arrivalSeconds := ""
		if result.Arrival != nil {
			arrivalLine = result.Arrival.LineName
			arrivalDestination = result.Arrival.DestinationName
			arrivalStation = result.Arrival.StationName
			arrivalTime = result.Arrival.ExpectedArrival.Format(time.RFC3339)
			arrivalSeconds = strconv.Itoa(result.Arrival.TimeToStation)
		}
		_ = output.WritePlainRows(stdout, [][]string{{
			result.Status,
			result.Message,
			result.Notification,
			strconv.FormatBool(result.NotificationOK),
			result.NotificationError,
			result.NextCheckAt,
			arrivalLine,
			arrivalDestination,
			arrivalStation,
			arrivalTime,
			arrivalSeconds,
		}})
		return exitcode.OK
	}
	fmt.Fprintln(stdout, result.Message)
	return exitcode.OK
}

func roundMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 30) / 60
}

func hhmm(iso string) string {
	if len(iso) >= 16 && iso[10] == 'T' {
		return iso[11:16]
	}
	return iso
}

func csvArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func appendCSV(existing, value string) string {
	if strings.TrimSpace(existing) == "" {
		return value
	}
	return existing + "," + value
}

func canonicalJourneyPreference(value string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "")) {
	case "", "leasttime", "time", "fastest":
		return "LeastTime"
	case "leastinterchange", "interchange":
		return "LeastInterchange"
	case "leastwalking", "walking":
		return "LeastWalking"
	default:
		return value
	}
}

func canonicalWalkingSpeed(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "average":
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return "Average"
	case "slow":
		return "Slow"
	case "fast":
		return "Fast"
	default:
		return value
	}
}

func canonicalCyclePreference(value string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "")) {
	case "":
		return ""
	case "none":
		return "None"
	case "leaveatstation":
		return "LeaveAtStation"
	case "takeontransport":
		return "TakeOnTransport"
	case "alltheway":
		return "AllTheWay"
	case "cyclehire":
		return "CycleHire"
	default:
		return value
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "londonjourneycli - executable contracts for agent skills")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage: londonjourneycli [--json|--plain] [--skills-dir DIR] <command> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list                         List discovered skills")
	fmt.Fprintln(w, "  search <query>               Search skills")
	fmt.Fprintln(w, "  show <skill>                 Show a skill and manifest")
	fmt.Fprintln(w, "  lint                         Validate discovered skills")
	fmt.Fprintln(w, "  doctor <skill>               Check local requirements")
	fmt.Fprintln(w, "  run <skill> <command>        Run a manifest command")
	fmt.Fprintln(w, "  test <skill>                 Run manifest tests")
	fmt.Fprintln(w, "  tfl status [--line ID]       Show live TfL line status")
	fmt.Fprintln(w, "  tfl disruptions [--line ID]  Show active TfL disruptions")
	fmt.Fprintln(w, "  tfl line-routes --line ID    Show line route sections")
	fmt.Fprintln(w, "  tfl nearby-stops <coords>    Find stops near coordinates")
	fmt.Fprintln(w, "  tfl stop-search <query>      Search TfL stops and stations")
	fmt.Fprintln(w, "  tfl stop-info --stop ID      Show a stop point and child stops")
	fmt.Fprintln(w, "  tfl arrivals --stop ID       Show live arrivals")
	fmt.Fprintln(w, "  tfl next-arrival ...         Resolve a stop query and show the next arrival")
	fmt.Fprintln(w, "  tfl journey --from A --to B  Plan a London journey")
	fmt.Fprintln(w, "  tfl watch-arrival ...        One-shot live arrival check")
}
