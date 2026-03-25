//go:build windows && !oem_qianxin

package main

import _ "embed"

//go:embed build/windows/icon.ico
var icon []byte
