# responsefile

[![Go Reference](https://pkg.go.dev/badge/mvdan.cc/responsefile.svg)](https://pkg.go.dev/mvdan.cc/responsefile)

A tiny library to support response files,
plaintext files which hold lists of arguments.

Response files are commonly used on systems with low argument length limits,
such as Windows.

The default format follows [GCC](https://gcc.gnu.org/wiki/Response_Files),
which is also understood by Clang and [Go 1.27 or later](https://go.dev/issue/77177).
The newline-separated format used by Go 1.26 or earlier is also supported.
