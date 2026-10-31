package hub

import (
	"testing"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestAgentLabel(t *testing.T) {
	cases := []struct{ name, loc, want string }{
		{"vps-1", "", "vps-1"},
		{"vps-1", "Frankfurt", "vps-1 · Frankfurt"},
		{"Frankfurt", "Frankfurt", "Frankfurt"},
		{"", "Frankfurt", "Frankfurt"},
	}
	for _, c := range cases {
		if got := agentLabel(&store.Agent{Name: c.name, Location: c.loc}); got != c.want {
			t.Errorf("agentLabel(%q, %q) = %q, want %q", c.name, c.loc, got, c.want)
		}
	}
}
