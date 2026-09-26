# Where files land

The download folder may be a plain path or a template:

```
/downloads/<jd:date>/<jd:hoster>/<jd:packagename>
```

Available: `<jd:packagename>`, `<jd:hoster>`, `<jd:filename>`, `<jd:date>`,
`<jd:year>`, `<jd:month>`, `<jd:day>` and `<jd:simpledate:FORMAT>` with the
usual `yyyy MM dd HH mm ss SSS` pattern letters. A task can also carry its own
folder, which always wins, and a Packagizer rule can set one per link, which is
how one paste lands in several places.

## Two variables that differ from JDownloader

The variables menu says so next to each. `<jd:source:N>` here is the **Nth path
segment of the source URL**, which is what a rule with no regular expression in
it can use. JDownloader's meaning, capture group N, is `<jd:match:FIELD:N>`,
which reads a group from any field the rule matched on, not only the source. A
rule naming a group on a field it has no `matches` condition for is refused when
you save it, rather than quietly producing the wrong folder.

## Renaming a package

When the download folder is named after the package through
`<jd:packagename>`, renaming the package renames the folder on disk too, with
everything already in it: finished files, partial ones and their share of the
working folder. Downloads still running stop while the folder moves, then carry
on in the new one from the bytes they already have, whichever backend fetches
them. JDownloader is told the package's new folder and picks up where it
stopped. A torrent that is seeding goes on seeding from the new place, but its
seeding goal, a ratio or a time, starts counting again. A folder of unpacked
files named after the package, from "A folder per package" or a template under
"Unpack to", moves along with it.

A link with a folder of its own, set by hand or by a Packagizer rule, stays
where it is. So does a folder whose name does not come from the package.

If a folder of the new name already holds something, the Name conflicts
setting decides (or the category's own, where it has one). "Keep both" gives
the folder a numbered name such as `New name (2)`. "Skip" leaves the files in
the old folder and renames only the package. "Overwrite" turns the rename down,
because a folder is never overwritten. The rename is also turned down while
part of the package is being unpacked, moved into its folder or recorded from a
live stream, and when the folder cannot be renamed, for example because a
program on Windows has a file in it open. Nothing changes in either case. A
folder that moves to another disk is copied there.

## Telling a media library to rescan

Once the last file of a package has arrived **and been moved into its folder**,
one stored address can be called: a media library told to rescan. It is set up
on the Automation page (an address, GET or POST, and one header whose value is
sealed in the same encrypted store as your account logins) and switched on per
category, so only the drawers you pick call anything. Nothing is called until
you do both.

## Starting a program of your own

The Automation page can start a program when something happens, the way
pyLoad's external scripts do: a script that files a finished download, say, or
one that tells another machine a package is complete. Each row names the
program, its arguments one per line, and the events that start it. Nothing runs
until you switch the row on.

The program is started directly, not through a shell. Placeholders in the
arguments are filled in first, and each line reaches the program as exactly one
argument, so a file name with a space, a quote, `$(...)` or `;` in it arrives as
those characters and is never run. On Windows a `.bat` or `.cmd` file is run
by cmd.exe, which would read those characters, so each argument reaches the
script in quotes and stays text; read it there as `"%~1"`. An argument with a
line break cannot be passed to a batch file at all, and that run fails; read
such a value from the `KL_` variables below instead. Every run also gets the
event in its environment:

| Variable | Placeholder | Holds |
|---|---|---|
| `KL_EVENT` | `%%event%%` | the event, such as `task.done`, `task.failed`, `package.done` or `extract.done` |
| `KL_TASK_ID` | `%%task.id%%` | the download's id, empty for an event about no single download |
| `KL_NAME` | `%%name%%` | the archive for an unpacking, the package for a finished package, otherwise the download |
| `KL_FILE` | `%%file%%` | the download's file, where it is when the program starts |
| `KL_FOLDER` | `%%folder%%` | the file's folder; for a finished package the folder most of its files went to, for an unpacked archive the folder it was unpacked into |
| `KL_PACKAGE` | `%%package%%` | the package name |
| `KL_CATEGORY` | `%%category%%` | the category's name |
| `KL_EXTRACT_OK` | `%%extract.ok%%` | `true` or `false` for an unpacking, which starts the program when it failed too; empty for every other event |

If you need a shell, make the shell the program, for example `/bin/sh` with
`-c` and your command line as the next argument, and read the variables there
in double quotes, such as `"$KL_FILE"`. A placeholder pasted into a shell's
command line would be read by that shell.

With a working folder set, a program started on a finished download or package
waits until the files have been checked and moved out of the working folder,
so it finds them at their destination. If a move fails, the program starts
after 15 minutes anyway and gets the file where it is. KnightLoader's own `KL_`
settings, service keys included, are never passed on to a program.

A run that takes longer than its row allows is stopped, and so is anything the
program started. A program runs one at a time unless its row allows more, up to
four, so by default it sees events in the order they happened. Switching Event
programs off on the Modules page drops the runs still waiting; one under way
finishes. The exit code and the first part of the output go into
the log, and the Automation page shows the last run. Once saved, the program's
path and arguments are not shown there again, because an argument can hold a
token, and the log names the row rather than the program.
