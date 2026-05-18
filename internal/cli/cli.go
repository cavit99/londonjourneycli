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
		fmt.Fprintln(stderr, "usage: londonjourneycli tfl <status|disruptions|line-routes|nearby-stops|accessible-stations|stop-search|stop-info|arrivals|next-arrival|journey|trip|fare|fares|watch-arrival>")
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
	case "accessible-stations":
		return tflAccessibleStations(ctx, g, client, args[1:], stdout, stderr)
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
	case "trip":
		return tflTrip(ctx, g, client, args[1:], stdout, stderr)
	case "fare", "fares":
		return tflFares(ctx, g, client, args[1:], stdout, stderr)
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

func tflAccessibleStations(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessible-stations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	near := fs.String("near", "", "place or station query to resolve with TfL stop search")
	lat := fs.Float64("lat", 0, "latitude")
	lon := fs.Float64("lon", 0, "longitude")
	location := fs.String("location", "", "location text, map link, geo URI, or lat,lon")
	radius := fs.Int("radius", 1200, "search radius in metres")
	mode := fs.String("mode", defaultAccessibleStationModes, "comma-separated modes, or all")
	stopTypes := fs.String("stop-type", "", "comma-separated TfL stop types; defaults to station-like stop types for the selected modes")
	limit := fs.Int("limit", 10, "maximum stations after filtering")
	requireLift := fs.Bool("require-lift", false, "only include stations with a lift signal")
	requireStepFree := fs.Bool("require-step-free", false, "only include stations with TfL-confirmed access via lift")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	modes := queryModes(*mode)
	stopTypeList := csvArgs(*stopTypes)
	if !flagSeen(fs, "stop-type") || len(stopTypeList) == 0 {
		stopTypeList = defaultAccessibleStationStopTypes(modes)
	}

	nearQuery := strings.TrimSpace(*near)
	locationText := strings.TrimSpace(*location)
	latSeen := flagSeen(fs, "lat")
	lonSeen := flagSeen(fs, "lon")
	locationSources := 0
	if nearQuery != "" {
		locationSources++
	}
	if locationText != "" {
		locationSources++
	}
	if latSeen || lonSeen {
		locationSources++
	}
	if locationSources != 1 {
		fmt.Fprintln(stderr, "provide exactly one of --near, --location, or both --lat and --lon")
		return exitcode.Usage
	}
	if latSeen != lonSeen {
		fmt.Fprintln(stderr, "provide both --lat and --lon")
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

	result := accessibleStationsResult{
		Status:          "ok",
		Radius:          *radius,
		Modes:           modes,
		StopTypes:       stopTypeList,
		RequireLift:     *requireLift,
		RequireStepFree: *requireStepFree,
		Stations:        []accessibleStation{},
	}

	if nearQuery != "" {
		search, err := client.StopSearchWithOptions(ctx, nearQuery, tfl.StopSearchOptions{Modes: modes, MaxResults: 1})
		if err != nil {
			if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
				return code
			}
			fmt.Fprintln(stderr, err)
			return exitcode.Network
		}
		result.Query = nearQuery
		result.Candidates = search.Total
		if len(search.Matches) == 0 {
			result.Status = "near_not_found"
			result.Message = fmt.Sprintf("TfL found no stop or station matching %q.", nearQuery)
			if code := writeAccessibleStationsResult(g, stdout, stderr, result); code != exitcode.OK {
				return code
			}
			return exitcode.NoData
		}
		resolved := stopFromMatch(search.Matches[0])
		result.ResolvedNear = resolved
		*lat = resolved.Lat
		*lon = resolved.Lon
	} else if locationText != "" {
		parsedLat, parsedLon, err := parseLocationArg(locationText)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitcode.Usage
		}
		*lat = parsedLat
		*lon = parsedLon
	}
	if err := validateLatLon(*lat, *lon); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Usage
	}
	result.Lat = *lat
	result.Lon = *lon

	stops, err := client.NearbyStopsWithProperties(ctx, tfl.NearbyStopOptions{
		Lat:        *lat,
		Lon:        *lon,
		Radius:     *radius,
		Modes:      modes,
		StopTypes:  stopTypeList,
		Categories: accessibleStationPropertyCategories(),
		Limit:      -1,
	})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	for _, stop := range stops {
		station := accessibleStationFromStop(stop)
		if *requireStepFree && !station.StepFreeAccess {
			continue
		}
		if *requireLift && !station.LiftPresent && !station.StepFreeAccess {
			continue
		}
		result.Stations = append(result.Stations, station)
		if len(result.Stations) == *limit {
			break
		}
	}
	if len(result.Stations) == 0 {
		result.Status = "no_data"
		if *requireStepFree {
			result.Message = fmt.Sprintf("TfL found no stations with confirmed access via lift within %dm.", *radius)
		} else if *requireLift {
			result.Message = fmt.Sprintf("TfL found no stations with a lift signal within %dm.", *radius)
		} else {
			result.Message = fmt.Sprintf("TfL found no stations within %dm.", *radius)
		}
	} else if *requireStepFree {
		result.Message = fmt.Sprintf("TfL found %d stations with confirmed access via lift within %dm.", len(result.Stations), *radius)
	} else if *requireLift {
		result.Message = fmt.Sprintf("TfL found %d stations with a lift signal within %dm.", len(result.Stations), *radius)
	} else {
		result.Message = fmt.Sprintf("TfL found %d stations within %dm.", len(result.Stations), *radius)
	}
	if code := writeAccessibleStationsResult(g, stdout, stderr, result); code != exitcode.OK {
		return code
	}
	if result.Status == "no_data" {
		return exitcode.NoData
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
	liftCountPattern         = regexp.MustCompile(`\d+`)
)

