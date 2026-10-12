// Copyright (c) 2023, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package responsefile provides support for response files,
// plaintext files which hold lists of arguments.
//
// Response files are commonly used on systems with low argument length limits,
// such as Windows. An argument starting with '@' followed by a file path
// is replaced by the arguments held in that response file,
// which may themselves include nested response files.
//
// Response files have no formal specification, and their syntax varies
// between programs; see [Format] for the supported syntaxes.
//
// Useful links:
//   - https://gcc.gnu.org/wiki/Response_Files
//   - https://go.dev/issue/77177
package responsefile

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Format is a syntax for response files.
type Format int

const (
	// FormatGCC separates arguments by whitespace,
	// and supports quoting with single and double quotes as well as
	// escaping with backslashes, following GCC's libiberty.
	// It is understood by GCC, Clang, and Go 1.27 or later.
	FormatGCC Format = iota

	// FormatLines puts each argument on its own line,
	// escaping backslashes as `\\` and newlines as `\n`.
	// It is understood by Go 1.26 or earlier.
	FormatLines
)

// funcs returns the functions which write and read response files in f.
func (f Format) funcs() (writeArg func(*strings.Builder, string), parse func([]byte) []string, _ error) {
	switch f {
	case FormatGCC:
		return writeGCCArg, parseGCC, nil
	case FormatLines:
		return writeLinesArg, parseLines, nil
	}
	return nil, nil, fmt.Errorf("unknown response file format: %d", f)
}

// ShortenOptions holds parameters for [Shorten].
type ShortenOptions struct {
	// Format is the syntax used to write response files.
	Format Format

	// ArgLengthLimit is the number of bytes which can be passed directly
	// as arguments without using response files.
	// Separators and any quoting needed on Windows count towards the limit.
	// The zero value implies the default of 30KiB,
	// as Windows is known to have a limit of around 32KiB.
	//
	// A negative value can be used to always create response files,
	// unless there are no arguments.
	ArgLengthLimit int
}

func (opts ShortenOptions) applyDefaults() ShortenOptions {
	if opts.ArgLengthLimit == 0 {
		opts.ArgLengthLimit = 30 << 10 // 30KiB, since Windows can limit at 32KiB
	}
	return opts
}

// Shorten produces an argument list which may use response files
// if args is too long.
//
// If no error is reported, a cleanup func is returned,
// which must be called to avoid leaving temporary files behind.
//
// The args slice may be returned directly if no response files were needed;
// otherwise, a new slice is returned.
func Shorten(args []string, opts ShortenOptions) (_ []string, cleanup func(), _ error) {
	opts = opts.applyDefaults()
	writeArg, _, err := opts.Format.funcs()
	if err != nil {
		return nil, nil, err
	}

	var argLen int
	for _, arg := range args {
		argLen += cmdLineLen(arg)
	}
	if len(args) == 0 || argLen <= opts.ArgLengthLimit {
		return args, func() {}, nil
	}

	// We will need space for at least each argument plus a newline.
	var buf strings.Builder
	buf.Grow(argLen)
	for _, arg := range args {
		writeArg(&buf, arg)
		buf.WriteByte('\n')
	}

	f, err := os.CreateTemp("", "responsefile")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create response file: %w", err)
	}
	// In the rare case where we were able to create a temporary file but we
	// cannot remove it, there's not much that can be done about it.
	cleanup = func() { os.Remove(f.Name()) }

	if _, err := f.WriteString(buf.String()); err != nil {
		f.Close()
		cleanup()
		return nil, nil, fmt.Errorf("cannot write response file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("cannot close response file: %w", err)
	}
	args = []string{"@" + f.Name()}
	return args, cleanup, nil
}

