//go:build lite

// Lite 构建变体。
//
// 用 `go build -tags lite` 产出面向 OpenWrt / 随身 WiFi 的精简版：
//   - 不嵌入前端控制台（体积直接小十几 MB，运行时也不占内存去 serve 静态资源）
//   - 关闭-,dashboard、聚合统计、MCP 发现等重型后台goroutine
//   - 路由层面裁剪掉 MCP / Memory / Dashboard 一组管理接口
//
// 注意：Lite 版保留完整的中继能力（含智能路由、故障切换、别名、缓存），
// 只是去掉「管理向」的重模块——小内存设备上最稀缺的就是常驻内存。

package main

import (
	"embed"
)

// buildFS 在 Lite 构建下为空占位：没有 //go:embed，因此前端产物不会被链进二进制
//
//go:embed stub.txt
var buildFS embed.FS

// liteFeatures 告知运行时尚未编译进来的能力，避免管理端显示错乱
func liteExcludedFeatures() []string {
	return []string{"dashboard", "mcp", "memory-heavy", "web-console"}
}
