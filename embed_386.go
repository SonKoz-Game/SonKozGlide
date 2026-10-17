package main

import "embed"

//go:embed resources/x64 resources/x86
var embeddedFiles embed.FS
