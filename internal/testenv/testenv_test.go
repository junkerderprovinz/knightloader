package testenv

import "testing"

// The gate has to work in BOTH directions, and only one of them is visible
// where it is used: a workstation sees the skips, and nothing on the workstation
// would notice if the variable stopped being read at all. CI would then run the
// full suite, skip the multicast and swarm tests along with everything else, and
// report green for a run that proved less than it used to.
func TestTheGateReadsTheVariableInBothDirections(t *testing.T) {
	t.Setenv(NetTestsEnv, "")
	if wideListenersWanted() {
		t.Errorf("%s empty: want the wide-listening tests skipped", NetTestsEnv)
	}

	t.Setenv(NetTestsEnv, "1")
	if !wideListenersWanted() {
		t.Errorf("%s=1: want the wide-listening tests to run, which is how CI covers them", NetTestsEnv)
	}

	// Any non-empty value, not the literal "1": CI sets a string, a shell
	// exports whatever it was given, and a gate that only accepted one spelling
	// would silently skip for "true", "yes" or "on".
	t.Setenv(NetTestsEnv, "true")
	if !wideListenersWanted() {
		t.Errorf("%s=true: want any non-empty value to count", NetTestsEnv)
	}
}
