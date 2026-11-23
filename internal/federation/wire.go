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
	// RelayKeyAccount held a relay key typed in by hand, for a group without
	// a phrase. It is read once more, to drop it.
	RelayKeyAccount = "relay"
	// MemberAccount holds the seed of this instance's member key. It belongs
	// to the group: joining one mints a new seed and leaving deletes it.
	MemberAccount = "relay-member"
	// GroupAccount holds what kit/group keeps of the group: who is approved,
	// who was taken out and where the members answered.
	GroupAccount = "relay-group"
	// FortressAccount holds the secret behind the fortress phrase. Once it is
	// set, the pairing lives in the fortress under the same phrase, and
	// SeedAccount holds the old one only until every member followed.
	FortressAccount = "fortress-secret"
	// FortressPreviousAccount holds the fortress secret before the phrase
	// last changed, which members, removals and permissions carry over from.
	FortressPreviousAccount = "fortress-previous"
	// FortressPreviousSinceAccount holds when, in Unix seconds, the phrase
	// changed away from FortressPreviousAccount.
	FortressPreviousSinceAccount = "fortress-previous-since"
	// FortressGroupAccount and FortressPairingAccount hold what kit/group
	// keeps of the fortress and of the pairing in it.
	FortressGroupAccount   = "fortress-group"
	FortressPairingAccount = "fortress-pairing"
	// FortressRemovedAccount holds, as JSON, the members this instance took
	// out of the fortress since the phrase last changed.
	FortressRemovedAccount = "fortress-removed"
	// FortressRemovedHereAccount is set once another member took this
	// instance out, until it joins again.
	FortressRemovedHereAccount = "fortress-removed-here"
)

// LAN is where KnightLoader instances announce themselves on their network,
// and the port a sweep tries on every neighbour.
var LAN = lan.Config{
	Group: netip.AddrPortFrom(netip.AddrFrom4([4]byte{239, 255, 77, 49}), 8750),
	Ports: []int{8749},
}
