//go:build !cgo

// corelib/audioconv/silk_nocgo.go — Silk v3 stub when CGo is not available.
//
// Silk v3 decoding requires CGo (github.com/git-jiadong/go-silk). When built
// with CGO_ENABLED=0, silk voice messages cannot be decoded and this stub
// returns an error so the caller can fall back to the original data.
package audioconv

import "fmt"

func silkToWAV(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("audioconv: silk decoding unavailable (built without CGo)")
}
