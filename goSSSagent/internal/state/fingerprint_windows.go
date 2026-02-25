//go:build windows

package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func ComputeMachineFingerprintHash() (string, error) {
	machineGuid, err := readMachineGuid()
	if err != nil {
		return "", err
	}
	raw := strings.Join([]string{
		"machine_guid=" + strings.TrimSpace(machineGuid),
		"os=" + runtime.GOOS,
		"arch=" + runtime.GOARCH,
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), nil
}

func readMachineGuid() (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.READ)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть ключ MachineGuid: %w", err)
	}
	defer key.Close()
	v, _, err := key.GetStringValue("MachineGuid")
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать MachineGuid: %w", err)
	}
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("MachineGuid пустой")
	}
	return v, nil
}
