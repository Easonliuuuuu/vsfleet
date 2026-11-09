// Command releasenotes puts a release's "What's new" notes at the top of its
// GitHub release body. The release workflow pipes the body in and writes the
// result back:
//
//	gh release view v0.7.0 --json body --jq .body |
//		go run ./internal/whatsnew/releasenotes 0.7.0 > notes.md
//
// It exits 3, writing nothing, when the release has no notes file, so the
// workflow can warn instead of failing; CI uses the same exit code to warn on
// a release pull request that has none yet.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/whatsnew"
)

const exitNoNotes = 3

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "usage: releasenotes VERSION < body > body-with-notes")
		return 2
	}
	version := strings.TrimPrefix(args[0], "v")
	r, ok := whatsnew.Get(version)
	if !ok {
		fmt.Fprintf(errOut, "no notes for %s: add internal/whatsnew/notes/%s.toml\n", version, version)
		return exitNoNotes
	}
	if err := r.Check(); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	body, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	result, err := whatsnew.WithBlock(string(body), r)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	_, err = io.WriteString(out, result)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
