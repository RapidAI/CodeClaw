package freeproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	dllCrypt32  = syscall.NewLazyDLL("crypt32.dll")
	dllKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procDecryptData = dllCrypt32.NewProc("CryptUnprotectData")
	procLocalFree   = dllKernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func dpapi(data []byte) ([]byte, error) {
	var inBlob dataBlob
	inBlob.cbData = uint32(len(data))
	inBlob.pbData = &data[0]

	var outBlob dataBlob
	r, _, err := procDecryptData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	out := make([]byte, outBlob.cbData)
	copy(out, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	return out, nil
}

// getAESKey reads the Chrome "Local State" file and extracts the AES key
// used for v10/v20 cookie encryption. The key itself is DPAPI-encrypted.
func getAESKey(profileDir string) ([]byte, error) {
	// Local State is in the parent of the profile dir (user-data-dir root)
	localStatePath := filepath.Join(profileDir, "Local State")
	data, err := os.ReadFile(localStatePath)
	if err != nil {
		return nil, err
	}
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.OSCrypt.EncryptedKey == "" {
		return nil, fmt.Errorf("no encrypted_key in Local State")
	}
	encKey, err := base64.StdEncoding.DecodeString(state.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	// Strip "DPAPI" prefix (5 bytes)
	if len(encKey) < 5 {
		return nil, fmt.Errorf("encrypted_key too short")
	}
	return dpapi(encKey[5:])
}

// decryptCookieValue decrypts a Chrome encrypted_value blob.
// profileDir is the user-data-dir (parent of "Default").
func decryptCookieValue(encrypted []byte, profileDir string) (string, error) {
	if len(encrypted) == 0 {
		return "", nil
	}

	// v10/v20 prefix: AES-256-GCM with key from Local State
	if len(encrypted) > 3 && (string(encrypted[:3]) == "v10" || string(encrypted[:3]) == "v20") {
		key, err := getAESKey(profileDir)
		if err != nil {
			return "", err
		}
		encrypted = encrypted[3:] // strip version prefix
		if len(encrypted) < 12+16 {
			return "", fmt.Errorf("ciphertext too short")
		}
		nonce := encrypted[:12]
		ciphertext := encrypted[12:]

		block, err := aes.NewCipher(key)
		if err != nil {
			return "", err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", err
		}
		plain, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return "", err
		}
		// Chrome prepends 32 bytes of padding/metadata to the actual cookie value.
		// Skip this prefix to get the real value.
		if len(plain) > 32 {
			plain = plain[32:]
		}
		return string(plain), nil
	}

	// Legacy DPAPI-only encryption (no version prefix)
	plain, err := dpapi(encrypted)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
