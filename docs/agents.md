# Agents & Automation

LondonJourneyCLI is designed to be driven by agents, cron jobs, and scripts.

## Contract

- Pass --json when parsing output.
- Use --output <path> with --json when you only need one stable field.
- Use --envelope with --json when the caller wants command metadata and an ok/data wrapper; ok mirrors exit-code success.
- Pass --no-input in unattended contexts.
- Pass --skills-dir or set LONDONJOURNEYCLI_SKILLS_DIR; do not rely on private machine paths.
- Branch on exit code, not stderr text.
- Resolve fuzzy human places before journey planning. The CLI should receive a postcode, exact address, station/stop ID, coordinates, or a TfL-known alias when possible.
- Run londonjourneycli doctor <skill> before depending on a skill in a new environment.
- Run londonjourneycli lint after editing skill manifests.
- Treat JSON path fields as local machine context. They may contain absolute paths and should not be pasted into public channels.

## Place Resolution

Agents should resolve "home", "office", venue names, and vague areas before calling "tfl trip" or "tfl journey". Use private user context for personal aliases and an address/postcode/source lookup for venues. If TfL still returns "status: ambiguous" in JSON mode, inspect "error.disambiguation" and retry with the obvious London candidate's parameterValue or exact address. Ask one clarification only when the candidates are genuinely unclear.

JSON mode structures TfL API errors and request failures. Journey planning can return status "ambiguous" with TfL disambiguation data; non-watch TfL request failures return status "api_error" for HTTP failures or "api_failed" for lower-level network/timeout failures. With --envelope, ok is false for all of these.

Use tfl trip for user-facing route advice when one answer should include route summary, Journey Planner fare data where available, active disruptions on the selected lines, and accessibility flags. Use tfl journey when the caller only needs raw Journey Planner options.
Use tfl status and tfl disruptions before giving time-sensitive route advice when delays would change the recommendation and you are not already using tfl trip.
Use tfl line-routes when choosing or explaining line direction, termini, or service sections.
Use tfl arrivals --query when the user gives a stop/station name and the agent needs several upcoming predictions without first running stop-search. Query arrivals default to bus stop search; pass --mode tube, --mode all, etc. for non-bus stops.
Use tfl fare for fare/cost questions. Prefer --from/--to with date/time for exact station-pair answers because TfL fares can vary by route, direction, peak rules, and National Rail acceptance; use --from-zone/--to-zone for simple adult PAYG zone questions.

Use tfl nearby-stops when the user gives a current GPS/location coordinate or when a resolved address yields coordinates and the agent needs concrete stop IDs nearby. Prefer --location for OpenClaw/WhatsApp pin text such as "📍 51.433533, -0.095258", geo: URIs, or map links with embedded coordinates; use --lat/--lon for separate LocationLat/LocationLon fields.

Use trip or journey's native --accessibility preferences for route questions such as "get me there step-free":

    londonjourneycli --json tfl trip --from "Oxford Circus" --to "King's Cross" --mode tube --accessibility step-free-to-platform --between-entrances

Use tfl accessible-stations for station-discovery questions such as "Which tube stations near Oxford Circus have confirmed lift access?":

    londonjourneycli --json tfl accessible-stations --near "Oxford Circus" --mode tube --radius 1200 --require-step-free

The command resolves --near with TfL StopSearch, then checks nearby station stop points through /StopPoint with only Accessibility and Facility categories. JSON distinguishes confirmed step-free access from lift-present-but-unconfirmed stations with accessStatus, stepFreeAccess, liftPresent, lifts, and accessViaLift. Use --require-step-free for confirmed access via lift; use --require-lift only when a broader lift signal is useful. It also accepts --lat/--lon or --location when the agent already has coordinates.

## Time-Critical Alerts

For transport, delivery, pickup, and deadline warnings, a silent run is a failure. Use commands that expose all terminal states as data, and configure visible delivery where required.

For TfL:

    londonjourneycli --json --no-input tfl watch-arrival --query "$STOP_NAME" --line "$LINE" --threshold 2m --openclaw-channel whatsapp --openclaw-target "$TARGET"

If the vehicle is delayed, missing, or TfL fails, LondonJourneyCLI returns that state and sends the degraded truth when delivery is configured.

If OpenClaw delivery fails, LondonJourneyCLI returns non-zero and sets notificationOk=false in JSON. Treat that as a live alert failure, not a harmless warning.

If status is delayed and nextCheckAt is present, schedule and verify the next visible check before stopping. LondonJourneyCLI is intentionally one-shot; it will not create cron jobs for you.

In --no-input mode, watch-arrival rejects missing or partial delivery configuration unless --dry-run is set. That is deliberate: unattended transport watches must not succeed silently.
