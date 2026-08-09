package reconnect

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// scriptFileMode is what the script is written with. It is readable only by the
// user the process runs as, because the expanded script contains the router
// password: everything else in this package keeps the password out of logs and
// errors, and a world-readable file in the temporary directory would give it all
// away in one line.
const scriptFileMode = 0o600

// script runs the user's script by handing it to their interpreter.
//
// There is no shell in this path and no command line is ever assembled from
// strings. A reconnect script holds the router password, and building
// `sh -c "..."` around it means one unquoted character turns the password into
// an argument list - or, on a password with a backtick in it, into a command.
// The script goes into a file and the interpreter is given the file's path,
// which is the same injected Runner the command method uses and has no quoting
// in it at all.
func (r *Reconnector) script(ctx context.Context, cfg Config, vars map[string]string) error {
	dir, err := os.MkdirTemp(r.tempDir, "knightloader-reconnect-")
	if err != nil {
		return fmt.Errorf("reconnect: the script could not be written: %w", err)
	}
	// The directory goes away whatever happens, including a run that fails or a
	// context that was cancelled halfway. A leftover file here is the router
	// password sitting in the temporary directory until the next reboot.
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "reconnect"+scriptSuffix(cfg.Interpreter))
	// The script is never marked executable and is never started on its own:
	// the interpreter is what reads it. That also means the method works on a
	// temporary directory mounted noexec, which is a common hardening choice and
	// would otherwise break the feature with a permission error nobody can place.
	if err := os.WriteFile(file, []byte(expandVars(cfg.Script, vars)), scriptFileMode); err != nil {
		return fmt.Errorf("reconnect: the script could not be written: %w", err)
	}

	args := make([]string, 0, len(cfg.InterpreterArgs)+1)
	for _, a := range cfg.InterpreterArgs {
		args = append(args, expandVars(a, vars))
	}
	// The path goes last, after whatever flags the interpreter needs, because
	// that is where every interpreter expects it - and passing it first would
	// make PowerShell's -File read the flag as the script's own argument.
	args = append(args, file)

	if err := r.run(ctx, cfg.Interpreter, args...); err != nil {
		// The interpreter and the failure, never the script's text. The user's
		// own script is the one place a password can be hard-coded, and this
		// package's redaction only knows the one in the password field.
		return fmt.Errorf("reconnect: %s: %w", cfg.Interpreter, err)
	}
	return nil
}

// scriptSuffixes maps an interpreter to the file extension it insists on.
//
// Most interpreters do not care what the file is called, and for those the table
// has nothing and the file has no extension. PowerShell is the reason the table
// exists: it refuses to run a file that is not named .ps1, with an error about
// the extension that reads like a policy problem. Windows' own cmd is the same.
var scriptSuffixes = map[string]string{
	"powershell":     ".ps1",
	"powershell.exe": ".ps1",
	"pwsh":           ".ps1",
	"pwsh.exe":       ".ps1",
	"cmd":            ".bat",
	"cmd.exe":        ".bat",
}

// scriptSuffix picks the extension for an interpreter given as a bare name or as
// a full path, in either path syntax - the settings are edited on one machine
// and may well be restored on another.
func scriptSuffix(interpreter string) string {
	base := strings.ToLower(strings.TrimSpace(interpreter))
	base = path.Base(filepath.ToSlash(base))
	return scriptSuffixes[base]
}
