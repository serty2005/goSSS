//go:build windows

package elevation

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestEnsureAdminSkipsRelaunchWhenAlreadyElevated(t *testing.T) {
	t.Parallel()

	restore := stubElevationEnv(t)
	defer restore()

	called := false
	isElevated = func() bool { return true }
	runAsAdmin = func(string, string, string) error {
		called = true
		return nil
	}

	relaunched, err := EnsureAdmin()
	if err != nil {
		t.Fatalf("EnsureAdmin завершился ошибкой: %v", err)
	}
	if relaunched {
		t.Fatal("при уже elevated процессе relaunch не нужен")
	}
	if called {
		t.Fatal("runAsAdmin не должен вызываться для elevated процесса")
	}
}

func TestEnsureAdminRelaunchesWithSameArgs(t *testing.T) {
	t.Parallel()

	restore := stubElevationEnv(t)
	defer restore()

	var gotExe string
	var gotArgs string
	var gotCWD string

	isElevated = func() bool { return false }
	executablePath = func() (string, error) { return `C:\agent\etalon-agent.exe`, nil }
	workingDir = func() (string, error) { return `C:\agent`, nil }
	processArgs = func() []string { return []string{"--mode", "debug value"} }
	runAsAdmin = func(exePath, argsLine, cwd string) error {
		gotExe = exePath
		gotArgs = argsLine
		gotCWD = cwd
		return nil
	}

	relaunched, err := EnsureAdmin()
	if err != nil {
		t.Fatalf("EnsureAdmin завершился ошибкой: %v", err)
	}
	if !relaunched {
		t.Fatal("ожидался relaunch с запросом UAC")
	}
	if gotExe != `C:\agent\etalon-agent.exe` {
		t.Fatalf("ожидался путь к exe %q, получено %q", `C:\agent\etalon-agent.exe`, gotExe)
	}
	if gotArgs != windows.ComposeCommandLine([]string{"--mode", "debug value"}) {
		t.Fatalf("ожидалась передача исходных аргументов, получено %q", gotArgs)
	}
	if gotCWD != `C:\agent` {
		t.Fatalf("ожидался рабочий каталог %q, получено %q", `C:\agent`, gotCWD)
	}
}

func TestEnsureAdminUsesExecutableDirWhenWorkingDirUnavailable(t *testing.T) {
	t.Parallel()

	restore := stubElevationEnv(t)
	defer restore()

	var got []string
	isElevated = func() bool { return false }
	executablePath = func() (string, error) { return `C:\agent\bin\etalon-agent.exe`, nil }
	workingDir = func() (string, error) { return "", errors.New("cwd недоступен") }
	processArgs = func() []string { return nil }
	runAsAdmin = func(exePath, argsLine, cwd string) error {
		got = []string{exePath, argsLine, cwd}
		return nil
	}

	relaunched, err := EnsureAdmin()
	if err != nil {
		t.Fatalf("EnsureAdmin завершился ошибкой: %v", err)
	}
	if !relaunched {
		t.Fatal("ожидался relaunch с fallback по каталогу exe")
	}
	want := []string{`C:\agent\bin\etalon-agent.exe`, "", filepath.Dir(`C:\agent\bin\etalon-agent.exe`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ожидались параметры %v, получено %v", want, got)
	}
}

func TestEnsureAdminReturnsReadableErrorWhenUserCancelledUAC(t *testing.T) {
	t.Parallel()

	restore := stubElevationEnv(t)
	defer restore()

	isElevated = func() bool { return false }
	executablePath = func() (string, error) { return `C:\agent\etalon-agent.exe`, nil }
	workingDir = func() (string, error) { return `C:\agent`, nil }
	processArgs = func() []string { return nil }
	runAsAdmin = func(string, string, string) error {
		return windows.ERROR_CANCELLED
	}

	relaunched, err := EnsureAdmin()
	if err == nil {
		t.Fatal("ожидалась ошибка при отмене UAC")
	}
	if relaunched {
		t.Fatal("при отмене UAC relaunch не должен считаться успешным")
	}
	if err.Error() != "пользователь отклонил запрос прав администратора" {
		t.Fatalf("ожидалось понятное сообщение об отмене UAC, получено %q", err.Error())
	}
}

func stubElevationEnv(t *testing.T) func() {
	t.Helper()

	prevIsElevated := isElevated
	prevExecutablePath := executablePath
	prevWorkingDir := workingDir
	prevProcessArgs := processArgs
	prevRunAsAdmin := runAsAdmin

	return func() {
		isElevated = prevIsElevated
		executablePath = prevExecutablePath
		workingDir = prevWorkingDir
		processArgs = prevProcessArgs
		runAsAdmin = prevRunAsAdmin
	}
}
