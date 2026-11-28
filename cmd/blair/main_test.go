package main

import (
	"net/netip"
	"testing"

	"github.com/wbollock/blair/internal/config"
)

func TestServeFlagOverridesEnvironment(t *testing.T) {
	env := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name string
		args []string
		want string
		err  bool
	}{
		{"no flag keeps the environment", nil, "10.0.0.0/8", false},
		{"flag replaces it", []string{"--mcp-allowed-cidrs", "160.79.104.0/21,153.61.208.0/20"}, "160.79.104.0/21,153.61.208.0/20", false},
		{"empty flag removes the limit", []string{"--mcp-allowed-cidrs="}, "", false},
		{"bad value is refused", []string{"--mcp-allowed-cidrs=nope"}, "", true},
		{"unknown flag is refused", []string{"--nope"}, "", true},
	}
	for _, c := range cases {
		cfg := &config.Config{MCPCIDRs: append([]netip.Prefix(nil), env...)}
		err := applyServeFlags(cfg, c.args)
		if (err != nil) != c.err {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if c.err {
			continue
		}
		got := ""
		for i, p := range cfg.MCPCIDRs {
			if i > 0 {
				got += ","
			}
			got += p.String()
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
