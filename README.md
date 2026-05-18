# londonjourneycli

A small Go CLI that turns TfL's endpoint-shaped API into task-shaped tools for agents: plan and compare journeys, resolve named stops, read live arrivals, price fares, find nearby stops from shared locations, discover accessible stations, and run one-shot transport checks.

It also includes a narrow skill manifest runner so an agent can discover, test, and call the transport skill without relying on prose alone.

## Why this exists

Human transport requests are not endpoint-shaped. "Next N3 from Ildersly Grove", "what's the fare from Zone 3 to Oxford Circus?", "which stations near here have lifts?", and "tell me when the bus is nearly here" all require API choreography before an agent can give a useful answer.

LondonJourneyCLI packages those workflows into commands that return answer-shaped data. An agent can spend its tokens on judgement and explanation instead of repeatedly rediscovering TfL mode parameters, StopPoint IDs, parent/child stop behavior, route ranking, fare caveats, and accessibility metadata.

It is useful when an agent needs to:

- compare and rank TfL journey options without writing scoring glue
- resolve a stop name plus line into the actual stop with live predictions
- turn a WhatsApp/OpenClaw location share into nearby stops
- find station candidates with lift/access fields without fetching heavyweight stop payloads
- answer simple zonal fares and route-sensitive station-pair fares
- branch on stable JSON status and exit codes
- discover, lint, test, doctor, and run the transport skill in one local runtime

Use direct TfL APIs or broad MCP servers when you want to explore the whole API surface. Use LondonJourneyCLI when you want a London transport agent to answer common transport questions with fewer tool calls and less glue code.

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
    londonjourneycli --json tfl nearby-stops --lat 51.505 --lon -0.087 --mode bus --limit 5
    londonjourneycli --json tfl nearby-stops --location "51.505,-0.087" --mode bus --limit 5
    londonjourneycli --json tfl accessible-stations --near "Oxford Circus" --mode tube --radius 1200 --require-step-free
    londonjourneycli tfl status --line victoria
    londonjourneycli --json tfl disruptions --mode tube,dlr,elizabeth-line,overground,tram
    londonjourneycli --json tfl line-routes --line victoria
    londonjourneycli --json --output 0.lineStatuses.0.statusSeverityDescription tfl status --line victoria
    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli --json tfl compare --from "London Bridge" --to "Paddington" --rank balanced --include-alternatives
    londonjourneycli tfl journey --from "Westminster" --to "Waterloo" --preference LeastWalking --max-walking-minutes 15
    londonjourneycli --json tfl fare --from-zone 3 --to-zone 1 --period peak --payment contactless
    londonjourneycli --json tfl fare --from "Tottenham Hale Underground Station" --to "Oxford Circus Underground Station" --date 20260519 --time 0800 --mode tube
    londonjourneycli --json tfl arrivals --stop 490000235N --line 43
    londonjourneycli --json tfl arrivals --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3

For agents:

- Resolve fuzzy places before calling journey: turn "home", "office", venue names, and vague areas into exact addresses, postcodes, coordinates, or TfL IDs.
- Use tfl compare when the user asks which route is best; JSON returns ranked options with score, reasons, duration, walking minutes, interchange count, modes, lines, fare when present, and legs.
- If the user shared a WhatsApp/OpenClaw location, pass the coordinates or location context text to nearby-stops with --location.
- For fare questions, prefer station-pair lookup with date/time when route, peak rules, or National Rail acceptance could matter; use zonal lookup for simple adult PAYG zone questions.
- For accessible route planning, prefer journey's native --accessibility preferences with --between-entrances when station access matters.
- For accessible nearby-station discovery, use accessible-stations; it resolves --near through StopSearch, queries nearby station stop points with only Accessibility and Facility property categories, and returns candidates sorted by distance with accessStatus, stepFreeAccess, liftPresent, lifts, and accessViaLift fields.
- If TfL returns JSON status "ambiguous", use the returned disambiguation options to retry or ask one clarification.

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
      - name: compare
        exec: [londonjourneycli, tfl, compare]
    tests:
      - name: stop-search-smoke
        command: [londonjourneycli, --json, tfl, stop-search, London Bridge, --mode, "tube,bus", --limit, "1"]

Commands and tests are argv arrays, not shell strings. That is deliberate: no quoting lottery and no shell injection by default.

## Output Contract

- stdout is data.
- stderr is diagnostics.
- --json is stable and intended for agents.
- --output <path> projects JSON output by dot path; pass it before the command, for example --json --output 0.name.
- --envelope wraps JSON as {ok,schemaVersion,command,requestedAt,data}; ok mirrors whether the command exits 0.
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

LondonJourneyCLI does not implement its own scheduler. Use OpenClaw cron, launchd, systemd, or another scheduler to run one-shot checks. The check returns the same task-shaped state whether it sends a message or is parsed by another program.

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

For a concise public summary and demo commands, see [docs/share.md](docs/share.md).

## Repository Layout

- cmd/londonjourneycli: binary entry point
- internal/cli: command parsing and command orchestration
- internal/skill: SKILL.md discovery, skill.yaml parsing, lint, doctor
- internal/tfl: TfL HTTP client and filtering
- internal/notify: OpenClaw delivery adapter
- internal/output: JSON and plain output helpers
- docs: agent, spec, skill, TfL, and troubleshooting docs
- examples: example LondonJourneyCLI-compatible skills

## Scope

LondonJourneyCLI stays intentionally narrow: a solid registry/runner plus a real TfL module for London journey planning, live arrivals, nearby stops, fares, accessibility lookups, line health, and transport checks. More providers should earn their way in through manifests and tests, not by expanding the core until it becomes mush.
