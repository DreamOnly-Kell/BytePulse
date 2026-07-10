//go:build !darwin && !windows

// Stub traffic attribution for platforms without a backend (e.g. Linux).
// 无流量后端的平台桩（例如 Linux）。
package proctraffic

import "fmt"

// NewNettopAttributor returns unsupported outside macOS.
// NewNettopAttributor 在 macOS 外返回不支持。
func NewNettopAttributor() Attributor {
	return unsupportedAttributor{}
}

// NewAttributor rejects traffic modes on unsupported platforms (except off).
// NewAttributor 在不支持的平台上拒绝流量模式（off 除外）。
func NewAttributor(mode string) (Attributor, error) {
	switch mode {
	case "off", "":
		return nil, nil
	case "auto", "on", "nettop", "estats":
		return nil, fmt.Errorf("process traffic mode %q is not supported on this platform", mode)
	default:
		return nil, fmt.Errorf("unsupported process traffic mode %q; use off", mode)
	}
}

func nettopArgs() []string {
	return nil
}
