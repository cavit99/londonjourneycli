# londonjourneycli

A small Go CLI for London journey planning, live TfL arrivals, line status, disruptions, and agent-safe transport alerts.

It also includes a narrow skill manifest runner so an agent can discover, test, and call the transport skill without relying on prose alone.

## Why this exists

Agent skills tend to start as prose. That is useful, but prose alone cannot answer:

- Is this skill installed here?
- Which skill owns this domain?
- What tools or env vars does it require?
- Can it safely run from cron?
- What command should an agent call?
- Did the command actually produce a user-visible outcome?

LondonJourneyCLI gives those questions a small deterministic runtime while keeping the TfL commands first-class.

Unlike broad TfL MCP servers, this is intentionally shell-first: easy to install on a Mac mini or server, stable to call from OpenClaw/cron, and strict about visible outcomes for live transport alerts.

## Install

From source:

    go install github.com/cavit99/londonjourneycli/cmd/londonjourneycli@latest

Requires Go 1.24 or newer.

From a checkout:

    go build -o bin/londonjourneycli ./cmd/londonjourneycli

For local agent use, put the binary somewhere on PATH:

    go build -o ~/.local/bin/londonjourneycli ./cmd/londonjourneycli

## Quick Start

Point LondonJourneyCLI at your skill roots explicitly, or set LONDONJOURNEYCLI_SKILLS_DIR to a path-list. From this repository you can inspect the bundled example directly from source:

    go run ./cmd/londonjourneycli --skills-dir ./examples/skills list
    go run ./cmd/londonjourneycli --skills-dir ./examples/skills show tfl-journey

Build or install the binary before running manifest doctor/test checks, because the example manifest deliberately verifies that londonjourneycli is on PATH:

    go build -o ~/.local/bin/londonjourneycli ./cmd/londonjourneycli
    londonjourneycli --skills-dir ./examples/skills doctor tfl-journey

For your own skills:

    export LONDONJOURNEYCLI_SKILLS_DIR="$HOME/.config/londonjourneycli/skills"
    londonjourneycli search tfl
    londonjourneycli --json show tfl-journey
    londonjourneycli lint
    londonjourneycli doctor tfl-journey

TfL examples:

    londonjourneycli tfl stop-search "London Bridge"
    londonjourneycli tfl stop-search "Ildersly Grove" --mode bus --line N3
    londonjourneycli --json tfl stop-info --stop 490G00008459
    londonjourneycli tfl status --line victoria
    londonjourneycli --json tfl disruptions --mode tube,dlr,elizabeth-line,overground,tram
    londonjourneycli --json --output 0.lineStatuses.0.statusSeverityDescription tfl status --line victoria
    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli tfl journey --from "Westminster" --to "Waterloo" --preference LeastWalking --max-walking-minutes 15
    londonjourneycli --json tfl arrivals --stop 490000235N --line 43
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3

For agents, resolve fuzzy places before calling journey: turn "home", "office", venue names, and vague areas into exact addresses, postcodes, coordinates, or TfL IDs. If TfL still returns JSON status "ambiguous", use the returned disambiguation options to retry or ask one clarification.

## Skill Manifests

A skill directory can contain SKILL.md and optional skill.yaml.

SKILL.md remains the agent playbook. skill.yaml is the executable contract.

    name: tfl-journey
    description: Plan London journeys and check live TfL arrivals through LondonJourneyCLI.
    ownerDomain: london-transport
    triggers:
      - how do I get to
      - next bus
    safetyLevel: read-only
    cronSafe: true
    requires:
      bins: [londonjourneycli]
    commands:
      - name: journey
        exec: [londonjourneycli, tfl, journey]
    tests:
      - name: stop-search-smoke
        command: [londonjourneycli, --json, tfl, stop-search, London Bridge, --mode, "tube,bus", --limit, "1"]

Commands and tests are argv arrays, not shell strings. That is deliberate: no quoting lottery and no shell injection by default.

## Output Contract

- stdout is data.
- stderr is diagnostics.
- --json is stable and intended for agents.
- --output <path> projects JSON output by dot path; pass it before the command, for example --json --output 0.name.
- --plain is tab-separated and scriptable for LondonJourneyCLI-owned output.
- --no-input declares unattended use and exports LONDONJOURNEYCLI_NO_INPUT=1 to manifest commands.
- command execution receives no interactive stdin by default.
- LondonJourneyCLI's own exit codes are stable:
  - 0 success
  - 1 generic failure
  - 2 usage error
  - 3 config, auth, or missing requirement
  - 4 network or API failure
  - 5 no data

Manifest child commands may fail internally; LondonJourneyCLI maps child failure to exit code 1 and preserves the raw child exit code in JSON and plain test results. The run command is pass-through by design and streams child stdout/stderr directly.

## Time-Critical Alerts

If a missed message can cause real-world inconvenience, silence is a bug.

londonjourneycli tfl watch-arrival performs a deterministic one-shot live check and can deliver through OpenClaw:

    londonjourneycli --json --no-input tfl watch-arrival --query "Ildersly Grove" --line N3 --threshold 2m --openclaw-channel whatsapp --openclaw-target +15555550123

When delivery is configured, it sends on due, delayed, no-data, and API-failure states. If the send itself fails, LondonJourneyCLI returns non-zero and reports notificationOk=false in JSON output. In --no-input mode, watch-arrival requires either complete OpenClaw delivery flags or --dry-run; silent unattended watches are rejected.

For assistant use, prefer --query when the user gave a stop name and --stop when you already have a known TfL stop ID. Query mode narrows stop search by line and checks candidate stops until it finds live predictions, which avoids most parent-stop/child-stop churn.

If the JSON status is delayed and nextCheckAt is present, the caller must schedule another visible check. LondonJourneyCLI performs the one-shot check; it does not silently reschedule itself.

LondonJourneyCLI does not implement its own scheduler. Use OpenClaw cron, launchd, systemd, or another scheduler to run one-shot checks. The important contract is that the check itself has a visible terminal state.

## Skill Discovery

Default discovery is deliberately portable:

- ./skills from the current working directory
- the current directory when it is named skills
- entries in LONDONJOURNEYCLI_SKILLS_DIR
- ~/.config/londonjourneycli/skills
- ~/.local/share/londonjourneycli/skills

Use --skills-dir for deterministic one-off runs. The CLI does not bake in private workspace paths. JSON list/show output includes the local skill path so agents can read files; do not paste that output into public logs if your paths are sensitive.

## Development

    make fmt
    make test
    make live-test

Live TfL tests are opt-in:

    LONDONJOURNEYCLI_LIVE_TFL=1 go test ./internal/tfl -run Live -count=1 -v

No TfL API key is committed. Set TFL_APP_KEY if you have one.

## Repository Layout

- cmd/londonjourneycli: binary entry point
- internal/cli: command parsing and command orchestration
- internal/skill: SKILL.md discovery, skill.yaml parsing, lint, doctor
- internal/tfl: TfL HTTP client and filtering
- internal/notify: OpenClaw delivery adapter
- internal/output: JSON and plain output helpers
- docs: agent, spec, skill, TfL, and troubleshooting docs
- examples: example LondonJourneyCLI-compatible skills

## Status

First public cut. The scope is intentionally small: a solid registry/runner plus a real TfL module. More providers should earn their way in through manifests and tests, not by expanding the core until it becomes mush.
