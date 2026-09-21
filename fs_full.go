//go:build !lite

package main

import (
	"embed"
)

//go:embed web/build/*
var buildFS embed.FS

// liteExcludedFeatures 标准构建下没有任何裁剪
func liteExcludedFeatures() []string {
	return nil
}
