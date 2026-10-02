package app

// multihostersLeftOut are multihosters JD lists that the hoster picker leaves
// out, since KnightLoader has no client of its own for them and routes no
// debrid service through JD. Every other multihoster JD lists is a debrid
// service in the catalogue, which the picker leaves out as well.
//
// Most of them are out of service: the debridplanet.com domain is parked,
// simply-debrid.com has its API switched off, multivip.net does not answer,
// and the dailyleech.com pages JD logs in through are gone. leechall.io still
// works but has no public API, and its login asks for an hCaptcha.
//
// Entries are bare lower-case hostnames as JD names them, without www.
var multihostersLeftOut = map[string]bool{
	"dailyleech.com":    true,
	"debridplanet.com":  true,
	"leechall.io":       true,
	"multivip.net":      true,
	"simply-debrid.com": true,
}
