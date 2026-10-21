package main

// product names the installation for all users: its entry under Apps and its
// folder under ProgramData. It is name in installer/main.go.
var product = "KnightLoader"

// appID names the installation on Linux, the folder under ~/.local/share
// included. It is id in installer/main.go.
var appID = "com.junkerderprovinz.knightloader"

// machineSettingsFile holds the installed copy's Update automatically switch in
// the folder under ProgramData, which the installer opens to every user.
const machineSettingsFile = "settings.json"
