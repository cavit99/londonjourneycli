# TfL

LondonJourneyCLI includes a small TfL client for London journey planning and live arrivals.

## Journey Planning

    londonjourneycli tfl journey --from "London Bridge" --to "Paddington"
    londonjourneycli --json tfl journey --from "Westminster" --to "Waterloo"

Known station aliases resolve to NaPTAN IDs for more reliable journey planning.

## Stop Search

    londonjourneycli tfl stop-search "London Bridge"

## Live Arrivals

    londonjourneycli tfl arrivals --stop 490000235N --line 43

## One-shot Watch

    londonjourneycli tfl watch-arrival --stop 490000235N --line 43 --threshold 2m

Add OpenClaw delivery flags to send visible messages from cron:

    londonjourneycli --json --no-input tfl watch-arrival --stop 490000235N --line 43 --threshold 2m --openclaw-channel whatsapp --openclaw-target +15555550123

The watch command sends on due, delayed, no-data, and TfL API failure states when delivery is configured. If delivery itself fails, JSON output includes notificationOk=false and the command exits non-zero.

In --no-input mode, the command rejects silent unattended watches. Provide both OpenClaw flags or use --dry-run for tests.

Branch on JSON status:

- due: message sent, no further check needed.
- delayed: message sent; schedule another visible check at nextCheckAt.
- no_data: message sent; the expected service is no longer visible.
- api_failed: message sent if possible; check manually if the alert matters.

No API key is committed. Set TFL_APP_KEY if needed.
