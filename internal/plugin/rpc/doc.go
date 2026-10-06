// Package rpc is maestro's half of the IPC channel to the PHP plugin shim
// (docs/PLUGINS.md §5.3, §5.10, §5.14, §6): framing, the value codec,
// handles, the re-entrant call stack and the sync engine. The shim's half
// is internal/plugin/php/src/Maestro/Shim (Rpc, Codec, Handles, Sync,
// Mirrors, Exceptions); the two are kept in step by the tests of this
// package and of internal/plugin.
//
// # The call stack
//
// Exactly one side runs at any moment. A Conn sends a call and then reads
// messages until the reply to that call arrives, serving every call PHP
// makes meanwhile by running its Handler on the same goroutine; a handler
// may call PHP again, to any depth. A reply must answer the innermost
// outstanding call of the other side, so the conversation is a single
// stack, as Composer's own call stack is.
//
// # The baton
//
// Only the goroutine holding the baton may call PHP (§5.14). A top-level
// call takes the baton when it is free; calls made by handlers run on the
// holder's goroutine and nest. A call from any other goroutine while the
// baton is held is a bug (a parallel download calling plugin code) and
// fails with ErrBaton instead of corrupting the stack. Delegate lends the
// baton to one other goroutine explicitly. Parallel work whose calls must
// reach PHP (an IO created in PHP) makes them through Run: on the holder,
// as a guest when the baton is free, or posted to the holder, which makes
// them in order before its next call, before it gives the baton up, and
// while it waits (ServePosted, util's wait hooks).
//
// # Sync
//
// Every message carries what changed on its sender's side since the
// previous one: environment variables, the working directory, Composer's
// statics, and the fields of mirrored objects (by revision), plus the
// handles maestro assigned to PHP-born objects. The receiver applies the
// block before anything else in the message. See sync.go.
//
// # Failure
//
// When the PHP process ends while a call is outstanding (exit() in plugin
// code, a fatal error, a signal), every pending call and every later one
// returns a *PHPExit with its exit status. A message that breaks the
// protocol kills the process and fails the same way with a
// *ProtocolError. Go never relies on EOF alone: the Peer reports the
// process's exit, because grandchildren may hold the channel open.
package rpc
