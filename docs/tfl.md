# TfL

LondonJourneyCLI includes a small TfL client for London journey planning and live arrivals.

## Line Status and Disruptions

    londonjourneycli tfl status
    londonjourneycli tfl status --line victoria
    londonjourneycli --json tfl disruptions --mode tube,dlr,elizabeth-line,overground,tram

The default mode set is tube, DLR, Elizabeth line, Overground, and tram. Use --line for specific line IDs or --mode for a comma-separated mode set.

## Journey Planning

    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli --json tfl journey --from "Westminster" --to "Waterloo"
    londonjourneycli tfl journey --from "London Bridge" --to "Paddington" --preference LeastWalking --max-walking-minutes 15
    londonjourneycli tfl journey --from "London Bridge" --to "Heathrow T2" --mode tube,elizabeth-line --real-time

Known station aliases resolve to NaPTAN IDs for more reliable journey planning.

Useful journey flags map directly to TfL Journey Planner parameters:

- --via <point>
- --preference LeastTime|LeastInterchange|LeastWalking
- --mode tube,bus,elizabeth-line,national-rail,dlr,tram,walking,cycle
- --accessibility StepFreeToVehicle,StepFreeToPlatform,NoEscalators,NoElevators,NoSolidStairs
- --max-walking-minutes N and --max-transfer-minutes N
- --walking-speed Slow|Average|Fast
- --include-alternatives, --alternative-walking, --alternative-cycle
- --real-time and --between-entrances
- --local-only to disable TfL nationalSearch

## Stop Search

    londonjourneycli tfl stop-search "London Bridge"
    londonjourneycli tfl stop-search "Ildersly Grove" --mode bus --line N3
    londonjourneycli --json tfl stop-info --stop 490G00008459
    londonjourneycli --json tfl nearby-stops --lat 51.505 --lon -0.087 --mode bus --radius 500 --limit 5

Use stop-info when stop-search returns a parent stop group and you need the concrete child stop IDs/letters for arrivals. Arrivals and watch-arrival also try those child stops automatically when a parent stop has no direct line predictions.

Use nearby-stops when an agent has coordinates from a resolved address/current location and needs candidate stop IDs around that point.

## Live Arrivals

    londonjourneycli tfl arrivals --stop 490000235N --line 43
    londonjourneycli tfl arrivals --stop 490000235N --line 43 --direction inbound
    londonjourneycli --json tfl next-arrival --query "Ildersly Grove" --line N3

When --line is provided, arrivals uses TfL's line-specific arrivals endpoint rather than fetching every prediction for the stop and filtering locally.

Use next-arrival for assistant-style requests such as "next N3 from Ildersly Grove". It resolves the stop query with the line filter, checks candidate stops for actual predictions, and returns the selected stop plus the next arrivals in one JSON payload. Query mode defaults to bus stops; pass --mode all or a rail/tube mode for non-bus lines. It checks up to five candidates by default; lower --search-limit for tighter cron checks when the stop name is unambiguous.

## One-shot Watch

    londonjourneycli tfl watch-arrival --stop 490000235N --line 43 --threshold 2m

Add OpenClaw delivery flags to send visible messages from cron:

    londonjourneycli --json --no-input tfl watch-arrival --query "Ildersly Grove" --line N3 --threshold 2m --openclaw-channel whatsapp --openclaw-target +15555550123

The watch command sends on due, delayed, no-data, and TfL API failure states when delivery is configured. If delivery itself fails, JSON output includes notificationOk=false and the command exits non-zero.

In --no-input mode, the command rejects silent unattended watches. Provide both OpenClaw flags or use --dry-run for tests.

Branch on JSON status:

- due: message sent, no further check needed.
- delayed: message sent; schedule another visible check at nextCheckAt.
- stop_not_found: message sent; the stop query did not resolve.
- no_data: message sent; the expected service is no longer visible.
- api_failed: message sent if possible; check manually if the alert matters.

No API key is committed. Set TFL_APP_KEY if needed.