const (
	defaultAccessibleStationModes = "tube,dlr,elizabeth-line,overground,national-rail,tram"
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

type accessibleStationsResult struct {
	Status          string              `json:"status"`
	Message         string              `json:"message"`
	Query           string              `json:"query,omitempty"`
	Candidates      int                 `json:"candidates,omitempty"`
	ResolvedNear    *resolvedStop       `json:"resolvedNear,omitempty"`
	Lat             float64             `json:"lat"`
	Lon             float64             `json:"lon"`
	Radius          int                 `json:"radius"`
	Modes           []string            `json:"modes,omitempty"`
	StopTypes       []string            `json:"stopTypes,omitempty"`
	RequireLift     bool                `json:"requireLift"`
	RequireStepFree bool                `json:"requireStepFree"`
	Stations        []accessibleStation `json:"stations"`
}

type accessibleStation struct {
	ID                           string   `json:"id"`
	Name                         string   `json:"name"`
	Lat                          float64  `json:"lat"`
	Lon                          float64  `json:"lon"`
	Distance                     *float64 `json:"distance"`
	Modes                        []string `json:"modes,omitempty"`
	StopType                     string   `json:"stopType,omitempty"`
	AccessStatus                 string   `json:"accessStatus"`
	StepFreeAccess               bool     `json:"stepFreeAccess"`
	LiftPresent                  bool     `json:"liftPresent"`
	Lifts                        *int     `json:"lifts"`
	AccessViaLift                *bool    `json:"accessViaLift"`
	LimitedCapacityLift          *bool    `json:"limitedCapacityLift"`
	SpecificEntranceRequired     *bool    `json:"specificEntranceRequired"`
	SpecificEntranceInstructions string   `json:"specificEntranceInstructions,omitempty"`
	AdditionalInformation        string   `json:"additionalInformation,omitempty"`
}

type tflErrorResult struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Error   *tfl.APIError `json:"error,omitempty"`
}

func accessibleStationFromStop(stop tfl.StopPointWithProperties) accessibleStation {
	lifts := liftCount(stop.AdditionalProperties)
	accessViaLift := accessViaLiftValue(stop.AdditionalProperties)
	liftPresent := lifts != nil && *lifts > 0
	stepFreeAccess := accessViaLift != nil && *accessViaLift
	return accessibleStation{
		ID:                           stop.ID,
		Name:                         stop.CommonName,
		Lat:                          stop.Lat,
		Lon:                          stop.Lon,
		Distance:                     stop.Distance,
		Modes:                        stop.Modes,
		StopType:                     stop.StopType,
		AccessStatus:                 accessibleStationStatus(lifts, accessViaLift),
		StepFreeAccess:               stepFreeAccess,
		LiftPresent:                  liftPresent,
		Lifts:                        lifts,
		AccessViaLift:                accessViaLift,
		LimitedCapacityLift:          boolAdditionalPropertyValue(stop.AdditionalProperties, "Accessibility", "LimitedCapacityLift"),
		SpecificEntranceRequired:     boolAdditionalPropertyValue(stop.AdditionalProperties, "Accessibility", "SpecificEntranceRequired"),
		SpecificEntranceInstructions: firstAdditionalPropertyValue(stop.AdditionalProperties, "Accessibility", "SpecificEntranceInstructions"),
		AdditionalInformation:        firstAdditionalPropertyValue(stop.AdditionalProperties, "Accessibility", "AdditionalInformation", "AddtionalInformation"),
	}
}

func accessibleStationStatus(lifts *int, accessViaLift *bool) string {
	if accessViaLift != nil {
		if *accessViaLift {
			return "confirmed_step_free"
		}
		return "no_lift_access"
	}
	if lifts != nil {
		if *lifts > 0 {
			return "lift_present_unconfirmed"
		}
		return "no_lifts"
	}
	return "unknown"
}

func liftCount(props []tfl.AdditionalProperty) *int {
	value, ok := additionalPropertyValue(props, "Facility", "Lifts")
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if n, err := strconv.Atoi(trimmed); err == nil {
		return &n
	}
	match := liftCountPattern.FindString(trimmed)
	if match == "" {
		return nil
	}
	n, err := strconv.Atoi(match)
	if err != nil {
		return nil
	}
	return &n
}

func accessViaLiftValue(props []tfl.AdditionalProperty) *bool {
	return boolAdditionalPropertyValue(props, "Accessibility", "AccessViaLift")
}

func boolAdditionalPropertyValue(props []tfl.AdditionalProperty, category, key string) *bool {
	value, ok := additionalPropertyValue(props, category, key)
	if !ok {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "y":
		v := true
		return &v
	case "no", "false", "0", "n":
		v := false
		return &v
	default:
		return nil
	}
}

