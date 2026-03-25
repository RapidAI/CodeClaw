//go:build linux && !oem_qianxin
// +build linux,!oem_qianxin

package main

import _ "embed"

//go:embed build/appicon.png
var icon []byte
