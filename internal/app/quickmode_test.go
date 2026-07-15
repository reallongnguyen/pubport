package app

import "testing"

func TestResolveHostname(t *testing.T) {
	cases := []struct {
		name    string
		port    int
		arg     string
		def     string
		want    string
		wantErr bool
	}{
		{"no arg uses p<port>", 8000, "", "example.com", "p8000.example.com", false},
		{"bare label + default", 8000, "api", "example.com", "api.example.com", false},
		{"dotted arg is full fqdn", 8000, "api.domain.com", "example.com", "api.domain.com", false},
		{"label is lowercased", 8000, "API", "example.com", "api.example.com", false},
		{"dotted fqdn needs no default", 8000, "api.domain.com", "", "api.domain.com", false},
		{"whitespace trimmed", 3000, "  api  ", "example.com", "api.example.com", false},
		{"no arg without default errors", 8000, "", "", "", true},
		{"bare label without default errors", 8000, "api", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveHostname(c.port, c.arg, c.def)
			if c.wantErr {
				if err == nil {
					t.Fatalf("resolveHostname(%d,%q,%q) = %q, want error", c.port, c.arg, c.def, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveHostname(%d,%q,%q): unexpected error %v", c.port, c.arg, c.def, err)
			}
			if got != c.want {
				t.Errorf("resolveHostname(%d,%q,%q) = %q, want %q", c.port, c.arg, c.def, got, c.want)
			}
		})
	}
}

func TestSeedDomainFromHost(t *testing.T) {
	cases := map[string]string{
		"api.example.com":   "example.com",
		"p8000.example.com": "example.com",
		"a.b.c.d":           "b.c.d",
		"example.com":       "", // stripping would leave a bare TLD — refuse
		"nodots":            "",
	}
	for in, want := range cases {
		if got := seedDomainFromHost(in); got != want {
			t.Errorf("seedDomainFromHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShouldOverwriteDNS(t *testing.T) {
	cases := []struct {
		name     string
		force    bool
		prevHost string
		newHost  string
		want     bool
	}{
		{"force overrides everything", true, "", "x.example.com", true},
		{"force over foreign host", true, "other.example.com", "x.example.com", true},
		{"new tunnel, no prior host", false, "", "x.example.com", false},
		{"idempotent re-up of our host", false, "x.example.com", "x.example.com", true},
		{"changed host is not ours", false, "old.example.com", "new.example.com", false},
	}
	for _, c := range cases {
		if got := shouldOverwriteDNS(c.force, c.prevHost, c.newHost); got != c.want {
			t.Errorf("%s: shouldOverwriteDNS(%v,%q,%q) = %v, want %v", c.name, c.force, c.prevHost, c.newHost, got, c.want)
		}
	}
}
