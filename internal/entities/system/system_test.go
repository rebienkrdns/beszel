package system

import (
	"encoding/json"
	"testing"
)

// TestStatsPressureFieldsOmittedWhenZero guards against a regression where
// json:"...,omitempty" was used on fixed-size array fields (encoding/json's
// omitempty never omits a [N]T array, since its length is always N, never
// zero - only omitzero, Go 1.24+, checks the actual zero value). Without
// omitzero, every stats record would serialize memps/mempf/cpup as [0,0,0]
// even when an agent never collected that data (old agents, non-Linux
// hosts), defeating the frontend's presence check that hides the panel.
// MemAvailable (mav) and OOMKillDelta (okd) are included here too: both are
// scalar omitzero fields with the same "must be absent, not zero, for old
// agents" requirement.
func TestStatsPressureFieldsOmittedWhenZero(t *testing.T) {
	b, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatalf("marshal zero Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd"} {
		if _, present := raw[key]; present {
			t.Errorf("expected %q to be omitted for a zero-value Stats, got: %s", key, raw[key])
		}
	}
}

func TestStatsPressureFieldsPresentWhenNonZero(t *testing.T) {
	s := Stats{
		MemPressureSome: [3]float64{1.1, 2.2, 3.3},
		MemPressureFull: [3]float64{4.4, 5.5, 6.6},
		CpuPressure:     [3]float64{7.7, 8.8, 9.9},
		IOPressureSome:  [3]float64{1.2, 3.4, 5.6},
		IOPressureFull:  [3]float64{7.8, 9.0, 1.2},
		MemAvailable:    5.5,
		OOMKillDelta:    1,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
