# Sharing Notes

LondonJourneyCLI is easiest to explain as:

> Agent-ready London transport answers from a small Go CLI: journeys, live arrivals, fares, accessibility, and location-aware stop lookup.

Short post:

> Built LondonJourneyCLI: a small Go CLI that turns TfL's endpoint-shaped API into task-shaped tools for agents: journey planning, live arrivals, line status, disruptions, fares, nearby stop search, accessible station discovery, and one-shot transport checks.
>
> github.com/cavit99/londonjourneycli

Useful demo commands:

    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli --json tfl fare --from-zone 3 --to-zone 1 --period peak --payment contactless
    londonjourneycli --json tfl accessible-stations --near "Oxford Circus" --mode tube --radius 1200 --require-step-free
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl arrivals --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl nearby-stops --location "📍 51.505000, -0.087000" --mode bus --limit 3
    londonjourneycli --json --envelope --output 0.lineStatuses.0.statusSeverityDescription tfl status --line victoria

What makes it different from broader TfL wrappers:

- Task-shaped commands, not just endpoint exposure.
- Stop-name plus line resolution for arrivals.
- Location-share nearby-stop lookup.
- Route-sensitive and zonal fare answers.
- Accessible station discovery with useful lift/access fields.
- Stable JSON, optional envelope, and field projection for agents.
- Shell-first: works from OpenClaw, cron, local scripts, and servers without hosting an MCP process.
