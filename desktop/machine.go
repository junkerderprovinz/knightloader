package main

// product names the installation for all users: its entry under Apps and its
// folder under ProgramData. It is INFO_PRODUCTNAME in
// build/windows/installer/project.nsi.
var product = "KnightLoader"

// machineSettingsFile holds the installed copy's Update automatically switch in
// the folder under ProgramData, which the installer opens to every user.
const machineSettingsFile = "settings.json"
