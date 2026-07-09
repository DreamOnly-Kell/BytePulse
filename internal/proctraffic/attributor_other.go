//go:build !darwin

// Non-macOS stubs for nettop attribution entry points.
// 非 macOS 平台的 nettop 归因入口桩。
package proctraffic

// NewNettopAttributor returns an unsupported stub outside macOS.
// NewNettopAttributor 在 macOS 外返回不支持的桩。
func NewNettopAttributor() Attributor {
	return unsupportedAttributor{}
}

// nettopArgs is unused off-darwin but kept for API symmetry with the darwin file.
// nettopArgs 在非 darwin 上未使用，仅为与 darwin 文件 API 对称保留。
func nettopArgs() []string {
	return nil
}
