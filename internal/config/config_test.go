package config

import "testing"

func TestParseCIDRs(t *testing.T) {
	got, err := ParseCIDRs(" 160.79.104.0/21 , 153.61.208.0/20,,2607:6bc0::/48 ")
	if err != nil || len(got) != 3 || got[0].String() != "160.79.104.0/21" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := ParseCIDRs(""); err != nil || got != nil {
		t.Fatalf("empty must mean no restriction: %v %v", got, err)
	}
	for _, bad := range []string{"nope", "1.2.3.4", "1.2.3.4/33", "10.0.0.0/8,x"} {
		if _, err := ParseCIDRs(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMCPIsOffAndUnrestrictedByDefault(t *testing.T) {
	t.Setenv("BLAIR_MCP_ENABLED", "")
	t.Setenv("BLAIR_MCP_ALLOWED_CIDRS", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MCPEnabled || len(c.MCPCIDRs) != 0 {
		t.Fatalf("defaults: enabled=%v cidrs=%v", c.MCPEnabled, c.MCPCIDRs)
	}
}

func TestMCPNeedsHTTPS(t *testing.T) {
	t.Setenv("BLAIR_MCP_ENABLED", "true")
	t.Setenv("BLAIR_BASE_URL", "http://blair.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("MCP over plain http must be refused")
	}
	t.Setenv("BLAIR_BASE_URL", "https://blair.example.com")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
