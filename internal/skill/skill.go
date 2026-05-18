package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Frontmatter struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Metadata    map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

type Requires struct {
	Bins []string `yaml:"bins,omitempty" json:"bins,omitempty"`
	Env  []string `yaml:"env,omitempty" json:"env,omitempty"`
}

type Command struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Exec        []string `yaml:"exec" json:"exec"`
	Timeout     string   `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Safety      string   `yaml:"safety,omitempty" json:"safety,omitempty"`
	Examples    []string `yaml:"examples,omitempty" json:"examples,omitempty"`
}

type Test struct {
	Name    string   `yaml:"name" json:"name"`
	Command []string `yaml:"command" json:"command"`
}

type Manifest struct {
	Name         string    `yaml:"name" json:"name"`
	Description  string    `yaml:"description,omitempty" json:"description,omitempty"`
	OwnerDomain  string    `yaml:"ownerDomain,omitempty" json:"ownerDomain,omitempty"`
	Triggers     []string  `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	SafetyLevel  string    `yaml:"safetyLevel,omitempty" json:"safetyLevel,omitempty"`
	CronSafe     bool      `yaml:"cronSafe,omitempty" json:"cronSafe"`
	Requires     Requires  `yaml:"requires,omitempty" json:"requires,omitempty"`
	Commands     []Command `yaml:"commands,omitempty" json:"commands,omitempty"`
	Tests        []Test    `yaml:"tests,omitempty" json:"tests,omitempty"`
	Verification []string  `yaml:"verification,omitempty" json:"verification,omitempty"`
}

type Skill struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Path        string      `json:"path"`
	SkillMD     string      `json:"skillMd"`
	Manifest    *Manifest   `json:"manifest,omitempty"`
	Frontmatter Frontmatter `json:"frontmatter"`
}

