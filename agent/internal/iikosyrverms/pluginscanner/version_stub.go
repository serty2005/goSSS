//go:build !windows

package pluginscanner

import "fmt"

func readDLLVersion(string) (string, error) {
	return "", fmt.Errorf("чтение версии DLL поддерживается только на Windows")
}
