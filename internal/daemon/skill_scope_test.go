package daemon

import "testing"

func TestValidateSkillSetScope(t *testing.T) {
	for _, scope := range []string{"", "global", "GLOBAL"} {
		if err := validateSkillSetScope(scope); err != nil {
			t.Fatalf("validateSkillSetScope(%q) error = %v, want nil", scope, err)
		}
	}
	for _, scope := range []string{"project", "PROJECT", "other"} {
		if err := validateSkillSetScope(scope); err == nil {
			t.Fatalf("validateSkillSetScope(%q) error = nil, want rejection", scope)
		}
	}
}
