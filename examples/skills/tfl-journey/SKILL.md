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
    londonjourneycli --json tfl compare --from "London Bridge" --to "Paddington" --rank balanced --include-alternatives
    londonjourneycli tfl status --line victoria
    londonjourneycli --json tfl disruptions --mode tube,dlr,elizabeth-line,overground,tram
    londonjourneycli --json tfl line-routes --line victoria
    londonjourneycli --json tfl fare --from-zone 3 --to-zone 1 --period peak --payment contactless
    londonjourneycli --json tfl fare --from "Tottenham Hale Underground Station" --to "Oxford Circus Underground Station" --date 20260519 --time 0800 --mode tube
    londonjourneycli --json tfl nearby-stops --lat 51.505 --lon -0.087 --mode bus --limit 5
    londonjourneycli --json tfl accessible-stations --near "Oxford Circus" --mode tube --radius 1200 --require-step-free
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3
    londonjourneycli --json tfl arrivals --stop 490000235N --line 43
    londonjourneycli --json tfl stop-search "London Bridge" --mode tube,bus
    londonjourneycli --json tfl stop-info --stop 490G00008459

For time-critical heads-ups, use londonjourneycli tfl watch-arrival from OpenClaw cron with explicit visible delivery. Prefer --query for user-named stops and --stop only when you already have the TfL stop ID.

Use tfl fare for fare/cost questions. Prefer exact stations with date/time for route-sensitive answers; use zone lookup for simple adult PAYG zone questions.

Use tfl compare when the user asks which journey option is best. The JSON output ranks options and includes score, reasons, duration, walkingMinutes, interchangeCount, modes, lines, fare when present, and legs.

For accessible journey questions, prefer tfl trip or tfl journey with --accessibility and --between-entrances. For nearby station-discovery questions, use accessible-stations rather than hand-checking stop metadata. It returns station candidates sorted by distance with accessStatus, stepFreeAccess, liftPresent, lifts, and accessViaLift fields. Use --require-step-free for confirmed AccessViaLift=true; use --require-lift for broader lift-present candidates.