type Issue struct {
	Severity string `json:"severity"`
	Skill    string `json:"skill,omitempty"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type DoctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func Discover(roots []string) ([]Skill, error) {
	seen := map[string]bool{}
	var skills []Skill
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		walkRoot := abs
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			walkRoot = resolved
		}
		err = filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" {
				if path != walkRoot {
					return filepath.SkipDir
				}
			}
			skillMD := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(skillMD); err != nil {
				return nil
			}
			seenKey := path
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				seenKey = resolved
			}
			if seen[seenKey] {
				return filepath.SkipDir
			}
			seen[seenKey] = true
			s, err := Load(path)
			if err != nil {
				return err
			}
			skills = append(skills, s)
			return filepath.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func Load(dir string) (Skill, error) {
	skillMD := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(skillMD)
	if err != nil {
		return Skill{}, err
	}
	fm, err := ParseFrontmatter(raw)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", skillMD, err)
	}
	name := fm.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	desc := fm.Description
	var manifest *Manifest
	manifestPath := filepath.Join(dir, "skill.yaml")
	if b, err := os.ReadFile(manifestPath); err == nil {
		var m Manifest
		if err := yaml.Unmarshal(b, &m); err != nil {
			return Skill{}, fmt.Errorf("%s: %w", manifestPath, err)
		}
		if m.Name == "" {
			m.Name = name
		}
		manifest = &m
		if desc == "" {
			desc = m.Description
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Skill{}, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return Skill{Name: name, Description: desc, Path: dir, SkillMD: skillMD, Manifest: manifest, Frontmatter: fm}, nil
}

func ParseFrontmatter(raw []byte) (Frontmatter, error) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte("---\n")) && !bytes.HasPrefix(trimmed, []byte("---\r\n")) {
		return Frontmatter{}, nil
	}
	firstLineEnd := bytes.IndexByte(trimmed, '\n')
	if firstLineEnd < 0 {
		return Frontmatter{}, errors.New("unterminated frontmatter")
	}
	rest := trimmed[firstLineEnd+1:]
	for start := 0; start <= len(rest); {
		lineEnd := bytes.IndexByte(rest[start:], '\n')
		end := len(rest)
		next := len(rest) + 1
		if lineEnd >= 0 {
			end = start + lineEnd
			next = end + 1
		}
		if isFrontmatterDelimiterLine(rest[start:end]) {
			var fm Frontmatter
			if err := yaml.Unmarshal(bytes.TrimSpace(rest[:start]), &fm); err != nil {
				return Frontmatter{}, err
			}
			return fm, nil
		}
		start = next
	}
	return Frontmatter{}, errors.New("unterminated frontmatter")
}

func isFrontmatterDelimiterLine(line []byte) bool {
	line = bytes.TrimRight(line, " \t\r")
	return bytes.Equal(line, []byte("---"))
}

func Lint(skills []Skill) []Issue {
	var issues []Issue
	names := map[string]string{}
	triggers := map[string]string{}

	for _, s := range skills {
		if strings.TrimSpace(s.Name) == "" {
			issues = append(issues, Issue{Severity: "error", Skill: s.Name, Path: s.Path, Message: "missing skill name"})
		}
		if strings.TrimSpace(s.Description) == "" {
			issues = append(issues, Issue{Severity: "warning", Skill: s.Name, Path: s.Path, Message: "missing description"})
		}
		key := strings.ToLower(s.Name)
		if prev, ok := names[key]; ok {
			issues = append(issues, Issue{Severity: "error", Skill: s.Name, Path: s.Path, Message: "duplicate skill name; also in " + prev})
		}
		names[key] = s.Path

		if s.Manifest == nil {
			continue
		}
		if s.Manifest.Name != "" && s.Manifest.Name != s.Name {
			issues = append(issues, Issue{Severity: "error", Skill: s.Name, Path: s.Path, Message: "skill.yaml name does not match SKILL.md"})
		}
		for _, cmd := range s.Manifest.Commands {
			if cmd.Name == "" {
				issues = append(issues, Issue{Severity: "error", Skill: s.Name, Path: s.Path, Message: "manifest command is missing name"})
			}
			if len(cmd.Exec) == 0 {
				issues = append(issues, Issue{Severity: "error", Skill: s.Name, Path: s.Path, Message: "manifest command " + cmd.Name + " has no exec argv"})
			}
		}
		for _, trig := range s.Manifest.Triggers {
			n := normalizeTrigger(trig)
			if n == "" {
				continue
			}
			if prev, ok := triggers[n]; ok && prev != s.Name {
				issues = append(issues, Issue{Severity: "warning", Skill: s.Name, Path: s.Path, Message: fmt.Sprintf("trigger %q overlaps with %s", trig, prev)})
			} else {
				triggers[n] = s.Name
			}
		}
	}
	return issues
}

func Doctor(s Skill) []DoctorCheck {
	var checks []DoctorCheck
	if _, err := os.Stat(s.SkillMD); err == nil {
		checks = append(checks, DoctorCheck{Name: "SKILL.md", OK: true})
	} else {
		checks = append(checks, DoctorCheck{Name: "SKILL.md", OK: false, Message: err.Error()})
	}
	if s.Manifest == nil {
		checks = append(checks, DoctorCheck{Name: "skill.yaml", OK: true, Message: "optional manifest not present"})
		return checks
	}
	for _, bin := range s.Manifest.Requires.Bins {
		_, err := exec.LookPath(bin)
		checks = append(checks, DoctorCheck{Name: "bin:" + bin, OK: err == nil, Message: errString(err)})
	}
	for _, env := range s.Manifest.Requires.Env {
		_, ok := os.LookupEnv(env)
		msg := ""
		if !ok {
			msg = "missing environment variable"
		}
		checks = append(checks, DoctorCheck{Name: "env:" + env, OK: ok, Message: msg})
	}
	return checks
}

func Find(skills []Skill, name string) (Skill, bool) {
	for _, s := range skills {
		if s.Name == name || strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Skill{}, false
}

func (m Manifest) Command(name string) (Command, bool) {
	for _, c := range m.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

func normalizeTrigger(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
