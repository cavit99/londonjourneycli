# Agents & Automation

LondonJourneyCLI is designed to be driven by agents, cron jobs, and scripts.

## Contract

- Pass --json when parsing output.
- Pass --no-input in unattended contexts.
- Pass --skills-dir or set LONDONJOURNEYCLI_SKILLS_DIR; do not rely on private machine paths.
- Branch on exit code, not stderr text.
- Run londonjourneycli doctor <skill> before depending on a skill in a new environment.
- Run londonjourneycli lint after editing skill manifests.
- Treat JSON path fields as local machine context. They may contain absolute paths and should not be pasted into public channels.

## Time-Critical Alerts

For transport, delivery, pickup, and deadline warnings, a silent run is a failure. Use commands that expose all terminal states as data, and configure visible delivery where required.

For TfL:

    londonjourneycli --json --no-input tfl watch-arrival --stop "$STOP" --line "$LINE" --threshold 2m --openclaw-channel whatsapp --openclaw-target "$TARGET"

If the vehicle is delayed, missing, or TfL fails, LondonJourneyCLI returns that state and sends the degraded truth when delivery is configured.

If OpenClaw delivery fails, LondonJourneyCLI returns non-zero and sets notificationOk=false in JSON. Treat that as a live alert failure, not a harmless warning.

If status is delayed and nextCheckAt is present, schedule and verify the next visible check before stopping. LondonJourneyCLI is intentionally one-shot; it will not create cron jobs for you.

In --no-input mode, watch-arrival rejects missing or partial delivery configuration unless --dry-run is set. That is deliberate: unattended transport watches must not succeed silently.
