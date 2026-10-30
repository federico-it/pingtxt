package hub

import "testing"

func TestExportTargetHidesDSNPasswords(t *testing.T) {
	for in, want := range map[string]string{
		"postgres://app:s3cret@db:5432/app": "postgres://app:" + redactedPassword + "@db:5432/app",
		"app:s3cret@tcp(db:3306)/app":       "app:" + redactedPassword + "@tcp(db:3306)/app",
		"https://example.com/health?x=1":    "https://example.com/health?x=1",
	} {
		if got := exportTarget(in); got != want {
			t.Errorf("exportTarget(%q) = %q, want %q", in, got, want)
		}
	}
}
