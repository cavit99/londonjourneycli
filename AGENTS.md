# AGENTS.md - LondonJourneyCLI

Use LondonJourneyCLI as the deterministic transport runtime. Agents own fuzzy intent resolution; this CLI owns TfL calls, task-shaped output, and live transport states.

## Core Pattern

1. Resolve human shorthand before calling `tfl journey`.
   - "home" / "office" come from the agent's private user context.
   - Venues like "Shoreditch House" should become an exact address, postcode, station ID, coordinates, or known venue alias before the journey call.
   - Area names like "Highgate" are ambiguous; choose a specific target only when context makes it obvious, otherwise ask a short clarification.
2. Call with `--json` when another program or agent will parse the result.
3. Present the top route in human terms: leave time, arrival time, total duration, key legs, and a buffer if the user has a deadline.
4. For "next bus/train" requests, prefer:

       londonjourneycli --json tfl next-arrival --query "<stop name>" --line <line>

5. For several predictions from a named stop, use:

       londonjourneycli --json tfl arrivals --query "<stop name>" --line <line>

6. For current-location/nearby-stop requests, pass the shared location or coordinates:

       londonjourneycli --json tfl nearby-stops --location "📍 51.433533, -0.095258" --mode bus --limit 5

7. For fare/cost requests, use:

       londonjourneycli --json tfl fare --from-zone 3 --to-zone 1 --period peak --payment contactless
       londonjourneycli --json tfl fare --from "<station>" --to "<station>" --date YYYYMMDD --time HHmm

8. `--query` arrival commands default to bus stop search. For rail/tube/DLR/Elizabeth line/Overground/tram stops, pass the matching `--mode` or `--mode all`.
9. For time-critical alerts, use `watch-arrival` only with visible delivery or `--dry-run` in tests. Silent unattended transport alerts are a bug.

## Ambiguous TfL Results

TfL can return HTTP 300 disambiguation when a place is too vague. In JSON mode LondonJourneyCLI returns:

- `status: "ambiguous"`
- `error.statusCode: 300`
- `error.disambiguation.*.disambiguationOptions[]`

Agent rule: do not paste the raw ambiguity list to the user unless needed. Pick the obvious London/venue/postcode candidate when safe, retry with its `parameterValue` or exact address, and mention the assumption briefly. If the candidates are genuinely unclear, ask one concise clarification.

## Status Handling

- `tfl journey`: success returns `journeys[]`; `ambiguous` means resolve place and retry.
- `tfl status` / `tfl disruptions`: use before or after journey planning when the user cares about reliability, delays, or whether a route is likely to work.
- `tfl line-routes`: use to inspect line termini/directions before applying direction filters.
- `tfl fare`: use for cost questions. Prefer exact station-pair lookup with date/time for route-sensitive fares; use zone lookup for simple adult PAYG zone questions.
- `tfl nearby-stops`: use when you have coordinates and need concrete stop IDs around a resolved address/current location. Prefer `--location` for OpenClaw/WhatsApp pin text, `geo:` URIs, or map links with embedded coordinates; use `--lat`/`--lon` for separate LocationLat/LocationLon fields.
- `tfl arrivals`: use `--query` for a named stop/station and `--stop` for a known stop ID. `--query` returns `status`, `resolvedStop`, `next`, and `arrivals`; `--stop` preserves the raw arrivals array for compatibility.
- `tfl next-arrival`: `ok` has `next`; `no_data` means the stop resolved but no matching prediction; `stop_not_found` means the stop query itself failed.
- `tfl watch-arrival`: `due`, `delayed`, `no_data`, `stop_not_found`, and `api_failed` are all user-visible terminal states when delivery is configured.

## Verification

Before shipping changes:

    go test ./... -coverprofile=coverage.out
    go tool cover -func=coverage.out | tail -1
    go vet ./...
    LONDONJOURNEYCLI_LIVE_TFL=1 go test ./internal/tfl -run Live -count=1 -v

Run a review pass after tests and fix any blocking findings before merging.
