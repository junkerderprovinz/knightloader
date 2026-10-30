package hosterauth

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// JD answers trafficLeft 0 for an account with nothing to meter as well, so
// only a plan that states a quota and has none of it left counts as spent.
func TestTrafficSpentNeedsAQuotaWithNothingLeft(t *testing.T) {
	for _, c := range []struct {
		name      string
		left, max int64
		valid     bool
		wantSpent bool
	}{
		{name: "quota used up", left: 0, max: 50 << 30, valid: true, wantSpent: true},
		{name: "traffic left", left: 5 << 30, max: 50 << 30, valid: true},
		{name: "nothing metered", valid: true},
		{name: "login not confirmed", left: 0, max: 50 << 30},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeJD{accounts: []jdAccount{{UUID: 1, Hostname: "rapidgator.net",
				InfoMap: &jdAccountInfo{Username: "u", Valid: c.valid, TrafficLeft: c.left, TrafficMax: c.max}}}}
			r, store := newTestReconciler(t, fake)
			t.Cleanup(func() { jdresolver.SetHostActive("rapidgator.net", false) })
			if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, host := range []string{"rapidgator.net", "rg.to"} {
				if got := r.TrafficSpent(host); got != c.wantSpent {
					t.Errorf("TrafficSpent(%q) = %v, want %v", host, got, c.wantSpent)
				}
			}
			if r.TrafficSpent("ddownload.com") {
				t.Error("a hoster with no login reads as spent")
			}
		})
	}
}
