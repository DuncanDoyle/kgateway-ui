// Package memlimit holds the UI's default soft memory limit (D-043), shared by
// the binary and the budget tests that measure with it.
package memlimit

// Default is the soft memory limit the binary sets when GOMEMLIMIT is unset:
// it keeps the memory taken from the OS under the 256 MiB gate (design §16.3).
const Default int64 = 224 << 20
