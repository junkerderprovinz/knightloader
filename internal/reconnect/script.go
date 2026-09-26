package reconnect

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// scriptFileMode keeps the script readable only by the process's user, since
// the expanded script contains the router password.
const scriptFileMode = 0o600

// script runs the user's script by handing it to their interpreter. No shell
// is involved and no command line is assembled: the password in the script
// could otherwise become extra arguments or, with a backtick, a command. The
// script goes into a file and the interpreter gets its path through the same
// Runner the command method uses.
func (r *Reconnector) script(ctx context.Context, cfg Config, vars map[string]string) error {
	dir, err := os.MkdirTemp(r.tempDir, "knightloader-reconnect-")
	if err != nil {
		return fmt.Errorf("reconnect: the script could not be written: %w", err)
	}
	// Removed whatever happens, so the password never lingers in the
	// temporary directory.
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "reconnect"+scriptSuffix(cfg.Interpreter))
	// Never marked executable, since the interpreter reads it; this also works
	// on a temporary directory mounted noexec.
	if err := os.WriteFile(file, []byte(expandVars(cfg.Script, vars)), scriptFileMode); err != nil {
		return fmt.Errorf("reconnect: the script could not be written: %w", err)
	}

	args := make([]string, 0, len(cfg.InterpreterArgs)+1)
	for _, a := range cfg.InterpreterArgs {
		args = append(args, expandVars(a, vars))
	}
	// The path goes after the interpreter's flags, where PowerShell's -File
	// expects it.
	args = append(args, file)

	if err := r.runFor(ctx, cfg, cfg.Interpreter, args); err != nil {
		// Never the script's text, which may hold a hard-coded password.
		return fmt.Errorf("reconnect: %s: %w", cfg.Interpreter, err)
	}
	return nil
}

// scriptSuffixes maps an interpreter to the file extension it insists on.
// Most interpreters do not care, but PowerShell refuses a file not named .ps1
// and cmd needs .bat.
var scriptSuffixes = map[string]string{
	"powershell":     ".ps1",
	"powershell.exe": ".ps1",
	"pwsh":           ".ps1",
	"pwsh.exe":       ".ps1",
	"cmd":            ".bat",
	"cmd.exe":        ".bat",
}

// scriptSuffix picks the extension for an interpreter given as a bare name or
// a full path in either path syntax, since settings may be restored on another
// machine.
func scriptSuffix(interpreter string) string {
	base := strings.ToLower(strings.TrimSpace(interpreter))
	base = path.Base(filepath.ToSlash(base))
	return scriptSuffixes[base]
}
