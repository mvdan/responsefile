# responsefile

[![Go Reference](https://pkg.go.dev/badge/mvdan.cc/responsefile.svg)](https://pkg.go.dev/mvdan.cc/responsefile)

A tiny library to support resonse files,
newline-separated plaintext files which hold lists of arguments.

Response files are commonly used on systems with low argument length limits,
such as Windows.

Supported by [GCC](https://gcc.gnu.org/wiki/Response_Files),
[Windows](https://learn.microsoft.com/en-us/windows/win32/midl/response-files),
the Go toolchain, and many others.
