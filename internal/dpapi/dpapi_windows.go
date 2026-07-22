//go:build windows

package dpapi

import (
	"fmt"
	"unsafe"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/cred"
	"golang.org/x/sys/windows"
)

// CRYPTPROTECT_UI_FORBIDDEN — never show UI; fail rather than prompt. The default
// scope (no CRYPTPROTECT_LOCAL_MACHINE) is per-USER, so the sealed identity can
// only be unsealed by this Windows user account on this machine — exactly the
// "local-only, never travels" property Model 2c requires.
const cryptprotectUIForbidden = 0x1

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtectData = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	procLocalFree        = kernel32.NewProc("LocalFree")
)

// dataBlob mirrors Windows DATA_BLOB { DWORD cbData; BYTE *pbData; }.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(d []byte) dataBlob {
	if len(d) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(d)), pbData: &d[0]}
}

// bytes copies the blob's memory into a Go slice (the source is OS-owned memory
// that we LocalFree afterwards).
func (b dataBlob) bytes() []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

type dpapiSealer struct{}

func platformSealer() cred.Sealer { return dpapiSealer{} }

func (dpapiSealer) Name() string { return "dpapi" }

func (dpapiSealer) Seal(plaintext []byte) ([]byte, error) {
	in := newBlob(plaintext)
	var out dataBlob
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func (dpapiSealer) Unseal(sealed []byte) ([]byte, error) {
	in := newBlob(sealed)
	var out dataBlob
	r, _, err := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData (wrong user/machine, or corrupt): %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}
