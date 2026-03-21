//go:build windows

package protocol

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

var (
	modVersion                  = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSizeW = modVersion.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = modVersion.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = modVersion.NewProc("VerQueryValueW")
)

func readFileVersion(path string) (string, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("не удалось подготовить путь к MitsuCube.exe: %w", err)
	}

	size, _, callErr := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(pathPtr)), 0)
	if size == 0 {
		if callErr != windows.ERROR_SUCCESS && callErr != nil {
			return "", fmt.Errorf("не удалось получить размер version info: %w", callErr)
		}
		return "", fmt.Errorf("version info для MitsuCube.exe отсутствует")
	}

	buffer := make([]byte, size)
	result, _, callErr := procGetFileVersionInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		size,
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if result == 0 {
		return "", fmt.Errorf("не удалось прочитать version info: %w", callErr)
	}

	rootPtr, err := windows.UTF16PtrFromString(`\`)
	if err != nil {
		return "", fmt.Errorf("не удалось подготовить корневой блок version info: %w", err)
	}

	var valuePtr uintptr
	var valueLen uint32
	result, _, callErr = procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&valuePtr)),
		uintptr(unsafe.Pointer(&valueLen)),
	)
	if result == 0 || valuePtr == 0 || valueLen == 0 {
		return "", fmt.Errorf("не удалось извлечь фиксированный блок version info: %w", callErr)
	}

	version := (*vsFixedFileInfo)(unsafe.Pointer(valuePtr))
	return fmt.Sprintf(
		"%d.%d.%d.%d",
		version.FileVersionMS>>16,
		version.FileVersionMS&0xFFFF,
		version.FileVersionLS>>16,
		version.FileVersionLS&0xFFFF,
	), nil
}
