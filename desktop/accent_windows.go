//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// readAccent takes the accent picked under Personalisation, which Windows
// keeps in the DWM key for the title bars and the Start menu.
func readAccent() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\DWM`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AccentColor")
	if err != nil {
		return ""
	}
	return fromABGR(uint32(v))
}
