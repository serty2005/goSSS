//go:build !windows

package protocol

import "fmt"

func readFileVersion(string) (string, error) {
	return "", fmt.Errorf("получение версии MitsuCube.exe поддерживается только на Windows")
}