// cmdLineLen returns an upper bound for the number of bytes that arg takes
// on a command line, including a separator.
// On Windows, arguments with spaces or tabs are quoted,
// and backslashes and quotes may need to be escaped.
func cmdLineLen(arg string) int {
	n := len(arg) + 1
	if arg == "" || strings.ContainsAny(arg, " \t") {
		n += 2
	}
	return n + strings.Count(arg, `\`) + strings.Count(arg, `"`)
}

var (
	gccEscaper     = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	linesEscaper   = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	linesUnescaper = strings.NewReplacer(`\\`, `\`, `\n`, "\n")
)

// writeGCCArg writes arg to buf in [FormatGCC],
// using double quotes and escaping when needed.
func writeGCCArg(buf *strings.Builder, arg string) {
	if arg != "" && !strings.ContainsAny(arg, gccSpace+`'"\`) {
		buf.WriteString(arg) // shortcut
		return
	}
	buf.WriteByte('"')
	gccEscaper.WriteString(buf, arg)
	buf.WriteByte('"')
}

// writeLinesArg writes arg to buf in [FormatLines],
// escaping backslashes and newlines.
func writeLinesArg(buf *strings.Builder, arg string) {
	linesEscaper.WriteString(buf, arg)
}

// ExpandOptions holds parameters for [Expand].
type ExpandOptions struct {
	// Format is the syntax used to read response files.
	Format Format
}

// Expand produces an argument list with any response files
// replaced with their inner arguments.
//
// The args slice may be returned directly if no response files were found;
// otherwise, a new slice is returned.
func Expand(args []string, opts ExpandOptions) ([]string, error) {
	_, parse, err := opts.Format.funcs()
	if err != nil {
		return nil, err
	}
	var count int
	return expand(args, parse, &count)
}

// maxResponseFiles is how many response files a single [Expand] call may read,
// guarding against cycles and exponential growth. GCC uses the same limit.
const maxResponseFiles = 2000

func expand(args []string, parse func([]byte) []string, count *int) ([]string, error) {
	var expanded []string
	for i, s := range args {
		path, ok := strings.CutPrefix(s, "@")
		if !ok {
			if expanded != nil {
				expanded = append(expanded, s)
			}
			continue
		}
		if expanded == nil {
			expanded = make([]string, 0, len(args)*2)
			expanded = append(expanded, args[:i]...)
		}
		*count++
		if *count > maxResponseFiles {
			return nil, fmt.Errorf("cannot expand more than %d response files", maxResponseFiles)
		}
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read response file: %w", err)
		}
		nested, err := expand(parse(buf), parse, count)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, nested...)
	}
	// Avoid making a copy of the slice when there are no response files.
	if expanded == nil {
		return args, nil
	}
	return expanded, nil
}

// gccSpace holds the bytes which separate arguments in [FormatGCC].
const gccSpace = " \t\n\v\f\r"

// parseGCC splits a response file in [FormatGCC] into arguments,
// following buildargv in GCC's libiberty.
// Like Go, a backslash followed by CRLF is also a line continuation,
// and neither a line continuation alone nor a trailing backslash
// produces an empty argument, unlike GCC.
func parseGCC(s []byte) []string {
	var args []string
	var arg []byte
	started := false // whether an argument has started, even if empty
	var squote, dquote bool
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case squote:
			if b == '\'' {
				squote = false
			} else {
				arg = append(arg, b)
			}
		case b == '\\':
			next := s[i+1:]
			switch {
			case bytes.HasPrefix(next, []byte("\n")):
				i++ // line continuation
			case bytes.HasPrefix(next, []byte("\r\n")):
				i += 2 // line continuation
			case len(next) == 0:
				// A trailing backslash is dropped.
			case dquote && strings.IndexByte("$`\"\\", next[0]) < 0:
				arg = append(arg, b) // a backslash escaping nothing is kept
			default:
				arg = append(arg, next[0])
				started = true
				i++
			}
		case dquote:
			if b == '"' {
				dquote = false
			} else {
				arg = append(arg, b)
			}
		case strings.IndexByte(gccSpace, b) >= 0:
			if started {
				args = append(args, string(arg))
				arg = arg[:0]
				started = false
			}
		case b == '\'':
			squote, started = true, true
		case b == '"':
			dquote, started = true, true
		default:
			arg = append(arg, b)
			started = true
		}
	}
	if started {
		args = append(args, string(arg))
	}
	return args
}

// parseLines splits a response file in [FormatLines] into arguments.
func parseLines(b []byte) []string {
	var args []string
	s := string(b)
	for len(s) > 0 {
		var line string
		line, s, _ = strings.Cut(s, "\n")
		line = strings.TrimSuffix(line, "\r") // support CRLF
		if strings.Contains(line, `\`) {
			line = linesUnescaper.Replace(line)
		}
		args = append(args, line)
	}
	return args
}
