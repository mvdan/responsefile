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
// Useful links:
// * https://gcc.gnu.org/wiki/Response_Files
// * https://learn.microsoft.com/en-us/windows/win32/midl/response-files
// * https://www.intel.com/content/www/us/en/docs/dpcpp-cpp-compiler/developer-guide-reference/2023-0/use-response-files.html
//
// TODO: support reading response files.
// TODO: some implementations support quoting.
// TODO: some implementations support '#' comments.
package responsefile

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Config holds parameters for reading and writing response files.
type Config struct {
	// ArgLengthLimit is the number of bytes which can be passed directly
	// as arguments without using response files.
	// The zero value implies the default of 30KiB,
	// as Windows is known to have a limit of around 32KiB.
	//
	// A negative value can be used to always create response files.
	//
	// Used in [Config.WithResponseFiles].
	ArgLengthLimit int
}

func (cfg Config) applyDefaults() Config {
	if cfg.ArgLengthLimit == 0 {
		cfg.ArgLengthLimit = 30 << 10 // 30KiB, since Windows can limit at 32KiB
	}
	return cfg
}

// WithResponseFiles produces a copy of args which may use response files
// depending on Config.
//
// If no error is reported, a cleanup func is returned,
// which must be called to avoid leaving temporary files behind.
func (cfg Config) WithResponseFiles(args []string) (_ []string, cleanup func(), _ error) {
	cfg = cfg.applyDefaults()

	// TODO: does the
	var argLen int
	for _, arg := range args {
		argLen += len(arg)
	}
	if argLen == 0 || argLen <= cfg.ArgLengthLimit {
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
