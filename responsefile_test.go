// Copyright (c) 2023, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package responsefile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"mvdan.cc/responsefile"
)

func TestShorten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		shortenOptions responsefile.ShortenOptions
		args           []string

		wantResponseFile bool
	}{
		{
			shortenOptions:   responsefile.ShortenOptions{},
			args:             []string{},
			wantResponseFile: false,
		},
		{
			shortenOptions:   responsefile.ShortenOptions{},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: false,
		},
		{
			shortenOptions: responsefile.ShortenOptions{
				ArgLengthLimit: 20,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: false,
		},
		{
			shortenOptions: responsefile.ShortenOptions{
				ArgLengthLimit: 2,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: true,
		},
		{
			shortenOptions: responsefile.ShortenOptions{
				ArgLengthLimit: -1,
			},
			args:             []string{},
			wantResponseFile: false,
		},
		{
			shortenOptions: responsefile.ShortenOptions{
				ArgLengthLimit: -1,
			},
			args:             []string{""},
			wantResponseFile: false,
		},
		{
			shortenOptions: responsefile.ShortenOptions{
				ArgLengthLimit: -1,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: true,
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			shortened, cleanup, err := responsefile.Shorten(test.args, test.shortenOptions)
			if err != nil {
				if cleanup != nil {
					t.Fatal("cleanup func must be nil on error")
				}
				t.Fatal(err)
			}
			if cleanup == nil {
				t.Fatal("cleanup func must not be nil without an error")
			}
			t.Cleanup(cleanup)
			if !test.wantResponseFile {
				if !slices.Equal(shortened, test.args) {
					t.Fatalf("did not expect a response file, got %#v", shortened)
				}
			} else {
				if slices.Equal(shortened, test.args) {
					t.Fatalf("expected a response file, got %#v", shortened)
				}
			}

			// Ensure that we can roundtrip back to the original args too.
			expanded, err := responsefile.Expand(shortened, responsefile.ExpandOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(expanded, test.args) {
				t.Fatalf("roundtrip got %#v, expected %#v", expanded, test.args)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	tdir := t.TempDir()
	atTemp := func(content string) (path string) {
		t.Helper()
		f, err := os.CreateTemp(tdir, "")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return "@" + f.Name()
	}

	tests := []struct {
		args     []string
		wantArgs []string
	}{
		{
			args:     []string{},
			wantArgs: []string{},
		},
		{
			args:     []string{"foo", "bar", "baz"},
			wantArgs: []string{"foo", "bar", "baz"},
		},
		{
			args:     []string{"foo", atTemp("bar1\nbar2\n"), "baz"},
			wantArgs: []string{"foo", "bar1", "bar2", "baz"},
		},
		{
			args:     []string{atTemp("crlf\r\n"), atTemp(""), atTemp("nolf")},
			wantArgs: []string{"crlf", "nolf"},
		},
		{
			args: []string{atTemp("escaped\\\\\\\nchars")},
			// TODO: newlines can be escaped, so this should be:
			// wantArgs: []string{"escaped\\\nchars"},
			wantArgs: []string{"escaped\\", "chars"},
		},
		{
			args: []string{atTemp("l1_1\n" +
				atTemp("l2_1\nl2_2\n") + "\n" +
				atTemp(atTemp("l3")) + "\n" +
				atTemp("l2_3\nl2_4\n") + "\n" +
				"l1_2\n"),
			},
			wantArgs: []string{"l1_1", "l2_1", "l2_2", "l3", "l2_3", "l2_4", "l1_2"},
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			expanded, err := responsefile.Expand(test.args, responsefile.ExpandOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(expanded, test.wantArgs) {
				t.Fatalf("expand got %#v, expected %#v", expanded, test.wantArgs)
			}
		})
	}
}

func TestExpandCycle(t *testing.T) {
	t.Parallel()

	// A response file which includes itself crashes with a stack overflow,
	// which cannot be recovered, so expand it in a subprocess.
	if path := os.Getenv("RESPONSEFILE_TEST_CYCLE"); path != "" {
		debug.SetMaxStack(1 << 20)
		responsefile.Expand([]string{"@" + path}, responsefile.ExpandOptions{})
		return
	}
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "cycle"))
	if err := os.WriteFile(path, []byte("foo\n@"+path+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestExpandCycle$")
	cmd.Env = append(os.Environ(), "RESPONSEFILE_TEST_CYCLE="+path)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "stack overflow") {
		t.Fatalf("expected a stack overflow, got %v:\n%s", err, out)
	}
}
