//go:build !unix

package rootio

// nonblockOpen is zero where Unix FIFOs do not exist as filesystem names. There
// is nothing that can block in the open, and the handle's regular-file check
// still refuses any other irregular target.
const nonblockOpen = 0
