package federation

import (
	"net/netip"

	"github.com/junkerderprovinz/parleyport/kit/keys"
	"github.com/junkerderprovinz/parleyport/kit/lan"
)

// Domain is what KnightLoader derives every key of its groups under.
const Domain keys.Domain = "knightloader"

// Signing is the labels KnightLoader's members sign their calls and answers
// under. They are older than the kit's own, and every phone and browser in the
// field signs with them.
var Signing = keys.Signing{
	Call:   "knightloader/relay/member-call/v2",
	Result: "knightloader/relay/member-result/v3",
}

// The credential-store services the group's secrets are sealed under.
const (
	// SeedAccount holds the secret behind the phrase. Only Domain.GroupKey of
	// it ever reaches a relay.
	SeedAccount = "relay-seed"
	// RelayKeyAccount holds a relay key typed in by hand, for a group that
	// has no phrase.
	RelayKeyAccount = "relay"
	// MemberAccount holds the seed of this instance's member key. It belongs
	// to the group: joining one mints a new seed and leaving deletes it.
	MemberAccount = "relay-member"
)

// LAN is where KnightLoader instances announce themselves on their network,
// and the port a sweep tries on every neighbour.
var LAN = lan.Config{
	Group: netip.AddrPortFrom(netip.AddrFrom4([4]byte{239, 255, 77, 49}), 8750),
	Ports: []int{8749},
}
