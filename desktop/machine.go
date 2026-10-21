package main

// product names the installation for all users: its entry under Apps and its
// folder under ProgramData. It is INFO_PRODUCTNAME in
// build/windows/nsis/project.nsi.
var product = "KnightLoader"

// appID names the installation on Linux, the folder under ~/.local/share
// included. It is id in installer/main.go.
var appID = "com.junkerderprovinz.knightloader"

// machineSettingsFile holds the installed copy's Update automatically switch in
// the folder under ProgramData, which the installer opens to every user.
const machineSettingsFile = "settings.json"