func firstAdditionalPropertyValue(props []tfl.AdditionalProperty, category string, keys ...string) string {
	for _, key := range keys {
		if value, ok := additionalPropertyValue(props, category, key); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func additionalPropertyValue(props []tfl.AdditionalProperty, category, key string) (string, bool) {
	category = normalizeAdditionalPropertyKey(category)
	key = normalizeAdditionalPropertyKey(key)
	for _, prop := range props {
		if normalizeAdditionalPropertyKey(prop.Category) == category && normalizeAdditionalPropertyKey(prop.Key) == key {
			return prop.Value, true
		}
	}
	return "", false
}

func normalizeAdditionalPropertyKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	return value
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

type journeyFlagValues struct {
	from                *string
	to                  *string
	date                *string
	when                *string
	via                 *string
	preference          *string
	modes               *string
	accessibility       *string
	maxTransfer         *string
	maxWalking          *string
	walkingSpeed        *string
	cyclePreference     *string
	arriving            *bool
	includeAlternatives *bool
	alternativeWalking  *bool
	alternativeCycle    *bool
	realTime            *bool
	betweenEntrances    *bool
	localOnly           *bool
}

func addJourneyFlags(fs *flag.FlagSet) journeyFlagValues {
	return journeyFlagValues{
		from:                fs.String("from", "", "origin"),
		to:                  fs.String("to", "", "destination"),
		date:                fs.String("date", "", "YYYYMMDD"),
		when:                fs.String("time", "", "HHmm"),
		arriving:            fs.Bool("arriving", false, "treat time as arrival time"),
		via:                 fs.String("via", "", "optional via point"),
		preference:          fs.String("preference", "LeastTime", "LeastTime, LeastInterchange, or LeastWalking"),
		modes:               fs.String("mode", "", "comma-separated modes, e.g. tube,elizabeth-line,bus"),
		accessibility:       fs.String("accessibility", "", "comma-separated accessibility preferences"),
		maxTransfer:         fs.String("max-transfer-minutes", "", "maximum transfer walking minutes"),
		maxWalking:          fs.String("max-walking-minutes", "", "maximum journey walking minutes"),
		walkingSpeed:        fs.String("walking-speed", "", "Slow, Average, or Fast"),
		cyclePreference:     fs.String("cycle-preference", "", "TfL cycle preference"),
		includeAlternatives: fs.Bool("include-alternatives", false, "include alternative public transport routes"),
		alternativeWalking:  fs.Bool("alternative-walking", false, "include alternative walking journey"),
		alternativeCycle:    fs.Bool("alternative-cycle", false, "include alternative cycling journey"),
		realTime:            fs.Bool("real-time", false, "request real-time live arrivals where available"),
		betweenEntrances:    fs.Bool("between-entrances", false, "include station entrance/platform routing"),
		localOnly:           fs.Bool("local-only", false, "disable TfL nationalSearch"),
	}
}

func (f journeyFlagValues) options() tfl.JourneyOptions {
	return tfl.JourneyOptions{
		Date:                     *f.date,
		Time:                     *f.when,
		Arriving:                 *f.arriving,
		Via:                      *f.via,
		Preference:               canonicalJourneyPreference(*f.preference),
		Modes:                    csvArgs(*f.modes),
		AccessibilityPreferences: canonicalAccessibilityPreferences(*f.accessibility),
		MaxTransferMinutes:       *f.maxTransfer,
		MaxWalkingMinutes:        *f.maxWalking,
		WalkingSpeed:             canonicalWalkingSpeed(*f.walkingSpeed),
		CyclePreference:          canonicalCyclePreference(*f.cyclePreference),
		IncludeAlternativeRoutes: *f.includeAlternatives,
		AlternativeWalking:       *f.alternativeWalking,
		AlternativeCycle:         *f.alternativeCycle,
		UseRealTimeLiveArrivals:  *f.realTime,
		RouteBetweenEntrances:    *f.betweenEntrances,
		LocalOnly:                *f.localOnly,
	}
}

func tflJourney(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("journey", flag.ContinueOnError)
	fs.SetOutput(stderr)
	journeyFlags := addJourneyFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *journeyFlags.from == "" || *journeyFlags.to == "" {
		fmt.Fprintln(stderr, "--from and --to are required")
		return exitcode.Usage
	}
	resp, err := client.Journey(ctx, *journeyFlags.from, *journeyFlags.to, journeyFlags.options())
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

func tflTrip(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trip", flag.ContinueOnError)
	fs.SetOutput(stderr)
	journeyFlags := addJourneyFlags(fs)
	limit := fs.Int("limit", 3, "maximum journey options to summarize")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *journeyFlags.from == "" || *journeyFlags.to == "" {
		fmt.Fprintln(stderr, "--from and --to are required")
		return exitcode.Usage
	}
	if *limit <= 0 {
		fmt.Fprintln(stderr, "--limit must be > 0")
		return exitcode.Usage
	}

	opts := journeyFlags.options()
	resp, err := client.Journey(ctx, *journeyFlags.from, *journeyFlags.to, opts)
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return tflErrorExitCode(err)
	}

	result := buildTripResult(*journeyFlags.from, *journeyFlags.to, resp, opts, *limit)
	finalCode := exitcode.OK
	if result.Status == "no_journey" {
		finalCode = exitcode.NoData
	} else {
		lineIDs := tripLineIDs(result.Disruptions.Lines)
		if len(lineIDs) == 0 {
			result.Disruptions.Status = "not_applicable"
			result.Disruptions.Message = "Selected journey has no TfL line to check for disruptions."
		} else {
			disruptions, err := client.LineDisruptions(ctx, lineIDs, nil)
			if err != nil {
				result.Status = "partial"
				result.Disruptions.Status = tripAPIStatus(err)
				result.Disruptions.Message = "Journey planned, but TfL line disruption lookup failed."
				result.Disruptions.Error = err.Error()
				result.Disruptions.Active = []tfl.Disruption{}
				finalCode = exitcode.Network
			} else {
				if disruptions == nil {
					disruptions = []tfl.Disruption{}
				}
				result.Disruptions.Status = "ok"
				result.Disruptions.Active = disruptions
				if len(disruptions) == 0 {
					result.Disruptions.Message = "No active disruptions found for selected journey lines."
				} else {
					result.Disruptions.Message = fmt.Sprintf("TfL reports %d active %s for selected journey lines.", len(disruptions), plural("disruption", len(disruptions)))
				}
			}
		}
	}
	result.Message = tripMessage(result)
	if code := writeTripResult(g, stdout, stderr, result); code != exitcode.OK {
		return code
	}
	return finalCode
}

type tripResult struct {
	Status        string                  `json:"status"`
	Message       string                  `json:"message"`
	From          string                  `json:"from"`
	To            string                  `json:"to"`
	Selected      *tripJourneySummary     `json:"selected"`
	Journeys      []tripJourneySummary    `json:"journeys"`
	Fare          tfl.FareQuote           `json:"fare"`
	Disruptions   tripDisruptionResult    `json:"disruptions"`
	Accessibility tripAccessibilityResult `json:"accessibility"`
}

type tripJourneySummary struct {
	Option          int               `json:"option"`
	StartDateTime   string            `json:"startDateTime"`
	ArrivalDateTime string            `json:"arrivalDateTime"`
	DurationMinutes int               `json:"durationMinutes"`
	Lines           []tripLineSummary `json:"lines"`
	Legs            []tripLegSummary  `json:"legs"`
}

type tripLegSummary struct {
	Mode            string   `json:"mode"`
	Line            string   `json:"line,omitempty"`
	LineID          string   `json:"lineId,omitempty"`
	RouteOptions    []string `json:"routeOptions,omitempty"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	DepartureTime   string   `json:"departureTime,omitempty"`
	ArrivalTime     string   `json:"arrivalTime,omitempty"`
	DurationMinutes int      `json:"durationMinutes"`
	Instruction     string   `json:"instruction,omitempty"`
}

type tripLineSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mode string `json:"mode,omitempty"`
}

type tripDisruptionResult struct {
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Lines   []tripLineSummary `json:"lines"`
	Active  []tfl.Disruption  `json:"active"`
	Error   string            `json:"error,omitempty"`
}

type tripAccessibilityResult struct {
	Status                  string   `json:"status"`
	Requested               []string `json:"requested"`
	RouteBetweenEntrances   bool     `json:"routeBetweenEntrances"`
	MaxWalkingMinutes       string   `json:"maxWalkingMinutes,omitempty"`
	MaxTransferMinutes      string   `json:"maxTransferMinutes,omitempty"`
	WalkingSpeed            string   `json:"walkingSpeed,omitempty"`
	SelectedRequiresWalking bool     `json:"selectedRequiresWalking"`
	Flags                   []string `json:"flags"`
	Notes                   []string `json:"notes"`
}

func buildTripResult(from, to string, resp tfl.JourneyResponse, opts tfl.JourneyOptions, limit int) tripResult {
	journeys := summarizeTripJourneys(resp.Journeys, limit)
	result := tripResult{
		Status:   "ok",
		From:     from,
		To:       to,
		Journeys: journeys,
		Fare:     emptyTripFare(from, to, "TfL returned no fare for that journey."),
		Disruptions: tripDisruptionResult{
			Status:  "not_applicable",
			Message: "No selected journey lines to check for disruptions.",
			Lines:   []tripLineSummary{},
			Active:  []tfl.Disruption{},
		},
	}
	if len(resp.Journeys) == 0 {
		result.Status = "no_journey"
		result.Fare = emptyTripFare(from, to, "TfL returned no journey and no fare.")
		result.Accessibility = tripAccessibility(opts, nil)
		result.Message = tripMessage(result)
		return result
	}
	selected := journeys[0]
	result.Selected = &selected
	result.Fare = journeyFareQuote(tfl.JourneyResponse{Journeys: []tfl.Journey{resp.Journeys[0]}}, tfl.MatchedStop{Name: from}, tfl.MatchedStop{Name: to}, "Adult", "contactless", opts.Date, opts.Time)
	result.Disruptions.Lines = selected.Lines
	result.Accessibility = tripAccessibility(opts, result.Selected)
	result.Message = tripMessage(result)
	return result
}

func summarizeTripJourneys(journeys []tfl.Journey, limit int) []tripJourneySummary {
	if len(journeys) == 0 {
		return []tripJourneySummary{}
	}
	if limit > len(journeys) {
		limit = len(journeys)
	}
	out := make([]tripJourneySummary, 0, limit)
	for i := 0; i < limit; i++ {
		journey := journeys[i]
		summary := tripJourneySummary{
			Option:          i + 1,
			StartDateTime:   journey.StartDateTime,
			ArrivalDateTime: journey.ArrivalDateTime,
			DurationMinutes: journey.Duration,
			Lines:           []tripLineSummary{},
			Legs:            []tripLegSummary{},
		}
		seenLines := map[string]bool{}
		for _, leg := range journey.Legs {
			routeOptions := tripRouteNames(leg.RouteOptions)
			lineName := ""
			lineID := ""
			if isTripLineMode(leg.Mode.Name) && len(routeOptions) > 0 {
				lineName = routeOptions[0]
				lineID = tripLineID(lineName)
				for _, routeName := range routeOptions {
					line := tripLineSummary{ID: tripLineID(routeName), Name: routeName, Mode: leg.Mode.Name}
					key := firstNonEmptyString(line.ID, line.Name)
					if key != "" && !seenLines[key] {
						seenLines[key] = true
						summary.Lines = append(summary.Lines, line)
					}
				}
			}
			instruction := strings.TrimSpace(firstNonEmptyString(leg.Instruction.Summary, leg.Instruction.Detailed))
			summary.Legs = append(summary.Legs, tripLegSummary{
				Mode:            leg.Mode.Name,
				Line:            lineName,
				LineID:          lineID,
				RouteOptions:    routeOptions,
				From:            leg.DeparturePoint.CommonName,
				To:              leg.ArrivalPoint.CommonName,
				DepartureTime:   leg.DepartureTime,
				ArrivalTime:     leg.ArrivalTime,
				DurationMinutes: leg.Duration,
				Instruction:     instruction,
			})
		}
		out = append(out, summary)
	}
	return out
}

func tripRouteNames(options []tfl.RouteOption) []string {
	seen := map[string]bool{}
	var out []string
	for _, option := range options {
		name := strings.TrimSpace(option.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func isTripLineMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "walk", "walking", "cycle", "cycling":
		return false
	default:
		return true
	}
}

func tripLineID(name string) string {
	key := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "&", "and"))), " ")
	switch key {
	case "bakerloo", "central", "circle", "district", "jubilee", "metropolitan", "northern", "piccadilly", "victoria":
		return key
	case "hammersmith and city":
		return "hammersmith-city"
	case "waterloo and city":
		return "waterloo-city"
	case "elizabeth", "elizabeth line":
		return "elizabeth"
	case "dlr":
		return "dlr"
	case "tram", "london trams":
		return "tram"
	case "london overground", "overground":
		return "london-overground"
	default:
		return slugLineID(name)
	}
}

func slugLineID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func tripLineIDs(lines []tripLineSummary) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		id := strings.TrimSpace(line.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func emptyTripFare(from, to, message string) tfl.FareQuote {
	return tfl.FareQuote{
		Status:        "no_data",
		Message:       message,
		Kind:          "journey",
		From:          from,
		To:            to,
		PassengerType: "Adult",
		Payment:       "contactless",
		Currency:      "GBP",
		Fares:         []tfl.FareOption{},
		Source:        "TfL Journey Planner fare",
		Notes:         []string{"TfL Journey Planner fares can vary by route, direction, time, and service."},
	}
}

func tripAccessibility(opts tfl.JourneyOptions, selected *tripJourneySummary) tripAccessibilityResult {
	result := tripAccessibilityResult{
		Status:                  "not_requested",
		Requested:               append([]string{}, opts.AccessibilityPreferences...),
		RouteBetweenEntrances:   opts.RouteBetweenEntrances,
		MaxWalkingMinutes:       opts.MaxWalkingMinutes,
		MaxTransferMinutes:      opts.MaxTransferMinutes,
		WalkingSpeed:            opts.WalkingSpeed,
		SelectedRequiresWalking: selectedHasWalking(selected),
		Flags:                   []string{},
		Notes:                   []string{},
	}
	if len(result.Requested) > 0 || opts.RouteBetweenEntrances || opts.MaxWalkingMinutes != "" || opts.MaxTransferMinutes != "" || opts.WalkingSpeed != "" {
		result.Status = "requested"
	}
	if len(result.Requested) > 0 {
		result.Flags = appendUniqueString(result.Flags, "accessibility_preferences_requested")
		result.Notes = append(result.Notes, "TfL Journey Planner accessibility preferences were applied.")
		for _, pref := range result.Requested {
			if strings.Contains(strings.ToLower(pref), "stepfree") {
				result.Flags = appendUniqueString(result.Flags, "step_free_requested")
				break
			}
		}
	}
	if opts.RouteBetweenEntrances {
		result.Flags = appendUniqueString(result.Flags, "route_between_entrances_requested")
		result.Notes = append(result.Notes, "Station entrance/platform routing was requested.")
	}
	if opts.MaxWalkingMinutes != "" {
		result.Flags = appendUniqueString(result.Flags, "max_walking_minutes_set")
	}
	if opts.MaxTransferMinutes != "" {
		result.Flags = appendUniqueString(result.Flags, "max_transfer_minutes_set")
	}
	if result.SelectedRequiresWalking {
		result.Flags = appendUniqueString(result.Flags, "walking_required")
		result.Notes = append(result.Notes, "Selected journey includes at least one walking leg.")
	}
	if len(result.Notes) == 0 {
		result.Notes = append(result.Notes, "No accessibility preference requested; route may include stairs, escalators, and walking transfers.")
	}
	return result
}

func selectedHasWalking(selected *tripJourneySummary) bool {
	if selected == nil {
		return false
	}
	for _, leg := range selected.Legs {
		mode := strings.ToLower(strings.TrimSpace(leg.Mode))
		if mode == "walk" || mode == "walking" {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func tripAPIStatus(err error) string {
	var apiErr *tfl.APIError
	if errors.As(err, &apiErr) {
		return "api_error"
	}
	return "api_failed"
}

func tripMessage(result tripResult) string {
	if result.Status == "no_journey" {
		return fmt.Sprintf("TfL returned no journeys from %s to %s.", result.From, result.To)
	}
	if result.Selected == nil {
		return result.Disruptions.Message
	}
	fare := "fare unavailable"
	if result.Fare.Status == "ok" && result.Fare.AmountPence > 0 {
		fare = "fare " + formatPounds(result.Fare.AmountPence)
	}
	disruption := "no line disruptions checked"
	switch {
	case result.Disruptions.Status == "api_error" || result.Disruptions.Status == "api_failed":
		disruption = "disruption lookup failed"
	case len(result.Disruptions.Active) > 0:
		disruption = fmt.Sprintf("%d active %s on %s", len(result.Disruptions.Active), plural("disruption", len(result.Disruptions.Active)), tripLineNames(result.Disruptions.Lines))
	case len(result.Disruptions.Lines) > 0:
		disruption = "no active disruptions on " + tripLineNames(result.Disruptions.Lines)
	}
	return fmt.Sprintf("Selected option departs %s, arrives %s (%d min), %s; %s.", hhmm(result.Selected.StartDateTime), hhmm(result.Selected.ArrivalDateTime), result.Selected.DurationMinutes, fare, disruption)
}

func tripLineNames(lines []tripLineSummary) string {
	var names []string
	for _, line := range lines {
		name := firstNonEmptyString(line.Name, line.ID)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "selected lines"
	}
	return strings.Join(names, ",")
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func writeTripResult(g globals, stdout, stderr io.Writer, result tripResult) int {
	if g.format == output.JSON {
		return writeJSONWithOK(g, stdout, stderr, result, result.Status == "ok", result.Status != "ok")
	}
	if g.format == output.Plain {
		var rows [][]string
		rows = append(rows, []string{"summary", result.Status, result.From, result.To, result.Message})
		if result.Selected != nil {
			rows = append(rows, []string{"selected", strconv.Itoa(result.Selected.Option), hhmm(result.Selected.StartDateTime), hhmm(result.Selected.ArrivalDateTime), strconv.Itoa(result.Selected.DurationMinutes), formatTripFare(result.Fare), tripLineNames(result.Selected.Lines)})
		}
		for _, journey := range result.Journeys {
			rows = append(rows, []string{"journey", strconv.Itoa(journey.Option), hhmm(journey.StartDateTime), hhmm(journey.ArrivalDateTime), strconv.Itoa(journey.DurationMinutes), tripLineNames(journey.Lines)})
			for _, leg := range journey.Legs {
				rows = append(rows, []string{"leg", strconv.Itoa(journey.Option), leg.Mode, leg.LineID, leg.Line, hhmm(leg.DepartureTime), hhmm(leg.ArrivalTime), strconv.Itoa(leg.DurationMinutes), leg.From + " -> " + leg.To})
			}
		}
		rows = append(rows, []string{"fare", result.Fare.Status, formatTripFare(result.Fare), result.Fare.Message})
		if len(result.Disruptions.Active) == 0 {
			rows = append(rows, []string{"disruptions", result.Disruptions.Status, strings.Join(tripLineIDs(result.Disruptions.Lines), ","), result.Disruptions.Message})
		} else {
			for _, d := range result.Disruptions.Active {
				rows = append(rows, []string{"disruption", d.LineID, d.LineName, d.Category, d.Type, disruptionText(d)})
			}
		}
		rows = append(rows, []string{"accessibility", result.Accessibility.Status, strings.Join(result.Accessibility.Flags, ","), strings.Join(result.Accessibility.Notes, " ")})
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	fmt.Fprintf(stdout, "%s -> %s\n", result.From, result.To)
	if result.Selected == nil {
		fmt.Fprintln(stdout, result.Message)
		return exitcode.OK
	}
	fmt.Fprintf(stdout, "Selected: depart %s, arrive %s (%d min), %s\n", hhmm(result.Selected.StartDateTime), hhmm(result.Selected.ArrivalDateTime), result.Selected.DurationMinutes, formatTripFare(result.Fare))
	for _, leg := range result.Selected.Legs {
		line := ""
		if leg.Line != "" {
			line = " [" + leg.Line + "]"
		}
		fmt.Fprintf(stdout, "  %s %s%s: %s -> %s\n", hhmm(leg.DepartureTime), strings.ToUpper(leg.Mode), line, leg.From, leg.To)
	}
	if len(result.Disruptions.Active) == 0 {
		fmt.Fprintf(stdout, "Disruptions: %s\n", result.Disruptions.Message)
	} else {
		fmt.Fprintln(stdout, "Disruptions:")
		for _, d := range result.Disruptions.Active {
			label := firstNonEmptyString(d.LineName, d.LineID, d.Category)
			fmt.Fprintf(stdout, "  %s: %s\n", label, disruptionText(d))
		}
	}
	fmt.Fprintf(stdout, "Accessibility: %s\n", strings.Join(result.Accessibility.Notes, " "))
	if len(result.Journeys) > 1 {
		fmt.Fprintln(stdout, "Alternatives:")
		for _, journey := range result.Journeys[1:] {
			fmt.Fprintf(stdout, "  Option %d: depart %s, arrive %s (%d min) via %s\n", journey.Option, hhmm(journey.StartDateTime), hhmm(journey.ArrivalDateTime), journey.DurationMinutes, tripLineNames(journey.Lines))
		}
	}
	return exitcode.OK
}

func formatTripFare(fare tfl.FareQuote) string {
	if fare.Status == "ok" && fare.AmountPence > 0 {
		return formatPounds(fare.AmountPence)
	}
	return "fare unavailable"
}

func tflFares(ctx context.Context, g globals, client *tfl.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fares", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "origin station")
	to := fs.String("to", "", "destination station")
	fromID := fs.String("from-id", "", "origin TfL stop ID")
	toID := fs.String("to-id", "", "destination TfL stop ID")
	fromZone := fs.Int("from-zone", 0, "origin fare zone")
	toZone := fs.Int("to-zone", 0, "destination fare zone")
	passenger := fs.String("passenger", "Adult", "TfL passenger type")
	payment := fs.String("payment", "contactless", "contactless, oyster, or cash")
	date := fs.String("date", "", "YYYYMMDD")
	when := fs.String("time", "", "HHmm")
	period := fs.String("period", "peak", "peak, off-peak, or anytime for zonal lookup")
	mode := fs.String("mode", "tube,dlr,overground,elizabeth-line,national-rail", "station search modes for --from/--to")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *fromZone != 0 || *toZone != 0 {
		if *from != "" || *to != "" || *fromID != "" || *toID != "" {
			fmt.Fprintln(stderr, "--from-zone/--to-zone cannot be combined with station flags")
			return exitcode.Usage
		}
		if *fromZone == 0 || *toZone == 0 {
			fmt.Fprintln(stderr, "--from-zone and --to-zone are required together")
			return exitcode.Usage
		}
		quote, code := zoneFareQuote(*fromZone, *toZone, *passenger, *payment, *period)
		return writeFareQuote(g, stdout, stderr, quote, code)
	}
	if *from == "" || *to == "" {
		fmt.Fprintln(stderr, "provide either --from/--to or --from-zone/--to-zone")
		return exitcode.Usage
	}
	if *passenger != "" && !strings.EqualFold(*passenger, "Adult") {
		quote := tfl.FareQuote{Status: "unsupported", Message: "Station fare lookup currently supports Adult PAYG/contactless fares only.", Kind: "journey", From: *from, To: *to, PassengerType: *passenger, Currency: "GBP", Fares: []tfl.FareOption{}, Source: "TfL Journey Planner fare"}
		return writeFareQuote(g, stdout, stderr, quote, exitcode.NoData)
	}
	stationPayment := canonicalFarePayment(*payment)
	if stationPayment != "contactless" && stationPayment != "oyster" {
		quote := tfl.FareQuote{Status: "unsupported", Message: "Station fare lookup currently supports contactless and Oyster fares only.", Kind: "journey", From: *from, To: *to, PassengerType: *passenger, Payment: stationPayment, Currency: "GBP", Fares: []tfl.FareOption{}, Source: "TfL Journey Planner fare"}
		return writeFareQuote(g, stdout, stderr, quote, exitcode.NoData)
	}
	resolvedFrom := tfl.MatchedStop{Name: *from, ID: *fromID}
	resolvedTo := tfl.MatchedStop{Name: *to, ID: *toID}
	var err error
	if resolvedFrom.ID == "" {
		resolvedFrom, err = resolveFareStation(ctx, client, *from, csvArgs(*mode))
		if err != nil {
			return writeFareResolutionError(g, stdout, stderr, "from", *from, err)
		}
	}
	if resolvedTo.ID == "" {
		resolvedTo, err = resolveFareStation(ctx, client, *to, csvArgs(*mode))
		if err != nil {
			return writeFareResolutionError(g, stdout, stderr, "to", *to, err)
		}
	}
	resp, err := client.Journey(ctx, firstNonEmptyString(resolvedFrom.ID, *from), firstNonEmptyString(resolvedTo.ID, *to), tfl.JourneyOptions{Date: *date, Time: *when, Modes: csvArgs(*mode), Preference: "LeastTime"})
	if err != nil {
		if code, ok := writeStructuredTfLError(g, stdout, stderr, err); ok {
			return code
		}
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	quote := journeyFareQuote(resp, resolvedFrom, resolvedTo, *passenger, stationPayment, *date, *when)
	code := exitcode.OK
	if quote.Status != "ok" {
		code = exitcode.NoData
	}
	return writeFareQuote(g, stdout, stderr, quote, code)
}

func journeyFareQuote(resp tfl.JourneyResponse, from, to tfl.MatchedStop, passenger, payment, date, when string) tfl.FareQuote {
	quote := tfl.FareQuote{Status: "no_data", Kind: "journey", From: from.Name, FromID: from.ID, To: to.Name, ToID: to.ID, PassengerType: passenger, Payment: canonicalFarePayment(payment), Currency: "GBP", Fares: []tfl.FareOption{}, Source: "TfL Journey Planner fare", Notes: []string{"TfL Journey Planner fares can vary by route, direction, time, and service."}}
	if date != "" || when != "" {
		quote.Time = strings.TrimSpace(strings.TrimSpace(date) + " " + strings.TrimSpace(when))
	}
	if len(resp.Journeys) == 0 || resp.Journeys[0].Fare == nil {
		quote.Message = "TfL returned no fare for that journey."
		return quote
	}
	fare := resp.Journeys[0].Fare
	quote.Status = "ok"
	quote.AmountPence = fare.TotalCost
	for _, item := range fare.Fares {
		name := strings.TrimSpace(item.ChargeLevel)
		if name == "" {
			name = "Fare"
		}
		option := tfl.FareOption{Name: name, Payment: []string{"contactless", "oyster"}, Time: canonicalFareTime(item.ChargeLevel), AmountPence: firstNonZeroInt(item.Cost, fare.TotalCost), Currency: "GBP"}
		quote.Fares = append(quote.Fares, option)
		if item.LowZone > 0 && item.HighZone > 0 {
			quote.Zones = inclusiveZones(item.LowZone, item.HighZone)
		}
	}
	if len(quote.Fares) == 0 && fare.TotalCost > 0 {
		quote.Fares = append(quote.Fares, tfl.FareOption{Name: "Fare", Payment: []string{"contactless", "oyster"}, AmountPence: fare.TotalCost, Currency: "GBP"})
	}
	quote.Message = fmt.Sprintf("TfL fare from %s to %s is %s.", quote.From, quote.To, formatPounds(quote.AmountPence))
	return quote
}

func resolveFareStation(ctx context.Context, client *tfl.Client, query string, modes []string) (tfl.MatchedStop, error) {
	resp, err := client.StopSearchWithOptions(ctx, query, tfl.StopSearchOptions{Modes: modes, MaxResults: 1, IncludeHubs: true})
	if err != nil {
		return tfl.MatchedStop{}, err
	}
	if len(resp.Matches) == 0 {
		return tfl.MatchedStop{}, fmt.Errorf("TfL found no station matching %q", query)
	}
	match := resp.Matches[0]
	if strings.TrimSpace(match.ID) == "" {
		return tfl.MatchedStop{}, fmt.Errorf("TfL station match for %q has no stop ID", query)
	}
	return match, nil
}

func writeFareResolutionError(g globals, stdout, stderr io.Writer, field, query string, err error) int {
	result := map[string]any{"status": "station_not_found", "field": field, "query": query, "message": err.Error()}
	if g.format == output.JSON {
		if code := writeJSONWithOK(g, stdout, stderr, result, false, false); code != exitcode.OK {
			return code
		}
		return exitcode.NoData
	}
	fmt.Fprintln(stderr, err)
	return exitcode.NoData
}

type zoneFareBand struct {
	PeakPence      int
	OffPeakPence   int
	CashPence      int
	DailyCapPence  int
	WeeklyCapPence int
}

var zoneOneFareBands = map[int]zoneFareBand{
	1: {PeakPence: 310, OffPeakPence: 300, CashPence: 700, DailyCapPence: 890, WeeklyCapPence: 4470},
	2: {PeakPence: 360, OffPeakPence: 310, CashPence: 700, DailyCapPence: 890, WeeklyCapPence: 4470},
	3: {PeakPence: 390, OffPeakPence: 330, CashPence: 700, DailyCapPence: 1050, WeeklyCapPence: 5250},
	4: {PeakPence: 480, OffPeakPence: 360, CashPence: 700, DailyCapPence: 1280, WeeklyCapPence: 6420},
	5: {PeakPence: 530, OffPeakPence: 380, CashPence: 700, DailyCapPence: 1530, WeeklyCapPence: 7640},
	6: {PeakPence: 590, OffPeakPence: 400, CashPence: 700, DailyCapPence: 1630, WeeklyCapPence: 8160},
}

func zoneFareQuote(fromZone, toZone int, passenger, payment, when string) (tfl.FareQuote, int) {
	payment = canonicalFarePayment(payment)
	when = canonicalFareTime(when)
	if payment == "cash" {
		when = "anytime"
	}
	minZone, maxZone := minMaxZone(fromZone, toZone)
	quote := tfl.FareQuote{Status: "ok", Kind: "zonal", FromZone: fromZone, ToZone: toZone, PassengerType: passenger, Payment: payment, Time: when, Currency: "GBP", Source: "TfL 2026 adult PAYG fares and caps", Notes: []string{"For exact station pairs, use tfl fares --from/--to because some fares vary by route, direction, and National Rail acceptance."}}
	if passenger != "" && !strings.EqualFold(passenger, "Adult") {
		quote.Status = "unsupported"
		quote.Message = "Zonal fare lookup currently supports Adult fares only; use station fare finder for other passenger types."
		quote.Fares = []tfl.FareOption{}
		return quote, exitcode.NoData
	}
	if minZone < 1 || maxZone > 6 {
		quote.Status = "unsupported"
		quote.Message = "Zonal fare lookup currently supports zones 1-6 only."
		quote.Fares = []tfl.FareOption{}
		return quote, exitcode.NoData
	}
	quote.Zones = inclusiveZones(fromZone, toZone)
	var band zoneFareBand
	if minZone == 1 {
		band = zoneOneFareBands[maxZone]
	} else if fromZone == toZone {
		band = zoneFareBand{PeakPence: 230, OffPeakPence: 220, CashPence: 700}
	} else {
		quote.Status = "unsupported"
		quote.Message = "Non-Zone-1 multi-zone single fares vary; use station fare finder with --from and --to."
		quote.Fares = []tfl.FareOption{}
		return quote, exitcode.NoData
	}
	quote.Fares = []tfl.FareOption{
		{Name: "Peak", Payment: []string{"contactless", "oyster"}, Time: "peak", AmountPence: band.PeakPence, Currency: "GBP"},
		{Name: "Off Peak", Payment: []string{"contactless", "oyster"}, Time: "off-peak", AmountPence: band.OffPeakPence, Currency: "GBP"},
		{Name: "Cash", Payment: []string{"cash"}, Time: "anytime", AmountPence: band.CashPence, Currency: "GBP"},
	}
	if band.DailyCapPence > 0 {
		quote.Fares = append(quote.Fares, tfl.FareOption{Name: "Daily cap", Payment: []string{"contactless", "oyster"}, Time: "anytime", AmountPence: band.DailyCapPence, Currency: "GBP"})
	}
	if band.WeeklyCapPence > 0 {
		quote.Fares = append(quote.Fares, tfl.FareOption{Name: "Weekly cap", Payment: []string{"contactless", "oyster"}, Time: "anytime", AmountPence: band.WeeklyCapPence, Currency: "GBP"})
	}
	quote.AmountPence = selectFareAmount(quote.Fares, payment, when)
	if quote.AmountPence == 0 {
		quote.Status = "unsupported"
		quote.Message = "No matching fare for that payment/time combination."
		if when == "anytime" && (payment == "contactless" || payment == "oyster") {
			quote.Message = "Contactless and Oyster single fares need --period peak or --period off-peak; caps are listed separately."
		}
		return quote, exitcode.NoData
	}
	quote.Message = fmt.Sprintf("%s fare for zones %d-%d is %s.", fareLabel(when, payment), minZone, maxZone, formatPounds(quote.AmountPence))
	return quote, exitcode.OK
}

func writeFareQuote(g globals, stdout, stderr io.Writer, quote tfl.FareQuote, code int) int {
	if quote.Fares == nil {
		quote.Fares = []tfl.FareOption{}
	}
	if g.format == output.JSON {
		if writeCode := writeJSONWithOK(g, stdout, stderr, quote, code == exitcode.OK, false); writeCode != exitcode.OK {
			return writeCode
		}
		return code
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, fare := range quote.Fares {
			rows = append(rows, []string{quote.Status, quote.Kind, fare.Name, strings.Join(fare.Payment, ","), fare.Time, formatPounds(fare.AmountPence)})
		}
		_ = output.WritePlainRows(stdout, rows)
		return code
	}
	fmt.Fprintln(stdout, quote.Message)
	for _, fare := range quote.Fares {
		fmt.Fprintf(stdout, "  %s: %s\n", fare.Name, formatPounds(fare.AmountPence))
	}
	return code
}

func canonicalFarePayment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "contactless":
		return "contactless"
	case "oyster":
		return "oyster"
	case "cash":
		return "cash"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func canonicalFareTime(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "peak":
		return "peak"
	case "offpeak", "off-peak", "off peak":
		return "off-peak"
	case "any", "anytime":
		return "anytime"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func selectFareAmount(fares []tfl.FareOption, payment, when string) int {
	for _, fare := range fares {
		if isFareCap(fare) {
			continue
		}
		if when == "anytime" {
			if fare.Time != "anytime" {
				continue
			}
		} else if fare.Time != when {
			continue
		}
		if fareSupportsPayment(fare, payment) {
			return fare.AmountPence
		}
	}
	return 0
}

func isFareCap(fare tfl.FareOption) bool {
	return strings.Contains(strings.ToLower(fare.Name), "cap")
}

func fareSupportsPayment(fare tfl.FareOption, payment string) bool {
	for _, option := range fare.Payment {
		if strings.EqualFold(option, payment) {
			return true
		}
	}
	return payment == ""
}

func fareLabel(when, payment string) string {
	parts := strings.Fields(strings.ReplaceAll(when+" "+payment, "-", " "))
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, " ")
}

func formatPounds(pence int) string {
	return fmt.Sprintf("£%d.%02d", pence/100, pence%100)
}

func minMaxZone(a, b int) (int, int) {
	if a < b {
		return a, b
	}
	return b, a
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func inclusiveZones(a, b int) []int {
	minZone, maxZone := minMaxZone(a, b)
	zones := make([]int, 0, maxZone-minZone+1)
	for zone := minZone; zone <= maxZone; zone++ {
		zones = append(zones, zone)
	}
	return zones
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

func defaultAccessibleStationStopTypes(modes []string) []string {
	if len(modes) == 0 {
		return []string{"NaptanMetroStation", "NaptanRailStation", "NaptanFerryPort", "NaptanPublicBusCoachTram", "NaptanCoachStation"}
	}
	seen := map[string]bool{}
	var out []string
	add := func(stopType string) {
		if !seen[stopType] {
			seen[stopType] = true
			out = append(out, stopType)
		}
	}
	for _, mode := range modes {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "tube", "dlr", "tram":
			add("NaptanMetroStation")
		case "elizabeth-line":
			add("NaptanMetroStation")
			add("NaptanRailStation")
		case "overground", "national-rail", "train":
			add("NaptanRailStation")
		case "river", "river-bus", "river-tour":
			add("NaptanFerryPort")
		case "bus":
			add("NaptanPublicBusCoachTram")
		case "coach":
			add("NaptanCoachStation")
		}
	}
	if len(out) == 0 {
		return []string{"NaptanMetroStation", "NaptanRailStation"}
	}
	return out
}

func accessibleStationPropertyCategories() []string {
	return []string{"Accessibility", "Facility"}
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

func writeAccessibleStationsResult(g globals, stdout, stderr io.Writer, result accessibleStationsResult) int {
	if g.format == output.JSON {
		return writeJSONWithOK(g, stdout, stderr, result, result.Status == "ok", result.Status != "ok")
	}
	if g.format == output.Plain {
		var rows [][]string
		for _, station := range result.Stations {
			rows = append(rows, []string{
				station.ID,
				station.Name,
				fmt.Sprintf("%.0f", accessibleStationDistanceMeters(station)),
				station.AccessStatus,
				strconv.FormatBool(station.StepFreeAccess),
				strconv.FormatBool(station.LiftPresent),
				formatOptionalInt(station.Lifts),
				formatOptionalBool(station.AccessViaLift),
				formatOptionalBool(station.LimitedCapacityLift),
				formatOptionalBool(station.SpecificEntranceRequired),
				fmt.Sprintf("%.5f", station.Lat),
				fmt.Sprintf("%.5f", station.Lon),
				strings.Join(station.Modes, ","),
			})
		}
		if len(rows) == 0 {
			rows = append(rows, []string{result.Status, result.Message})
		}
		_ = output.WritePlainRows(stdout, rows)
		return exitcode.OK
	}
	if len(result.Stations) == 0 {
		fmt.Fprintln(stdout, result.Message)
		return exitcode.OK
	}
	for _, station := range result.Stations {
		fmt.Fprintf(stdout, "%s\t%.0fm\t%s\t%s\t%s\n", station.Name, accessibleStationDistanceMeters(station), humanAccessStatus(station.AccessStatus), liftLabel(station.Lifts), accessViaLiftLabel(station.AccessViaLift))
	}
	return exitcode.OK
}

func accessibleStationDistanceMeters(station accessibleStation) float64 {
	if station.Distance == nil {
		return 0
	}
	return *station.Distance
}

func liftLabel(lifts *int) string {
	if lifts == nil {
		return "lifts unknown"
	}
	if *lifts == 1 {
		return "1 lift"
	}
	return fmt.Sprintf("%d lifts", *lifts)
}

func humanAccessStatus(status string) string {
	switch status {
	case "confirmed_step_free":
		return "confirmed step-free"
	case "lift_present_unconfirmed":
		return "lift present, step-free unconfirmed"
	case "no_lift_access":
		return "no access via lift"
	case "no_lifts":
		return "no lifts"
	default:
		return "unknown accessibility"
	}
}

func accessViaLiftLabel(accessViaLift *bool) string {
	if accessViaLift == nil {
		return "access via lift unknown"
	}
	if *accessViaLift {
		return "access via lift yes"
	}
	return "access via lift no"
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func formatOptionalBool(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
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

func canonicalAccessibilityPreferences(value string) []string {
	var out []string
	for _, pref := range csvArgs(value) {
		switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(pref), "-", "")) {
		case "nosolidstairs", "nostairs":
			out = append(out, "NoSolidStairs")
		case "noescalators":
			out = append(out, "NoEscalators")
		case "noelevators", "nolifts":
			out = append(out, "NoElevators")
		case "stepfreetovehicle", "vehicle":
			out = append(out, "StepFreeToVehicle")
		case "stepfreetoplatform", "platform", "stepfree":
			out = append(out, "StepFreeToPlatform")
		default:
			out = append(out, pref)
		}
	}
	return out
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
	fmt.Fprintln(w, "  tfl accessible-stations ...  Find nearby stations with lift/access data")
	fmt.Fprintln(w, "  tfl stop-search <query>      Search TfL stops and stations")
	fmt.Fprintln(w, "  tfl stop-info --stop ID      Show a stop point and child stops")
	fmt.Fprintln(w, "  tfl arrivals --stop ID       Show live arrivals")
	fmt.Fprintln(w, "  tfl next-arrival ...         Resolve a stop query and show the next arrival")
	fmt.Fprintln(w, "  tfl journey --from A --to B  Plan a London journey")
	fmt.Fprintln(w, "  tfl trip --from A --to B     Plan a trip with fare, disruptions, and access notes")
	fmt.Fprintln(w, "  tfl fare ...                 Estimate TfL PAYG fares")
	fmt.Fprintln(w, "  tfl watch-arrival ...        One-shot live arrival check")
}
