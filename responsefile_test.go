// Copyright (c) 2023, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package responsefile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mvdan.cc/responsefile"
)

func TestShorten(t *testing.T) {
	t.Parallel()
	always := responsefile.ShortenOptions{ArgLengthLimit: -1}
	alwaysLines := responsefile.ShortenOptions{Format: responsefile.FormatLines, ArgLengthLimit: -1}

	tests := []struct {
		shortenOptions responsefile.ShortenOptions
		args           []string

		// wantFile is the content of the response file, or empty if none is used.
		wantFile string
		wantErr  bool
	}{
		{
			args: []string{},
		},
		{
			args: []string{"foo", "bar", "baz"},
		},
		{
			shortenOptions: responsefile.ShortenOptions{ArgLengthLimit: 20},
			args:           []string{"foo", "bar", "baz"},
		},
		{
			shortenOptions: responsefile.ShortenOptions{ArgLengthLimit: 2},
			args:           []string{"foo", "bar", "baz"},
			wantFile:       "foo\nbar\nbaz\n",
		},
		{
			shortenOptions: always,
			args:           []string{},
		},
		{
			shortenOptions: always,
			args:           []string{""},
			// A negative limit always uses a response file.
			wantFile: "\"\"\n",
		},
		{
			args: slices.Repeat([]string{"a.o"}, 10<<10),
			// Separators count towards the limit.
			wantFile: strings.Repeat("a.o\n", 10<<10),
		},
		{
			args: slices.Repeat([]string{"-DNAME=a long value"}, 1400),
			// Quoting counts towards the limit.
			wantFile: strings.Repeat("\"-DNAME=a long value\"\n", 1400),
		},
		{
			shortenOptions: always,
			args:           []string{"foo", "bar", "baz"},
			wantFile:       "foo\nbar\nbaz\n",
		},
		{
			shortenOptions: always,
			args:           []string{"back\\slash", "new\nline", ""},
			wantFile:       "\"back\\\\slash\"\n\"new\nline\"\n\"\"\n",
		},
		{
			shortenOptions: always,
			args:           []string{"it's", `say "hi"`, "tab\there", "cr\r"},
			wantFile:       "\"it's\"\n\"say \\\"hi\\\"\"\n\"tab\there\"\n\"cr\r\"\n",
		},
		{
			shortenOptions: always,
			args:           []string{"a\xff\\b", "c\xff\nd"},
			// Invalid UTF-8 is kept as-is.
			wantFile: "\"a\xff\\\\b\"\n\"c\xff\nd\"\n",
		},
		{
			shortenOptions: alwaysLines,
			args:           []string{"back\\slash", "new\nline", ""},
			wantFile:       "back\\\\slash\nnew\\nline\n\n",
		},
		{
			shortenOptions: alwaysLines,
			args:           []string{"a\xff\\b", "c\xff\nd"},
			// Invalid UTF-8 is kept as-is.
			wantFile: "a\xff\\\\b\nc\xff\\nd\n",
		},
		{
			shortenOptions: alwaysLines,
			args:           []string{"cr\r"},
			// Expand would drop the carriage return as part of a CRLF line ending.
			wantErr: true,
		},
		{
			shortenOptions: responsefile.ShortenOptions{Format: 99},
			args:           []string{"foo"},
			wantErr:        true,
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
				if !test.wantErr {
					t.Fatal(err)
				}
				return
			}
			if test.wantErr {
				t.Fatalf("expected an error, got %q", shortened)
			}
			if cleanup == nil {
				t.Fatal("cleanup func must not be nil without an error")
			}
			t.Cleanup(cleanup)
			if test.wantFile == "" {
				if !slices.Equal(shortened, test.args) {
					t.Fatalf("did not expect a response file, got %q", shortened)
				}
				return
			}
			if len(shortened) != 1 || !strings.HasPrefix(shortened[0], "@") {
				t.Fatalf("expected a single response file argument, got %q", shortened)
			}
			path := shortened[0][1:]
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.wantFile {
				t.Fatalf("response file got %q, want %q", got, test.wantFile)
			}
			cleanup()
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("cleanup did not remove the response file: %v", err)
			}
		})
	}
}

