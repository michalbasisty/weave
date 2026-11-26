//go:build unix

package rootio

import "syscall"

// nonblockOpen is added to every open. On Unix a FIFO at the name would
// otherwise block in open: O_RDONLY waits for a writer and O_WRONLY waits for a
// reader, so a planted FIFO could hang a writer. O_NONBLOCK makes the open return
// at once and the handle's regular-file check refuses the FIFO. The flag is
// ignored for regular files, so ordinary writes are unchanged.
const nonblockOpen = syscall.O_NONBLOCK
