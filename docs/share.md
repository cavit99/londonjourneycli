# Sharing Notes

LondonJourneyCLI is easiest to explain as:

> Agent-safe TfL journey planning and live London transport alerts from a small Go CLI.

Short post:

> Built LondonJourneyCLI: a small Go CLI for TfL journey planning, live arrivals, line status, disruptions, and one-shot transport alerts. Designed for AI assistants/OpenClaw agents, cron jobs, and scripts that need structured JSON and visible failure states.
>
> github.com/cavit99/londonjourneycli

Useful demo commands:

    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl arrivals --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl nearby-stops --location "📍 51.505000, -0.087000" --mode bus --limit 3
    londonjourneycli --json --envelope --output 0.lineStatuses.0.statusSeverityDescription tfl status --line victoria

What makes it different from broader TfL wrappers:

- Shell-first and cron-friendly.
- Stable JSON, optional envelope, and field projection.
- Stop-name arrivals and location-share nearby-stop lookup.
- Explicit no-silent-failure contract for time-critical alerts.
- OpenClaw skill manifests and docs included.
