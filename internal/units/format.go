// Package units formats byte counts and transfer rates for human display.
// units 包将字节数与传输速率格式化为人类可读文本。
package units

import "fmt"

// byteUnits are IEC-style 1024-based labels for byte quantities.
// byteUnits 是以 1024 为进制的字节量单位标签。
var byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// bitUnits are 1024-based labels when displaying bits instead of bytes.
// bitUnits 是以 bit 显示时使用的 1024 进制单位标签。
var bitUnits = []string{"b", "Kb", "Mb", "Gb", "Tb", "Pb"}

// FormatBytes renders a raw byte count (e.g. "1.25 MB").
// FormatBytes 渲染原始字节数（例如 "1.25 MB"）。
func FormatBytes(v uint64) string {
	// Reuse the shared scaler with no rate suffix.
	// 复用统一缩放逻辑，不加速率后缀。
	return formatFloat(float64(v), byteUnits, "")
}

// FormatRate renders a throughput; bits=true multiplies by 8 and uses bit labels.
// FormatRate 渲染吞吐量；bits=true 时乘以 8 并使用 bit 单位。
func FormatRate(bytesPerSecond float64, bits bool) string {
	// Clamp negative rates (should not happen from counters, but keep UI safe).
	// 钳制负速率（计数器一般不会产生，但保证 UI 安全）。
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	// Convert B/s to bits/s when requested.
	// 需要时把 B/s 转为 bits/s。
	if bits {
		return formatFloat(bytesPerSecond*8, bitUnits, "/s")
	}
	// Default display is bytes per second.
	// 默认显示为每秒字节。
	return formatFloat(bytesPerSecond, byteUnits, "/s")
}

// formatFloat scales v into the largest unit under 1024 and picks precision.
// formatFloat 将 v 缩放到小于 1024 的最大单位，并选择小数精度。
func formatFloat(v float64, labels []string, suffix string) string {
	// Start at the base unit index.
	// 从基础单位下标开始。
	i := 0
	// Promote units while value is large enough and units remain.
	// 在数值足够大且还有更高单位时升级单位。
	for v >= 1024 && i < len(labels)-1 {
		v /= 1024
		i++
	}
	// Whole numbers for bare base units (bytes/bits).
	// 基础单位（字节/比特）使用整数。
	if i == 0 {
		return fmt.Sprintf("%.0f %s%s", v, labels[i], suffix)
	}
	// Large scaled values need no fractional digits.
	// 较大的缩放值不需要小数位。
	if v >= 100 {
		return fmt.Sprintf("%.0f %s%s", v, labels[i], suffix)
	}
	// Medium values keep one decimal place.
	// 中等数值保留一位小数。
	if v >= 10 {
		return fmt.Sprintf("%.1f %s%s", v, labels[i], suffix)
	}
	// Small scaled values keep two decimal places for readability.
	// 较小的缩放值保留两位小数以便阅读。
	return fmt.Sprintf("%.2f %s%s", v, labels[i], suffix)
}
