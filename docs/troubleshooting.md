# Troubleshooting

## londonjourneycli list shows no skills

Use --skills-dir explicitly:

    londonjourneycli --skills-dir ~/.config/londonjourneycli/skills list

For a persistent setup, set LONDONJOURNEYCLI_SKILLS_DIR to the directory or path-list you want LondonJourneyCLI to search.

A skill directory must contain SKILL.md.

## TfL returns HTTP errors

Retry once. If failures persist, set TFL_APP_KEY or check https://api.tfl.gov.uk availability.

## OpenClaw notification fails

Verify the OpenClaw CLI can send directly:

    openclaw message send --channel whatsapp --target +15555550123 --message "test"

LondonJourneyCLI does not hide delivery failures; a failed send exits non-zero.
