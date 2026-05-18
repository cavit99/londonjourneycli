# Architecture

LondonJourneyCLI is a small Go CLI with a narrow split of responsibilities.

## Principle

The LLM owns judgement. Go owns invariants.

The agent decides whether a skill applies, interprets user context, and writes human-quality responses. LondonJourneyCLI validates skills, runs declared commands, and exposes deterministic outputs.

## Packages

- cmd/londonjourneycli: process entry point.
- internal/cli: global flag parsing, command dispatch, and human output.
- internal/skill: skill discovery, frontmatter parsing, manifest parsing, linting, doctor checks, and manifest command lookup.
- internal/tfl: TfL API client, known stop aliases, arrival filtering, journey planning.
- internal/notify: delivery adapters. Today this is OpenClaw message send.
- internal/output: stable JSON and plain row helpers.
- internal/exitcode: stable exit-code constants.

## Data Flow

Registry commands:

1. Discover skill roots.
2. Find directories containing SKILL.md.
3. Parse SKILL.md frontmatter.
4. Parse optional skill.yaml.
5. Return, lint, doctor, run, or test the skill.

TfL commands:

1. Build a TfL client from TFL_APP_KEY and optional TFL_BASE_URL.
2. Resolve known aliases for common stations.
3. Query TfL with a bounded HTTP timeout.
4. Sort/filter where appropriate.
5. Emit human, plain, or JSON output.

Time-critical watch commands:

1. Query live state.
2. Classify the state: due, delayed, no-data, or API failure.
3. If delivery flags are set, send a visible message for every state.
4. Emit the same state to stdout.

## Why no daemon

OpenClaw already has cron. launchd and systemd already exist. LondonJourneyCLI should be a sharp one-shot tool with reliable contracts, not another scheduler.

## Why argv arrays

Manifest commands use argv arrays rather than shell strings. This is less pretty in YAML, but safer and easier for agents to reason about.
