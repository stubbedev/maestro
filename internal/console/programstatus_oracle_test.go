package console

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/testutil"
)

type programStatusOracle struct {
	Encode []struct {
		State     string  `json:"state"`
		Message   *string `json:"message"`
		Progress  *int    `json:"progress"`
		Kind      any     `json:"kind"`
		Osc7501   *string `json:"osc7501"`
		Osc9      *string `json:"osc9"`
		Start7501 *string `json:"start7501"`
		Start9    *string `json:"start9"`
	} `json:"encode"`
	Policy []struct {
		Environment map[string]string `json:"environment"`
		Protocols   []string          `json:"protocols"`
	} `json:"policy"`
}

// TestProgramStatusOracle_Encode compares ProgramStatus's encoders with
// Composer's for every state, progress, kind and message of the golden
// (tools/oracle/console/programstatus.php).
func TestProgramStatusOracle_Encode(t *testing.T) {
	var oracle programStatusOracle
	data, err := testutil.ReadGoldenFile("testdata/oracle/programstatus.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}

	encoder7501 := &Osc7501ProgramStatusEncoder{}
	encoder9 := &Osc9ProgressEncoder{}
	for i, tc := range oracle.Encode {
		status, err := NewProgramStatus(tc.State, tc.Message)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		status = status.WithProgress(tc.Progress)
		if tc.Kind != nil {
			status = status.WithProtocolOption(Osc7501Protocol, "kind", tc.Kind)
		}
		if tc.Osc7501 != nil && encoder7501.Encode(status) != *tc.Osc7501 {
			t.Errorf("case %d (7501): got %q, want %q", i, encoder7501.Encode(status), *tc.Osc7501)
		}
		if tc.Osc9 != nil && encoder9.Encode(status) != *tc.Osc9 {
			t.Errorf("case %d (9;4): got %q, want %q", i, encoder9.Encode(status), *tc.Osc9)
		}
		if tc.Start7501 != nil {
			got := encoder7501.EncodeStart()
			if got == nil || *got != *tc.Start7501 {
				t.Errorf("case %d: start 7501 = %v, want %q", i, got, *tc.Start7501)
			}
		}
		if tc.Start9 != nil {
			t.Errorf("case %d: start 9;4 = %v, want null", i, encoder9.EncodeStart())
		}
	}
}

// TestProgramStatusOracle_Policy compares TerminalStatusPolicy's protocol
// selection with Composer's for every environment of the golden.
func TestProgramStatusOracle_Policy(t *testing.T) {
	var oracle programStatusOracle
	data, err := testutil.ReadGoldenFile("testdata/oracle/programstatus.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}

	for i, tc := range oracle.Policy {
		policy := NewTerminalStatusPolicy(tc.Environment)
		var protocols []string
		for _, encoder := range policy.Encoders() {
			protocols = append(protocols, encoder.Command())
		}
		if !slices.Equal(protocols, tc.Protocols) {
			t.Errorf("case %d (%v): protocols = %v, want %v", i, tc.Environment, protocols, tc.Protocols)
		}
	}
}
