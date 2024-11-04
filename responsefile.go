// Copyright (c) 2023, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package responsefile provides support for response files,
// newline-separated plaintext files which hold lists of arguments.
//
// Response files are commonly used on systems with low argument length limits,
// such as Windows. They are supported by programs like GCC, Go's compiler
// and linker, Windows toolchains, and the ninja build system.
//
// Response files have no formal specification or grammar,
// but they are commonly identified by an argument starting with '@'
// followed by a path to a file containing newline-separated arguments.
// Since arguments may themselves contain newlines,
// newline and backslash characters are escaped with backslashes.
//
// Nested response files are also supported, although not all programs support
// reading them.
//
// Useful links:
// * https://gcc.gnu.org/wiki/Response_Files
// * https://learn.microsoft.com/en-us/windows/win32/midl/response-files
// * https://www.intel.com/content/www/us/en/docs/dpcpp-cpp-compiler/developer-guide-reference/2023-0/use-response-files.html
//
// TODO: some implementations support quoting.
// TODO: some implementations support '#' comments.
package responsefile

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// ShortenOptions holds parameters for [Shorten].
type ShortenOptions struct {
	// ArgLengthLimit is the number of bytes which can be passed directly
	// as arguments without using response files.
	// The zero value implies the default of 30KiB,
	// as Windows is known to have a limit of around 32KiB.
	//
	// A negative value can be used to always create response files.
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

	// TODO: does the
	var argLen int
	for _, arg := range args {
		argLen += len(arg)
	}
	if argLen == 0 || argLen <= opts.ArgLengthLimit {
		return args, func() {}, nil
	}

	// We will need space for at least each argument plus a newline.
	buf := make([]byte, 0, argLen+len(args))
	for _, arg := range args {
		buf = appendEncodedArg(buf, arg)
		buf = append(buf, '\n')
	}

	f, err := os.CreateTemp("", "responsefile")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create response file: %w", err)
	}
	// In the rare case where we were able to create a temporary file but we
	// cannot remove it, there's not much that can be done about it.
	cleanup = func() { os.Remove(f.Name()) }

	if _, err := f.Write(buf); err != nil {
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

// appendEncodedArg appends arg to buf while escaping backslashes and
// newlines.
func appendEncodedArg(buf []byte, arg string) []byte {
	if !strings.ContainsAny(arg, "\\\n") {
		return append(buf, arg...) // shortcut
	}
	for _, r := range arg {
		switch r {
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		default:
			buf = utf8.AppendRune(buf, r)
		}
	}
	return buf
}

// ExpandOptions holds parameters for [Expand].
type ExpandOptions struct {
	// Empty for now; we will likely need parameters in the future.
	// For example, it might be nice to support io/fs.
}

// Expand produces an argument list with any response files
// replaced with their inner arguments.
//
// The args slice may be returned directly if no response files were found;
// otherwise, a new slice is returned.
func Expand(args []string, opts ExpandOptions) ([]string, error) {
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
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read response file: %w", err)
		}
		// Parsing the entire file as a string is perhaps unnecessary,
		// but it simplifies the code and may result in fewer allocs.
		rest := string(buf)
		for len(rest) > 0 {
			var line string
			line, rest, _ = strings.Cut(rest, "\n")
			// TODO: errors should include filename and ideally position.
			// TODO: should we trim all surrounding spaces?
			// TODO: should we skip empty lines?
			line = strings.TrimSuffix(line, "\r") // support CRLF
			arg, err := decodeArg(line)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(arg, "@") {
				// Nested response files, which should be rare.
				nested, err := Expand([]string{arg}, opts)
				if err != nil {
					return nil, err
				}
				expanded = append(expanded, nested...)
			} else {
				expanded = append(expanded, arg)
			}
		}
	}
	// Avoid making a copy of the slice when there are no response files.
	if expanded == nil {
		return args, nil
	}
	return expanded, nil
}

func decodeArg(line string) (string, error) {
	if !strings.Contains(line, "\\") {
		return line, nil // shortcut
	}

	var buf strings.Builder
	var escaping bool
	for _, r := range line {
		if escaping {
			switch r {
			case '\\':
				buf.WriteByte('\\')
			case 'n':
				buf.WriteByte('\n')
			default:
				return "", fmt.Errorf("unsupported escape sequence: %q", "\\"+string(r))
			}
			escaping = false
		} else if r == '\\' {
			escaping = true
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String(), nil
}
