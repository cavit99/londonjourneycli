---
name: tfl-journey
description: Plan London journeys and check live TfL arrivals through LondonJourneyCLI.
---

# TfL Journey

Use LondonJourneyCLI for deterministic London journey planning and live transport checks.

Resolve fuzzy places before journey planning: turn "home", "office", venue names, and vague areas into exact addresses, postcodes, coordinates, or TfL IDs. If "tfl journey --json" returns status "ambiguous", inspect error.disambiguation, retry with the obvious candidate, or ask one clarification.

Examples:

    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli tfl journey --from "London Bridge" --to "Paddington" --preference LeastWalking --max-walking-minutes 15
    londonjourneycli tfl status --line victoria
    londonjourneycli --json tfl disruptions --mode tube,dlr,elizabeth-line,overground,tram
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl arrivals --stop 490000235N --line 43
    londonjourneycli --json tfl stop-search "London Bridge" --mode tube,bus
    londonjourneycli --json tfl stop-info --stop 490G00008459

For time-critical heads-ups, use londonjourneycli tfl watch-arrival from OpenClaw cron with explicit visible delivery. Prefer --query for user-named stops and --stop only when you already have the TfL stop ID.