// FuzzRoundtrip checks that [responsefile.Expand] undoes [responsefile.Shorten].
// The input holds the arguments separated by NUL bytes,
// which cannot appear in program arguments.
func FuzzRoundtrip(f *testing.F) {
	formats := []responsefile.Format{responsefile.FormatGCC, responsefile.FormatLines}
	for _, format := range formats {
		f.Add("", int(format))
		f.Add("foo\x00bar\x00baz", int(format))
		f.Add("back\\slash\x00new\nline\x00", int(format))
		f.Add("a\xff\\b\x00c\xff\nd", int(format))
	}
	f.Add("it's\x00say \"hi\"\x00tab\there\x00cr\r", int(responsefile.FormatGCC))
	f.Fuzz(func(t *testing.T, joined string, format int) {
		if !slices.Contains(formats, responsefile.Format(format)) {
			t.Skip("unknown format")
		}
		args := strings.Split(joined, "\x00")
		if slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "@") }) {
			t.Skip("arguments starting with @ are response files")
		}
		if format == int(responsefile.FormatLines) && slices.ContainsFunc(args, func(arg string) bool { return strings.HasSuffix(arg, "\r") }) {
			t.Skip("FormatLines cannot write arguments ending with a carriage return")
		}
		shortened, cleanup, err := responsefile.Shorten(args, responsefile.ShortenOptions{
			Format:         responsefile.Format(format),
			ArgLengthLimit: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		expanded, err := responsefile.Expand(shortened, responsefile.ExpandOptions{
			Format: responsefile.Format(format),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(expanded, args) {
			t.Fatalf("roundtrip got %q, want %q", expanded, args)
		}
	})
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
		// Use forward slashes, as backslashes are escapes in response files.
		return "@" + filepath.ToSlash(f.Name())
	}
	lines := responsefile.ExpandOptions{Format: responsefile.FormatLines}
	missing := "@" + filepath.ToSlash(filepath.Join(tdir, "missing"))
	listsMissing := atTemp("bar\n" + missing)
	cycle := filepath.ToSlash(filepath.Join(tdir, "cycle"))
	if err := os.WriteFile(cycle, []byte("foo\n@"+cycle+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		expandOptions responsefile.ExpandOptions
		args          []string
		wantArgs      []string
		wantErr       string // a substring of the error, if any
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
			args:     []string{atTemp("  foo\tbar\n\nbaz  qux \r\n")},
			wantArgs: []string{"foo", "bar", "baz", "qux"},
		},
		{
			args:     []string{atTemp(`'single \"quoted\"' "double \"quoted\" \$x \y" un\ quoted`)},
			wantArgs: []string{`single \"quoted\"`, `double "quoted" $x \y`, "un quoted"},
		},
		{
			args:     []string{atTemp(`"" '' a""b`)},
			wantArgs: []string{"", "", "ab"},
		},
		{
			args:     []string{atTemp("con\\\ntinued \"con\\\r\ntinued\" \\\n \"multi\nline\" trailing\\")},
			wantArgs: []string{"continued", "continued", "multi\nline", "trailing"},
		},
		{
			args:     []string{atTemp("foo \\")},
			wantArgs: []string{"foo"},
		},
		{
			args: []string{atTemp(`C:\dir "C:\dir\\"`)},
			// Backslashes escape the next character unless double-quoted.
			wantArgs: []string{"C:dir", `C:\dir\`},
		},
		{
			args: []string{atTemp("\"a\xff\\\\b\"")},
			// Invalid UTF-8 is kept as-is.
			wantArgs: []string{"a\xff\\b"},
		},
		{
			expandOptions: lines,
			args:          []string{atTemp("foo bar\n'baz'\n\n")},
			wantArgs:      []string{"foo bar", "'baz'", ""},
		},
		{
			expandOptions: lines,
			args:          []string{atTemp("escaped\\\\\\\nchars")},
			// A trailing lone backslash is kept.
			wantArgs: []string{"escaped\\\\", "chars"},
		},
		{
			expandOptions: lines,
			args:          []string{atTemp(`C:\dir\`)},
			// A trailing lone backslash is kept.
			wantArgs: []string{`C:\dir\`},
		},
		{
			expandOptions: lines,
			args:          []string{atTemp("a\xff\\\\b")},
			// Invalid UTF-8 is kept as-is.
			wantArgs: []string{"a\xff\\b"},
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
		{
			args:    []string{"foo", missing},
			wantErr: "cannot read response file: ",
		},
		{
			args:    []string{"foo", listsMissing},
			wantErr: "cannot read response file listed in " + listsMissing[1:] + ": ",
		},
		{
			args:    []string{"@" + cycle},
			wantErr: "cannot expand more than 2000 response files",
		},
		{
			args:    []string{"@"},
			wantErr: "cannot read response file: ",
		},
		{
			expandOptions: responsefile.ExpandOptions{Format: 99},
			args:          []string{"foo"},
			wantErr:       "unknown response file format",
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			expanded, err := responsefile.Expand(test.args, test.expandOptions)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected an error containing %q, got %v", test.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(expanded, test.wantArgs) {
				t.Fatalf("expand got %#v, expected %#v", expanded, test.wantArgs)
			}
		})
	}
}
