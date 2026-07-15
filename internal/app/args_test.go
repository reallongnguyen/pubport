package app

import (
	"flag"
	"testing"
)

func TestParseArgsFlagsAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	host := fs.String("hostname", "", "")
	noWait := fs.Bool("no-wait", false, "")
	// The natural CLI order: positional first, flags after.
	if err := parseArgs(fs, []string{"3100", "-hostname", "gate.example.com", "-no-wait"}, "no-wait"); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if *host != "gate.example.com" {
		t.Errorf("hostname = %q, want gate.example.com", *host)
	}
	if !*noWait {
		t.Error("no-wait not set")
	}
	if fs.NArg() != 1 || fs.Arg(0) != "3100" {
		t.Errorf("operands = %v, want [3100]", fs.Args())
	}
}

func TestParseArgsEqualsForm(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	host := fs.String("hostname", "", "")
	if err := parseArgs(fs, []string{"-hostname=x.example.com", "9000"}, "no-wait"); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if *host != "x.example.com" {
		t.Errorf("hostname = %q", *host)
	}
	if fs.Arg(0) != "9000" {
		t.Errorf("operand = %q, want 9000", fs.Arg(0))
	}
}

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"app.example.com": "app",
		"My_App.dev.io":   "my-app",
		"":                "pubport",
		"--weird--":       "weird",
	}
	for in, want := range cases {
		if got := deriveName(in); got != want {
			t.Errorf("deriveName(%q) = %q, want %q", in, got, want)
		}
	}
}
