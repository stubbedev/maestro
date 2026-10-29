// Ports tests/Composer/Test/Console/Osc7501ProgramStatusEncoderTest.php
// (Composer).

package console

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOsc7501ProgramStatusEncoder_CommandStartReportsWorking(t *testing.T) {
	payload := (&Osc7501ProgramStatusEncoder{}).EncodeStart()
	if payload == nil || *payload != "state=working:app=composer" {
		t.Errorf("encodeStart = %v", payload)
	}
}

func TestOsc7501ProgramStatusEncoder_EncodesRootRecordWithProgress(t *testing.T) {
	encoder := &Osc7501ProgramStatusEncoder{}
	if got := encoder.Command(); got != "7501" {
		t.Errorf("command = %q", got)
	}
	if got := encoder.Encode(ProgramStatusWorking(nil).WithProgress(new(0))); got != "state=working:app=composer:progress=0" {
		t.Errorf("progress 0 = %q", got)
	}
	if got := encoder.Encode(ProgramStatusWorking(nil).WithProgress(new(100))); got != "state=working:app=composer:progress=100" {
		t.Errorf("progress 100 = %q", got)
	}
	if got := encoder.Encode(ProgramStatusDone(nil)); got != "state=done:app=composer" {
		t.Errorf("done = %q", got)
	}
}

func TestOsc7501ProgramStatusEncoder_EncodesSupportedKinds(t *testing.T) {
	for _, kind := range []string{Osc7501KindPermission, Osc7501KindQuestion, Osc7501KindAuth} {
		status := ProgramStatusBlocked(nil).WithProtocolOption(Osc7501Protocol, "kind", kind)
		want := "state=blocked:app=composer:kind=" + kind
		if got := (&Osc7501ProgramStatusEncoder{}).Encode(status); got != want {
			t.Errorf("%s = %q, want %q", kind, got, want)
		}
	}
}

func TestOsc7501ProgramStatusEncoder_IgnoresInvalidKindsWhenEncoding(t *testing.T) {
	values := []any{
		"foobar",
		"permission:msg=injected",
		false,
		42,
		1.5,
		"",
		[]any{},
		[]any{"permission"},
		&stdoutObject{name: "object"},
	}
	for _, value := range values {
		status := ProgramStatusBlocked(nil).WithProtocolOption(Osc7501Protocol, "kind", value)
		if got := (&Osc7501ProgramStatusEncoder{}).Encode(status); got != "state=blocked:app=composer" {
			t.Errorf("%#v = %q", value, got)
		}
	}
}

func TestOsc7501ProgramStatusEncoder_IgnoresKindForNonBlockedStateWhenEncoding(t *testing.T) {
	for _, status := range []*ProgramStatus{
		ProgramStatusIdle(nil), ProgramStatusWorking(nil), ProgramStatusDone(nil),
		ProgramStatusError(nil), ProgramStatusClear(nil),
	} {
		status = status.WithProtocolOption(Osc7501Protocol, "kind", "auth")
		want := "state=" + status.State() + ":app=composer"
		if got := (&Osc7501ProgramStatusEncoder{}).Encode(status); got != want {
			t.Errorf("%s = %q, want %q", status.State(), got, want)
		}
	}
}

func TestOsc7501ProgramStatusEncoder_IgnoresOtherProtocolOptionsAndAbsentKind(t *testing.T) {
	encoder := &Osc7501ProgramStatusEncoder{}
	status := ProgramStatusBlocked(nil).WithProtocolOption("oscFuture", "kind", map[string]any{"nested": []any{false}})
	if got := encoder.Encode(status); got != "state=blocked:app=composer" {
		t.Errorf("other protocol = %q", got)
	}
	status = status.WithProtocolOption(Osc7501Protocol, "kind", nil)
	if got := encoder.Encode(status); got != "state=blocked:app=composer" {
		t.Errorf("absent kind = %q", got)
	}
}

func TestOsc7501ProgramStatusEncoder_SanitizesAndEncodesMessage(t *testing.T) {
	message := "<info>Approve?</info>\n\x00\x7f\u0085\x1b]2;title\a"
	status := ProgramStatusBlocked(&message).WithProtocolOption(Osc7501Protocol, "kind", Osc7501KindPermission)
	want := "state=blocked:app=composer:kind=permission:msg=" + base64.StdEncoding.EncodeToString([]byte("Approve?"))
	if got := (&Osc7501ProgramStatusEncoder{}).Encode(status); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOsc7501ProgramStatusEncoder_MessageLimitPreservesUtf8(t *testing.T) {
	cases := []struct{ message, expected string }{
		{strings.Repeat("a", 2049), strings.Repeat("a", 2048)},
		{strings.Repeat("a", 2046) + "éx", strings.Repeat("a", 2046) + "é"},
		{strings.Repeat("a", 2047) + "é", strings.Repeat("a", 2047)},
		{strings.Repeat("a", 2046) + "€", strings.Repeat("a", 2046)},
		{strings.Repeat("a", 2045) + "😀", strings.Repeat("a", 2045)},
	}
	for _, tc := range cases {
		status := ProgramStatusWorking(&tc.message)
		encoded := (&Osc7501ProgramStatusEncoder{}).Encode(status)
		want := "state=working:app=composer:msg=" + base64.StdEncoding.EncodeToString([]byte(tc.expected))
		if encoded != want {
			t.Errorf("message %q = %q, want %q", tc.message, encoded, want)
		}
		if len(encoded)+9 >= 4096 {
			t.Errorf("encoded length %d", len(encoded)+9)
		}
	}
}
