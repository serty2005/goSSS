//go:build windows

package shutdown

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"etalon-agent/internal/iikosyrverms/domain"

	"golang.org/x/sys/windows"
)

const wmClose = 0x0010

var (
	ErrProcessNotFound = errors.New("процесс фронта не найден")
	ErrWindowNotFound  = errors.New("видимые окна фронта не найдены")

	modUser32                    = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = modUser32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = modUser32.NewProc("IsWindowVisible")
	procPostMessageW             = modUser32.NewProc("PostMessageW")
)

type Controller struct {
	tasklistOutput func(context.Context) ([]byte, error)
	sendClose      func(uint32) (int, error)
}

func New() *Controller {
	return &Controller{
		tasklistOutput: tasklistCSV,
		sendClose:      sendCloseToVisibleWindows,
	}
}

func (c *Controller) SoftShutdown(ctx context.Context, softwareType domain.SoftwareType, processName string) (domain.ShutdownResult, error) {
	processName = strings.TrimSpace(processName)
	result := domain.ShutdownResult{
		SoftwareType: softwareType,
		ProcessName:  processName,
	}

	pids, err := findPIDsByProcessPrefix(ctx, c.tasklistOutput, processName)
	if err != nil {
		return result, err
	}
	if len(pids) == 0 {
		return result, ErrProcessNotFound
	}

	result.MatchedPIDs = pids
	for _, pid := range pids {
		count, err := c.sendClose(pid)
		if err != nil {
			return result, err
		}
		result.WindowsClosed += count
	}
	result.CloseSent = result.WindowsClosed > 0
	if !result.CloseSent {
		return result, ErrWindowNotFound
	}
	return result, nil
}

func findPIDsByProcessPrefix(ctx context.Context, tasklistOutput func(context.Context) ([]byte, error), name string) ([]uint32, error) {
	output, err := tasklistOutput(ctx)
	if err != nil {
		return nil, err
	}

	return parseTasklistCSV(output, name)
}

func parseTasklistCSV(raw []byte, name string) ([]uint32, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1

	prefix := strings.ToLower(strings.TrimSpace(name))
	if prefix == "" {
		return nil, fmt.Errorf("не задано имя процесса фронта")
	}

	result := make([]uint32, 0, 2)
	seen := make(map[uint32]struct{})
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, csv.ErrFieldCount) {
				continue
			}
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if err.Error() == "EOF" {
				break
			}
			if errors.Is(err, windows.ERROR_HANDLE_EOF) {
				break
			}
			if strings.Contains(strings.ToLower(err.Error()), "eof") {
				break
			}
			return nil, err
		}
		if len(record) < 2 {
			continue
		}

		imageName := strings.ToLower(strings.TrimSpace(record[0]))
		if !strings.HasPrefix(imageName, prefix) {
			continue
		}

		pid, err := strconv.ParseUint(strings.TrimSpace(record[1]), 10, 32)
		if err != nil {
			continue
		}
		value := uint32(pid)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func tasklistCSV(ctx context.Context) ([]byte, error) {
	command := exec.CommandContext(ctx, "tasklist", "/NH", "/FO", "CSV")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список процессов через tasklist: %w", err)
	}
	return output, nil
}

func sendCloseToVisibleWindows(pid uint32) (int, error) {
	type enumContext struct {
		pid   uint32
		count int
	}

	state := &enumContext{pid: pid}
	callback := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		context := (*enumContext)(unsafe.Pointer(lParam))

		var windowPID uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		if windowPID != context.pid {
			return 1
		}

		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}

		result, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0)
		if result != 0 {
			context.count++
		}
		return 1
	})

	result, _, callErr := procEnumWindows.Call(callback, uintptr(unsafe.Pointer(state)))
	if result == 0 {
		return 0, fmt.Errorf("не удалось перечислить окна процесса %d: %w", pid, callErr)
	}
	return state.count, nil
}
