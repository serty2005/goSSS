//go:build windows

package elevation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/windows"
)

var (
	isElevated = func() bool {
		return windows.GetCurrentProcessToken().IsElevated()
	}
	executablePath = os.Executable
	workingDir     = os.Getwd
	processArgs    = func() []string {
		return slices.Clone(os.Args[1:])
	}
	runAsAdmin = func(exePath, argsLine, cwd string) error {
		verbPtr, err := windows.UTF16PtrFromString("runas")
		if err != nil {
			return fmt.Errorf("не удалось подготовить verb runas: %w", err)
		}
		filePtr, err := windows.UTF16PtrFromString(exePath)
		if err != nil {
			return fmt.Errorf("не удалось подготовить путь к exe: %w", err)
		}

		var argsPtr *uint16
		if strings.TrimSpace(argsLine) != "" {
			argsPtr, err = windows.UTF16PtrFromString(argsLine)
			if err != nil {
				return fmt.Errorf("не удалось подготовить аргументы запуска: %w", err)
			}
		}

		var cwdPtr *uint16
		if strings.TrimSpace(cwd) != "" {
			cwdPtr, err = windows.UTF16PtrFromString(cwd)
			if err != nil {
				return fmt.Errorf("не удалось подготовить рабочий каталог: %w", err)
			}
		}

		if err := windows.ShellExecute(0, verbPtr, filePtr, argsPtr, cwdPtr, windows.SW_NORMAL); err != nil {
			return err
		}
		return nil
	}
)

func EnsureAdmin() (bool, error) {
	if isElevated() {
		return false, nil
	}

	exePath, err := executablePath()
	if err != nil {
		return false, fmt.Errorf("не удалось определить путь текущего exe: %w", err)
	}

	cwd, err := workingDir()
	if err != nil || strings.TrimSpace(cwd) == "" {
		cwd = filepath.Dir(exePath)
	}

	args := processArgs()
	argsLine := ""
	if len(args) > 0 {
		argsLine = windows.ComposeCommandLine(args)
	}

	if err := runAsAdmin(exePath, argsLine, cwd); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return false, errors.New("пользователь отклонил запрос прав администратора")
		}
		return false, fmt.Errorf("не удалось перезапустить агент с правами администратора: %w", err)
	}

	return true, nil
}
