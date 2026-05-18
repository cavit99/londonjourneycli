# LondonJourneyCLI Plan

LondonJourneyCLI turns prose-heavy agent skills into a small, testable operating layer.

## Goal

Build a Go CLI that keeps SKILL.md as the agent-facing playbook, while adding a deterministic runtime for discovery, validation, execution, tests, and safe scheduling.

The LLM still decides which skill applies and supplies judgement. LondonJourneyCLI owns the invariants:
- stable machine-readable output
- no interactive surprises
- explicit tool/env requirements
- reproducible checks
- safe handling for time-critical alerts
- MECE skill ownership checks
- testable skill commands

## Name

londonjourneycli.

Why: short, memorable, and accurate. Skills are still human-shaped artifacts, but the CLI forges them into something executable and reliable.

## First Ship Scope

1. Core CLI
   - list, search, show
   - lint, doctor
   - run, test
   - global --json, --plain, --no-input, --skills-dir, --timeout

2. Skill manifest
   - optional skill.yaml beside SKILL.md
   - owner domain, triggers, safety level, cron safety, requirements
   - commands and tests with argv arrays, never shell strings

3. TfL module
   - tfl stop-search
   - tfl arrivals
   - tfl journey
   - tfl watch-arrival

4. Safety contract
   - stable exit codes
   - stdout is data, stderr is diagnostics
   - time-critical watch commands message on due, delayed, no-data, and API-failure when OpenClaw delivery is configured
   - no hidden hard-coded secrets

5. Tests
   - unit tests for manifest parsing, linting, TfL filtering, and CLI output
   - CLI smoke tests
   - live TfL smoke test gated by LONDONJOURNEYCLI_LIVE_TFL=1

6. Documentation
   - README quickstart
   - docs/spec.md
   - docs/agents.md
   - docs/tfl.md
   - docs/skills.md

7. Adoption
   - adapt existing TfL skills to call londonjourneycli tfl
   - retire direct scripts as legacy fallbacks only

## Non-goals For First Ship

- Full remote skill package manager.
- Replacing SKILL.md.
- A daemon.
- A generalized cron implementation. LondonJourneyCLI validates and performs deterministic one-shot checks; OpenClaw cron remains the scheduler.
