package io

import (
	"reflect"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
)

// What a ConsoleIO wrote while recording is written again alike by
// Replay, after a round trip through its binary form; writes below the
// verbosity are not recorded.
func TestRecording_Replay(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal, decorated: true}
	c := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	c.Write("before", true, Normal)

	rec := c.Record()
	c.WriteError("<info>Updating dependencies</info>", true, Normal)
	c.WriteError("hidden", true, Verbose)
	c.WriteMessages([]string{"one", "two"}, false, Normal)
	c.OverwriteError("progress", true, -1, Normal)
	c.WriteRaw("<raw>", true, Quiet)
	if rec.Len() == 0 {
		t.Fatal("nothing recorded")
	}
	recording, ok := rec.Stop()
	if !ok {
		t.Fatal("the recording is spoiled")
	}
	lastMessage, lastMessageErr := c.lastMessage, c.lastMessageErr
	c.Write("after", true, Normal)
	written := append([]writeCall(nil), out.writes[1:len(out.writes)-1]...)

	decoded, rest, err := ReadRecording(recording.AppendBinary(nil))
	if err != nil || len(rest) != 0 || !decoded.Equal(recording) {
		t.Fatalf("round trip: %v, %d bytes left, equal %v", err, len(rest), decoded.Equal(recording))
	}

	replayOut := &fakeOutput{verbosity: console.VerbosityNormal, decorated: true}
	replay := NewConsoleIO(&fakeInput{}, replayOut, &fakeHelperSet{})
	replay.Replay(decoded)
	if !reflect.DeepEqual(replayOut.writes, written) {
		t.Errorf("replayed %#v, want %#v", replayOut.writes, written)
	}
	if replay.lastMessageErr != lastMessageErr || replay.lastMessage != lastMessage {
		t.Errorf("replay left last messages %q, %q, want %q, %q", replay.lastMessage, replay.lastMessageErr, lastMessage, lastMessageErr)
	}
}

// A recording is spoiled by a question, a progress bar, and the debugging
// and timestamp prefixes.
func TestRecording_Spoiled(t *testing.T) {
	for name, act := range map[string]func(c *ConsoleIO){
		"progress bar": func(c *ConsoleIO) { c.ProgressBar(1) },
		"question":     func(c *ConsoleIO) { _, _ = c.Ask("?", nil) },
	} {
		c := NewConsoleIO(&fakeInput{}, &fakeOutput{verbosity: console.VerbosityNormal}, &fakeHelperSet{})
		rec := c.Record()
		act(c)
		if _, ok := rec.Stop(); ok {
			t.Errorf("%s: the recording is not spoiled", name)
		}
	}

	c := NewConsoleIO(&fakeInput{}, &fakeOutput{verbosity: console.VerbosityNormal}, &fakeHelperSet{})
	c.EnableDebugging(time.Now())
	if _, ok := c.Record().Stop(); ok {
		t.Error("debugging: the recording is not spoiled")
	}
}

// A truncated or corrupted recording does not read.
func TestReadRecording_Malformed(t *testing.T) {
	c := NewConsoleIO(&fakeInput{}, &fakeOutput{verbosity: console.VerbosityNormal}, &fakeHelperSet{})
	rec := c.Record()
	c.Write("message", true, Normal)
	recording, _ := rec.Stop()
	b := recording.AppendBinary(nil)
	for n := range b {
		if r, rest, err := ReadRecording(b[:n]); err == nil && len(rest) == 0 && r.Equal(recording) {
			t.Fatalf("%d of %d bytes read as the recording", n, len(b))
		}
	}
}
