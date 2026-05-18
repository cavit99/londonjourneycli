package skill

import "testing"

func TestDoctorChecksMissingBin(t *testing.T) {
	s := Skill{Name: "demo", SkillMD: "missing", Manifest: &Manifest{Requires: Requires{Bins: []string{"definitely-not-a-real-binary-londonjourneycli-test"}}}}
	checks := Doctor(s)
	var found bool
	for _, c := range checks {
		if c.Name == "bin:definitely-not-a-real-binary-londonjourneycli-test" {
			found = true
			if c.OK {
				t.Fatal("expected missing bin check to fail")
			}
		}
	}
	if !found {
		t.Fatalf("missing bin check not found: %+v", checks)
	}
}

func TestManifestCommand(t *testing.T) {
	m := Manifest{Commands: []Command{{Name: "journey", Exec: []string{"londonjourneycli", "tfl", "journey"}}}}
	if _, ok := m.Command("journey"); !ok {
		t.Fatal("expected command")
	}
	if _, ok := m.Command("missing"); ok {
		t.Fatal("unexpected command")
	}
}
