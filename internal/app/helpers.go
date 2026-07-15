package app

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/reallongnguyen/pubport/internal/profile"
)

func listProfiles() ([]string, error) { return profile.List() }

// parseArgs parses a flag set while tolerating flags that appear after
// positional operands (e.g. "up 3100 -hostname x"), which the stdlib flag
// package otherwise stops at. boolFlags names the flags that take no value.
func parseArgs(fs *flag.FlagSet, args []string, boolFlags ...string) error {
	isBool := map[string]bool{}
	for _, b := range boolFlags {
		isBool[b] = true
	}
	var opts, ops []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			ops = append(ops, args[i+1:]...)
			break
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			opts = append(opts, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(a, "=") || isBool[name] {
				continue
			}
			if i+1 < len(args) {
				opts = append(opts, args[i+1])
				i++
			}
			continue
		}
		ops = append(ops, a)
	}
	return fs.Parse(append(opts, ops...))
}

// tailFile prints the tail of a file and, when follow is set, streams new
// lines until interrupted.
func tailFile(path string, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log yet at %s (has the tunnel been started?)", path)
		}
		return err
	}
	defer f.Close()

	// Print the last ~200 lines.
	lines := lastLines(f, 200)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	if !follow {
		return nil
	}
	// Seek to end and poll for appended data.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Fprint(stdout, line)
		}
		if err == io.EOF {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}

// lastLines returns up to n trailing lines from r.
func lastLines(r io.Reader, n int) []string {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ring := make([]string, 0, n)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring
}
