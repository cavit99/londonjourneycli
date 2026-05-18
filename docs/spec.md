# LondonJourneyCLI CLI Spec

## Usage

    londonjourneycli [global flags] <command> [args]

## Global Flags

- --json stable JSON output
- --output <path> project JSON output by dot path; requires --json and must appear before the command
- --envelope wrap JSON output in {ok,schemaVersion,command,requestedAt,data}; requires --json; ok mirrors whether the command exits 0; sensitive delivery target/token args are redacted from command
- --plain stable tab-separated output for LondonJourneyCLI-owned output
- --skills-dir <dir> skill root override
- --timeout <duration> command timeout, default 30s
- --no-input declare non-interactive use and export LONDONJOURNEYCLI_NO_INPUT=1 to manifest commands

## Commands

Registry:

- londonjourneycli list
- londonjourneycli search <query>
- londonjourneycli show <skill>
- londonjourneycli lint
- londonjourneycli doctor <skill>
- londonjourneycli run <skill> <command> [args...]
- londonjourneycli test <skill>

TfL:

- londonjourneycli tfl status [--line line-ids] [--mode modes]
- londonjourneycli tfl disruptions [--line line-ids] [--mode modes]
- londonjourneycli tfl line-routes --line line-ids
- londonjourneycli tfl nearby-stops (--lat latitude --lon longitude|--location text) [--radius metres] [--mode modes] [--stop-type stopTypes] [--limit N]
- londonjourneycli tfl accessible-stations (--near query|--lat latitude --lon longitude|--location text) [--mode modes|all] [--radius metres] [--stop-type stopTypes] [--require-step-free] [--require-lift] [--limit N]
- londonjourneycli tfl stop-search <query> [--mode modes] [--line lines] [--max-results N] [--include-hubs] [--limit N]
- londonjourneycli tfl stop-info --stop <id>
- londonjourneycli tfl arrivals (--stop <id>|--query <stop name>) [--line N] [--towards text] [--direction inbound|outbound|all] [--destination-stop id] [--mode bus|all|modes] [--search-limit N] [--limit N]
- londonjourneycli tfl next-arrival (--stop <id>|--query <stop name>) --line <line> [--towards text] [--direction inbound|outbound|all] [--destination-stop id] [--mode bus|all|modes] [--search-limit N] [--limit N]
- londonjourneycli tfl journey --from <origin> --to <destination> [--date YYYYMMDD] [--time HHmm] [--arriving] [--via point] [--preference LeastTime|LeastInterchange|LeastWalking] [--mode modes] [--accessibility prefs] [--max-walking-minutes N] [--max-transfer-minutes N] [--walking-speed Slow|Average|Fast] [--include-alternatives] [--alternative-walking] [--alternative-cycle] [--cycle-preference pref] [--real-time] [--between-entrances] [--local-only]
- londonjourneycli tfl fare|fares (--from <station> --to <station> [--from-id id] [--to-id id] | --from-zone N --to-zone N) [--date YYYYMMDD] [--time HHmm] [--period peak|off-peak|anytime] [--payment contactless|oyster|cash] [--passenger Adult] [--mode modes]
- londonjourneycli tfl watch-arrival (--stop <id>|--query <stop name>) --line <line> [--towards text] [--direction inbound|outbound|all] [--destination-stop id] [--mode bus|all|modes] [--search-limit N] [--threshold 2m] [--openclaw-channel whatsapp --openclaw-target <target>] [--dry-run]

watch-arrival requires --threshold > 0. In --no-input mode, it also requires complete OpenClaw delivery flags or --dry-run. With delivery configured, due, delayed, no_data, stop_not_found, and api_failed are all visible terminal states.

In JSON mode, journey ambiguity from TfL is structured as status "ambiguous" with error.statusCode 300 and error.disambiguation candidate options. Agents should resolve and retry rather than scraping stderr.

## Exit Codes

- 0 success
- 1 generic failure
- 2 usage, validation, or ambiguous TfL place input
- 3 configuration or missing requirement
- 4 network or API failure
- 5 no data

Manifest child command failures are mapped to exit code 1. JSON and plain test output include childExitCode when a child process exits non-zero.

## Streams

- stdout contains the result.
- stderr contains diagnostics.
- JSON mode never mixes logs into stdout.
- run streams child stdout/stderr directly; use test --json or test --plain when a structured manifest-command result is needed.

## Skill Discovery

Default roots are:

- ./skills from the current working directory
- the current directory when its basename is skills
- LONDONJOURNEYCLI_SKILLS_DIR, split with the OS path-list separator
- ~/.config/londonjourneycli/skills
- ~/.local/share/londonjourneycli/skills

Pass --skills-dir <dir> to use exactly one root for a command.

JSON list/show responses include local skill paths. This is useful for agents but may reveal absolute paths in logs.

Use --output when a caller needs one JSON field rather than the full object, for example:

    londonjourneycli --json --output 0.lineStatuses.0.statusSeverityDescription tfl status --line victoria

## Manifest Command Execution

- command exec and test command fields are argv arrays.
- global flags stop parsing at the first command token, so flags after run subcommands pass through to the child command.
- command timeout fields are parsed as Go durations and bound the child process.
- londonjourneycli test returns structured results in --json and --plain modes.
