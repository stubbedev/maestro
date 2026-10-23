// Ports nothing: what a ConsoleIO wrote, recorded to be written again by a
// later run that skips the work that wrote it (deliberate deviation 3,
// speed).

package io

import (
	"encoding/binary"
	"errors"
	"slices"
)

// Recording is what a ConsoleIO wrote while it recorded (Record), in
// order: every write as the console got it, before the debugging and
// timestamp prefixes, and the last message an overwrite left.
type Recording struct {
	steps []recordedStep
}

type recordedStep struct {
	kind      stepKind
	messages  []string
	newline   bool
	stderr    bool
	raw       bool
	verbosity Verbosity
}

type stepKind uint8

const (
	stepWrite stepKind = iota
	// stepLastMessage is an overwrite's setLastMessage, messages[0] the
	// message
	stepLastMessage
)

// Recorder is a recording in progress (Record).
type Recorder struct {
	c       *ConsoleIO
	steps   []recordedStep
	spoiled bool
}

// Record starts recording what c writes, until Stop.
func (c *ConsoleIO) Record() *Recorder {
	rec := &Recorder{c: c, spoiled: c.debugging || c.timestamp != nil}
	c.recorder = rec

	return rec
}

// Len is the number of steps recorded so far.
func (rec *Recorder) Len() int { return len(rec.steps) }

// Stop ends the recording and returns it; ok is false when the IO did
// something a recording cannot write again alike: it asked a question,
// drew a progress bar, or prefixed its messages with the memory, time or
// date.
func (rec *Recorder) Stop() (r *Recording, ok bool) {
	if rec.c.recorder == rec {
		rec.c.recorder = nil
	}

	return &Recording{steps: rec.steps}, !rec.spoiled
}

// spoil marks the recording in progress, if any, as one that cannot be
// written again.
func (c *ConsoleIO) spoil() {
	if c.recorder != nil {
		c.recorder.spoiled = true
	}
}

// record adds a step to the recording in progress, if any.
func (c *ConsoleIO) record(step recordedStep) {
	if c.recorder != nil {
		step.messages = append([]string(nil), step.messages...)
		c.recorder.steps = append(c.recorder.steps, step)
	}
}

// Replay writes r to c as it was written when it was recorded.
func (c *ConsoleIO) Replay(r *Recording) {
	for _, s := range r.steps {
		switch s.kind {
		case stepWrite:
			c.doWrite(append([]string(nil), s.messages...), s.newline, s.stderr, s.verbosity, s.raw)
		case stepLastMessage:
			c.setLastMessage(s.stderr, s.messages[0])
		}
	}
}

// Equal reports whether r and o hold the same steps.
func (r *Recording) Equal(o *Recording) bool {
	return slices.EqualFunc(r.steps, o.steps, func(a, b recordedStep) bool {
		return a.kind == b.kind && a.newline == b.newline && a.stderr == b.stderr && a.raw == b.raw &&
			a.verbosity == b.verbosity && slices.Equal(a.messages, b.messages)
	})
}

// AppendBinary appends r to b, for ReadRecording.
func (r *Recording) AppendBinary(b []byte) []byte {
	b = binary.AppendUvarint(b, uint64(len(r.steps)))
	for _, s := range r.steps {
		var flags byte
		for i, set := range [...]bool{s.newline, s.stderr, s.raw} {
			if set {
				flags |= 1 << i
			}
		}
		b = append(b, byte(s.kind), flags)
		b = binary.AppendUvarint(b, uint64(s.verbosity)) //nolint:gosec // one of the positive IOInterface levels
		b = binary.AppendUvarint(b, uint64(len(s.messages)))
		for _, m := range s.messages {
			b = binary.AppendUvarint(b, uint64(len(m)))
			b = append(b, m...)
		}
	}

	return b
}

var errRecording = errors.New("malformed output recording")

// ReadRecording reads a Recording AppendBinary appended at the start of b,
// and returns what follows it.
func ReadRecording(b []byte) (*Recording, []byte, error) {
	uvarint := func() (uint64, bool) {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			return 0, false
		}
		b = b[n:]

		return v, true
	}
	str := func() (string, bool) {
		n, ok := uvarint()
		if !ok || n > uint64(len(b)) {
			return "", false
		}
		s := string(b[:n])
		b = b[n:]

		return s, true
	}

	count, ok := uvarint()
	if !ok || count > uint64(len(b)) {
		return nil, nil, errRecording
	}
	r := &Recording{steps: make([]recordedStep, 0, count)}
	for range count {
		if len(b) < 2 {
			return nil, nil, errRecording
		}
		s := recordedStep{kind: stepKind(b[0])}
		flags := b[1]
		b = b[2:]
		if s.kind > stepLastMessage || flags > 7 {
			return nil, nil, errRecording
		}
		s.newline, s.stderr, s.raw = flags&1 != 0, flags&2 != 0, flags&4 != 0
		verbosity, ok := uvarint()
		if !ok || verbosity > uint64(Debug) {
			return nil, nil, errRecording
		}
		s.verbosity = Verbosity(verbosity)
		n, ok := uvarint()
		if !ok || n > uint64(len(b)) || (s.kind == stepLastMessage && n != 1) {
			return nil, nil, errRecording
		}
		s.messages = make([]string, 0, n)
		for range n {
			m, ok := str()
			if !ok {
				return nil, nil, errRecording
			}
			s.messages = append(s.messages, m)
		}
		r.steps = append(r.steps, s)
	}

	return r, b, nil
}
