package eventprog

// What one event becomes for a program: the KL_* variables in its environment
// and the placeholders in its arguments, both filled from one Values so the two
// cannot disagree about a file name.

import (
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// The variables a program finds the event in. Every one is set on every run,
// empty where the event has nothing to say, so a script can read them without
// first asking whether they exist.
const (
	EnvEvent    = "KL_EVENT"
	EnvTaskID   = "KL_TASK_ID"
	EnvName     = "KL_NAME"
	EnvFile     = "KL_FILE"
	EnvFolder   = "KL_FOLDER"
	EnvPackage  = "KL_PACKAGE"
	EnvCategory = "KL_CATEGORY"
	// EnvExtractOK is "true" or "false" for extract.done, which fires when an
	// unpacking failed as well, and empty for every other event.
	EnvExtractOK = "KL_EXTRACT_OK"
)

// envPrefix is what this instance's own configuration variables start with.
// KL_TORBOX and its neighbours are service keys, so none of them is handed down
// to a program, and none can shadow a variable of the event's.
const envPrefix = "KL_"

// Where is what only the app can say about an event: where its file is on disk
// and which category it was filed under. The firing carries neither, and adding
// them to script's views would change the globals every user script sees.
type Where struct {
	// File is the download's file as it is on disk when the program starts.
	File string
	// Folder is the file's folder for a download, the destination for a
	// package, and the target folder for an unpacked archive.
	Folder string
	// Category is the category's name, or its ID when it has none.
	Category string
}

// Values is one event in the shape the variables and the placeholders share.
type Values struct {
	Event     string
	TaskID    string
	Name      string
	File      string
	Folder    string
	Package   string
	Category  string
	ExtractOK string
}

// ValuesOf reads what the firing carries and takes the rest from w.
//
// Name is the thing the event is about: the archive for an unpacking, the
// package for a finished package, and otherwise the download.
func ValuesOf(f script.Firing, w Where) Values {
	v := Values{Event: string(f.Trigger), File: w.File, Folder: w.Folder, Category: w.Category}
	switch {
	case f.Extract != nil:
		v.Name, v.Package = f.Extract.Name, f.Extract.Package
		v.ExtractOK = strconv.FormatBool(f.Extract.OK)
	case f.Package != nil:
		v.Name, v.Package = f.Package.Name, f.Package.Name
	}
	if f.Task != nil {
		v.TaskID = f.Task.ID
		if v.Name == "" {
			v.Name = f.Task.Name
		}
		if v.Package == "" {
			v.Package = f.Task.Package
		}
	}
	return v
}

// Environ is base without this instance's own KL_* variables, followed by the
// event's.
//
// The prefix is compared without regard to case, because Windows looks up
// environment names that way and a leftover "kl_torbox" would reach the program
// there.
func Environ(base []string, v Values) []string {
	out := make([]string, 0, len(base)+8)
	for _, kv := range base {
		if len(kv) >= len(envPrefix) && strings.EqualFold(kv[:len(envPrefix)], envPrefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		EnvEvent+"="+v.Event,
		EnvTaskID+"="+v.TaskID,
		EnvName+"="+v.Name,
		EnvFile+"="+v.File,
		EnvFolder+"="+v.Folder,
		EnvPackage+"="+v.Package,
		EnvCategory+"="+v.Category,
		EnvExtractOK+"="+v.ExtractOK,
	)
}

// ownNames is the placeholders a program has that an event target does not,
// one per variable the event targets' table has no name for. %%event%%,
// %%task.id%% and %%extract.ok%% are the table's own and mean the same there
// as KL_EVENT, KL_TASK_ID and KL_EXTRACT_OK here. Args and Placeholders both read this list, so the picker
// cannot offer a name the expander does not fill in.
var ownNames = []struct {
	name  string
	value func(Values) string
}{
	{"name", func(v Values) string { return v.Name }},
	{"file", func(v Values) string { return v.File }},
	{"folder", func(v Values) string { return v.Folder }},
	{"package", func(v Values) string { return v.Package }},
	{"category", func(v Values) string { return v.Category }},
}

// Args is the argument list with its placeholders filled in, each argument on
// its own. Nothing is split, joined or quoted: without a shell there is nobody
// to read a quote, so a value goes in as it is and stays one argument.
func Args(cmd idleaction.CommandSpec, f script.Firing, v Values, instanceName string) []string {
	if len(cmd.Args) == 0 {
		return nil
	}
	own := make(map[string]string, len(ownNames))
	for _, n := range ownNames {
		own[n.name] = n.value(v)
	}
	out := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		out[i] = notify.ExpandWith(a, notify.SlotArg, f, instanceName, own)
	}
	return out
}

// Placeholders is the picker's list: this package's own names first, then the
// event targets' table, which Args also reads.
func Placeholders() []notify.Placeholder {
	out := make([]notify.Placeholder, 0, len(ownNames))
	for _, n := range ownNames {
		out = append(out, notify.Placeholder{Name: n.name, Scope: notify.ScopeAlways})
	}
	return append(out, notify.Placeholders()...)
}
