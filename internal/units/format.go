package units

import "fmt"

var byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}
var bitUnits = []string{"b", "Kb", "Mb", "Gb", "Tb", "Pb"}

func FormatBytes(v uint64) string {
	return formatFloat(float64(v), byteUnits, "")
}

func FormatRate(bytesPerSecond float64, bits bool) string {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	if bits {
		return formatFloat(bytesPerSecond*8, bitUnits, "/s")
	}
	return formatFloat(bytesPerSecond, byteUnits, "/s")
}

func formatFloat(v float64, labels []string, suffix string) string {
	i := 0
	for v >= 1024 && i < len(labels)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s%s", v, labels[i], suffix)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s%s", v, labels[i], suffix)
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f %s%s", v, labels[i], suffix)
	}
	return fmt.Sprintf("%.2f %s%s", v, labels[i], suffix)
}
