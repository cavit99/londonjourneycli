# Skills

A LondonJourneyCLI-compatible skill is a directory with SKILL.md and optional skill.yaml.

SKILL.md is prose for the model. skill.yaml is the executable contract for tools.

## Manifest Fields

- name
- description
- ownerDomain
- triggers
- safetyLevel
- cronSafe
- requires.bins
- requires.env
- commands
- tests
- verification

Commands and tests are argv arrays. They are not shell strings. This avoids quoting bugs and shell injection.

Command timeout values use Go duration strings, such as 5s or 2m. In --no-input mode, LondonJourneyCLI exports LONDONJOURNEYCLI_NO_INPUT=1 to manifest commands.

## MECE Checks

londonjourneycli lint checks duplicate names, missing descriptions, manifest mismatches, invalid commands, and exact duplicate triggers.

It deliberately warns on trigger overlap rather than treating it as fatal. Human judgement still owns semantic boundaries.

## Discovery

Use --skills-dir for deterministic runs. For persistent local setup, put skills under ~/.config/londonjourneycli/skills or ~/.local/share/londonjourneycli/skills, or set LONDONJOURNEYCLI_SKILLS_DIR to one or more roots.
