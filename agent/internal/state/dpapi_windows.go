//go:build windows

package state

import (
	"fmt"
	"syscall"
	"unsafe"
)

const cryptProtectLocalMachine = 0x4

var (
	modCrypt32             = syscall.NewLazyDLL("Crypt32.dll")
	modKernel32            = syscall.NewLazyDLL("Kernel32.dll")
	procCryptProtectData   = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modKernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func ProtectMachineScope(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return []byte{}, nil
	}
	in := bytesToBlob(src)
	var out dataBlob
	r1, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		uintptr(cryptProtectLocalMachine),
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobToBytes(out), nil
}

func Unprotect(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return []byte{}, nil
	}
	in := bytesToBlob(src)
	var out dataBlob
	r1, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobToBytes(out), nil
}

func bytesToBlob(src []byte) dataBlob {
	return dataBlob{
		cbData: uint32(len(src)),
		pbData: &src[0],
	}
}

func blobToBytes(b dataBlob) []byte {
	if b.cbData == 0 || b.pbData == nil {
		return []byte{}
	}
	src := unsafe.Slice(b.pbData, b.cbData)
	out := make([]byte, len(src))
	copy(out, src)
	return out
}
