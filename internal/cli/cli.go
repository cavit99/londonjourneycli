package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
	"github.com/cavit99/londonjourneycli/internal/notify"
	"github.com/cavit99/londonjourneycli/internal/output"
	"github.com/cavit99/londonjourneycli/internal/skill"
	"github.com/cavit99/londonjourneycli/internal/tfl"
)

const version = "0.1.2"

type globals struct {
	format    output.Format
	skillsDir string
	timeout   time.Duration
	noInput   bool
	verbose   bool
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	g, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
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
		case "--plain":
			g.format = output.Plain
		case "--no-input":
			g.noInput = true
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
			return g, args[i:], nil
		}
	}
	return g, nil, nil
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

func cmdList(ctx context.Context, g globals, args []string, stdout, stderr io.Writer) int {
	skills, err := discover(g)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	switch g.format {
	case output.JSON:
		_ = output.WriteJSON(stdout, skills)
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
		_ = output.WriteJSON(stdout, matches)
		return exitcode.OK
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
		_ = output.WriteJSON(stdout, s)
		return exitcode.OK
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
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, issues)
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
	for _, issue := range issues {
		if issue.Severity == "error" {
			return exitcode.Generic
		}
	}
	return exitcode.OK
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
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, checks)
	} else {
		for _, c := range checks {
			status := "ok"
			if !c.OK {
				status = "fail"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", status, c.Name, c.Message)
		}
	}
	for _, c := range checks {
		if !c.OK {
			return exitcode.Config
		}
	}
	return exitcode.OK
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
		_ = output.WriteJSON(stdout, results)
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
		fmt.Fprintln(stderr, "usage: londonjourneycli tfl <stop-search|stop-info|arrivals|next-arrival|journey|watch-arrival>")
		return exitcode.Usage
	}
	client := tfl.NewClient(os.Getenv("TFL_APP_KEY"))
	if base := os.Getenv("TFL_BASE_URL"); base != "" {
		client.BaseURL = base
	}
	switch args[0] {
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
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, info)
		return exitcode.OK
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
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	if limit < len(resp.Matches) {
		resp.Matches = resp.Matches[:limit]
	}
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, resp)
		return exitcode.OK
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
	line := fs.String("line", "", "line filter")
	towards := fs.String("towards", "", "towards/destination substring")
	direction := fs.String("direction", "", "line-arrivals direction: inbound, outbound, or all")
	destinationStop := fs.String("destination-stop", "", "TfL destination stop ID for line-arrivals filtering")
	limit := fs.Int("limit", 5, "maximum arrivals")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}
	if *stop == "" {
		fmt.Fprintln(stderr, "--stop is required")
		return exitcode.Usage
	}
	if *limit < 0 {
		fmt.Fprintln(stderr, "--limit must be >= 0")
		return exitcode.Usage
	}
	arrivals, err := client.LineArrivals(ctx, *stop, csvArgs(*line), *direction, *destinationStop)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.Network
	}
	arrivals = tfl.FilterArrivals(arrivals, "", *towards)
	if *limit < len(arrivals) {
		arrivals = arrivals[:*limit]
	}
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, arrivals)
		return exitcode.OK
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
		return 0, false
	}
	status := "api_error"
	message := err.Error()
	code := exitcode.Network
	if apiErr.StatusCode == http.StatusMultipleChoices && apiErr.Disambiguation != nil {
		status = "ambiguous"
		message = "TfL needs a more specific place. Resolve the origin, destination, or via point to a postcode, station/stop ID, coordinates, or exact address, then retry."
		code = exitcode.Usage
	}
	if writeErr := output.WriteJSON(stdout, tflErrorResult{Status: status, Message: message, Error: apiErr}); writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return exitcode.Generic, true
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
		writeNextArrivalResult(g, stdout, result)
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
		writeNextArrivalResult(g, stdout, result)
		return exitcode.NoData
	}
	next := arrivals[0]
	result.Status = "ok"
	result.Next = &next
	result.Message = fmt.Sprintf("Next %s from %s is about %d min away towards %s.", next.LineName, stopLabel, roundMinutes(next.TimeToStation), next.DestinationName)
	writeNextArrivalResult(g, stdout, result)
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
		_ = output.WriteJSON(stdout, resp)
		return exitcode.OK
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
			writeWatchResult(g, stdout, watchResult{Status: "api_failed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: false, NotificationError: sendErr.Error()})
			fmt.Fprintln(stderr, sendErr)
			return exitcode.Generic
		}
		writeWatchResult(g, stdout, watchResult{Status: "api_failed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: notificationOK})
		return exitcode.Network
	}
	if !found.StopFound {
		msg := fmt.Sprintf("Live transport update: TfL found no stop matching %q.", *query)
		notificationOK, err := sendWatchNotification(ctx, sender, msg)
		if err != nil {
			writeWatchResult(g, stdout, watchResult{Status: "stop_not_found", Message: msg, Query: found.Query, Candidates: found.Candidates, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		writeWatchResult(g, stdout, watchResult{Status: "stop_not_found", Message: msg, Query: found.Query, Candidates: found.Candidates, Notification: msg, NotificationOK: notificationOK})
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
			writeWatchResult(g, stdout, watchResult{Status: "no_data", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		writeWatchResult(g, stdout, watchResult{Status: "no_data", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Notification: msg, NotificationOK: notificationOK})
		return exitcode.NoData
	}

	next := arrivals[0]
	dueIn := time.Duration(next.TimeToStation) * time.Second
	if dueIn <= *threshold {
		msg := fmt.Sprintf("Transport heads-up: live TfL says the %s is about %d min away from %s.", next.LineName, roundMinutes(next.TimeToStation), next.StationName)
		notificationOK, err := sendWatchNotification(ctx, sender, msg)
		if err != nil {
			writeWatchResult(g, stdout, watchResult{Status: "due", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, Notification: msg, NotificationOK: false, NotificationError: err.Error()})
			fmt.Fprintln(stderr, err)
			return exitcode.Generic
		}
		writeWatchResult(g, stdout, watchResult{Status: "due", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, Notification: msg, NotificationOK: notificationOK})
		return exitcode.OK
	}

	nextCheck := next.ExpectedArrival.Add(-*threshold)
	msg := fmt.Sprintf("Transport update: live TfL says the %s has slipped to %s, about %d min away. Check again around %s.", next.LineName, next.ExpectedArrival.Local().Format("15:04"), roundMinutes(next.TimeToStation), nextCheck.Local().Format("15:04"))
	notificationOK, err := sendWatchNotification(ctx, sender, msg)
	if err != nil {
		writeWatchResult(g, stdout, watchResult{Status: "delayed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, NextCheckAt: nextCheck.Format(time.RFC3339), Notification: msg, NotificationOK: false, NotificationError: err.Error()})
		fmt.Fprintln(stderr, err)
		return exitcode.Generic
	}
	writeWatchResult(g, stdout, watchResult{Status: "delayed", Message: msg, Query: found.Query, Candidates: found.Candidates, ResolvedStop: found.Stop, Arrival: &next, NextCheckAt: nextCheck.Format(time.RFC3339), Notification: msg, NotificationOK: notificationOK})
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

func writeNextArrivalResult(g globals, stdout io.Writer, result nextArrivalResult) {
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, result)
		return
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
		return
	}
	fmt.Fprintln(stdout, result.Message)
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

func writeWatchResult(g globals, stdout io.Writer, result watchResult) {
	if g.format == output.JSON {
		_ = output.WriteJSON(stdout, result)
		return
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
		return
	}
	fmt.Fprintln(stdout, result.Message)
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
	fmt.Fprintln(w, "  tfl stop-search <query>      Search TfL stops and stations")
	fmt.Fprintln(w, "  tfl stop-info --stop ID      Show a stop point and child stops")
	fmt.Fprintln(w, "  tfl arrivals --stop ID       Show live arrivals")
	fmt.Fprintln(w, "  tfl next-arrival ...         Resolve a stop query and show the next arrival")
	fmt.Fprintln(w, "  tfl journey --from A --to B  Plan a London journey")
	fmt.Fprintln(w, "  tfl watch-arrival ...        One-shot live arrival check")
}
