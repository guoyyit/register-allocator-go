package benchmarks

import _ "embed"

//go:embed benchdata/high_pressure.ir
var HighPressure string

//go:embed benchdata/call_live.ir
var CallLive string

//go:embed benchdata/branches.ir
var Branches string

// Programs returns the named benchmark programs.
func Programs() map[string]string {
	return map[string]string{
		"high_pressure": HighPressure,
		"call_live":     CallLive,
		"branches":      Branches,
	}
}

// SpillLimit is the enforced upper bound on spill instructions
// for a benchmark at a given physical register count.
type SpillLimit struct {
	Name        string
	NumPhysical int
	MaxSpills   int
}

// Limits returns enforced spill-count limits.
func Limits() []SpillLimit {
	return []SpillLimit{
		{Name: "high_pressure", NumPhysical: 4, MaxSpills: 16},
		{Name: "call_live", NumPhysical: 4, MaxSpills: 14},
		{Name: "branches", NumPhysical: 4, MaxSpills: 20},
	}
}
