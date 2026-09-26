package execx

// Two things work differently on Windows.
//
// There is no process group to kill, so a run goes into a job object, and the
// end of the context ends the job: everything the program started, not only
// the program.
//
// A batch file is run by cmd.exe, which reads the command line it is given the
// way a shell would. "a&b" is two commands to it and %PATH% is expanded, and
// the quoting the Go runtime does is for an ordinary program's argument parser,
// not for cmd.exe (https://flatt.tech/research/posts/batbadbut-you-cant-securely-execute-commands-on-windows/).
// A download called "x&del *&.mkv" handed to a .bat as %%name%% would be run.
// So a batch file is given to cmd.exe directly, with a command line quoted for
// cmd.exe, following what Rust's standard library does since CVE-2024-24576
// and CVE-2024-43402.

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// run runs cmd to the end inside a job object that the end of cmd's context
// terminates, with a batch file handed to cmd.exe.
func run(cmd *exec.Cmd) error {
	if cmd.Err == nil {
		// Windows drops dots and spaces from the end of a file name, so
		// "hook.bat. ." opens hook.bat and CreateProcess hands it to cmd.exe.
		// The extension is read off the name Windows resolves the path to.
		script, err := windows.FullPath(cmd.Path)
		if err != nil {
			return err
		}
		if isBatchFile(script) {
			if err := throughCmd(cmd, script); err != nil {
				return err
			}
		}
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	if err := cmd.Start(); err != nil {
		return err
	}
	// Only a child started between Start and this call escapes the job, and
	// no program is that quick. If the assignment fails, os/exec still kills
	// the program itself once WaitDelay has passed.
	if proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)); err == nil {
		_ = windows.AssignProcessToJobObject(job, proc)
		windows.CloseHandle(proc)
	}
	return cmd.Wait()
}

func isBatchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".bat" || ext == ".cmd"
}

// throughCmd makes cmd start cmd.exe with script and the arguments cmd holds
// as one command line of its own making. The arguments in cmd.Args are ignored
// once SysProcAttr.CmdLine is set.
func throughCmd(cmd *exec.Cmd, script string) error {
	line, err := batchCommandLine(script, cmd.Args[1:])
	if err != nil {
		return err
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	cmd.Path = filepath.Join(system, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return nil
}

// batchCommandLine is the command line cmd.exe runs script from. /c strips one
// pair of quotes from what follows it, hence the pair around everything. /d
// skips the AutoRun commands in the registry, /e:ON is what %cd:~,% below
// needs, and /v:OFF keeps an exclamation mark plain.
//
// A line break cannot be passed: cmd.exe ends the command there, and the rest
// of the argument would run as a command of its own.
func batchCommandLine(script string, args []string) (string, error) {
	var b strings.Builder
	b.WriteString(`cmd.exe /d /e:ON /v:OFF /c ""`)
	b.WriteString(script)
	b.WriteByte('"')
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return "", errors.New("a batch file cannot be given an argument with a line break in it")
		}
		b.WriteByte(' ')
		writeBatchArg(&b, a)
	}
	b.WriteByte('"')
	return b.String(), nil
}

// writeBatchArg writes one argument so that cmd.exe hands it on as text.
//
// Inside quotes cmd.exe leaves & | < > ^ ( ) alone, so anything but a plain
// word is quoted. A quote in the argument is doubled, which ends the quoting
// and starts it again at once. A percent sign is written as %%cd:~,%: the
// second half is an empty slice of a variable that always exists, so the sign
// stays and no %name% can form around it. Backslashes before a quote are
// doubled for the program the batch file may pass the argument on to, and a
// trailing backslash is quoted so it cannot swallow the closing quote in a
// batch file that writes "%~1".
func writeBatchArg(b *strings.Builder, a string) {
	quote := a == "" || strings.HasSuffix(a, `\`) || strings.IndexFunc(a, notPlain) >= 0
	if quote {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(a); i++ {
		c := a[i]
		switch c {
		case '\\':
			slashes++
			b.WriteByte(c)
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes))
			b.WriteByte('"')
		case '%':
			b.WriteString("%%cd:~,")
		}
		slashes = 0
		b.WriteByte(c)
	}
	if quote {
		b.WriteString(strings.Repeat(`\`, slashes))
		b.WriteByte('"')
	}
}

// notPlain reports whether a character needs quotes around it for cmd.exe.
// Letters, digits and a few signs of paths and flags do not; everything else,
// every character outside ASCII included, is quoted.
func notPlain(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune(`#$*+-./:?@\_`, r)
	}
}
