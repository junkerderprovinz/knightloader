package access

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheResetCodeGoesToTheConsoleAndNotTheLog(t *testing.T) {
	var console bytes.Buffer
	Logger(t.TempDir(), &console).Warn("signin: someone asked to reset the password", "code", "ABCD-EFGH-JKMN")
	if !strings.Contains(console.String(), "Password reset code: ABCD-EFGH-JKMN") {
		t.Errorf("the console reads %q, want the code", console.String())
	}
}

func TestWithoutAConsoleTheResetCodeGoesToAFile(t *testing.T) {
	dir := t.TempDir()
	Logger(dir, nil).Warn("signin: someone asked to reset the password", "code", "ABCD-EFGH-JKMN")
	b, err := os.ReadFile(filepath.Join(dir, ResetCodeFile))
	if err != nil || !strings.Contains(string(b), "ABCD-EFGH-JKMN") {
		t.Fatalf("the code file reads %q, %v", b, err)
	}
	RemoveResetCode(dir)
	if _, err := os.Stat(filepath.Join(dir, ResetCodeFile)); !os.IsNotExist(err) {
		t.Error("RemoveResetCode left the file")
	}
}
