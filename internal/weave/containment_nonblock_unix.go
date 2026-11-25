//go:build unix

package weave

import "syscall"

// nonblockOpen is added to every confined open of a name the code did not just
// create. On Unix a FIFO at that name would otherwise block in open: O_RDONLY waits
// for a writer and O_WRONLY waits for a reader, so a planted FIFO could hang the
// panel or a ledger append. With O_NONBLOCK the open returns at once and the
// handle's regular-file check refuses the FIFO before any read or write. The flag is
// ignored for regular files, so ordinary reads and appends are unchanged.
const nonblockOpen = syscall.O_NONBLOCK
