package resolver

import "fmt"

const (
	kib = 1024
	mib = 1024 * kib
	gib = 1024 * mib
	tib = 1024 * gib
)

// FormatBytes converts a byte count to a human-readable string with the
// appropriate binary unit (B, KiB, MiB, GiB, TiB).
func FormatBytes(b uint64) string {
	switch {
	case b >= tib:
		return fmt.Sprintf("%.1f TiB", float64(b)/float64(tib))
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(mib))
	case b >= kib:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatRate converts a bytes-per-second rate to a human-readable string.
func FormatRate(bps float64) string {
	switch {
	case bps >= float64(gib):
		return fmt.Sprintf("%.2f GB/s", bps/float64(gib))
	case bps >= float64(mib):
		return fmt.Sprintf("%.1f MB/s", bps/float64(mib))
	case bps >= float64(kib):
		return fmt.Sprintf("%.1f KB/s", bps/float64(kib))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// FormatPercent formats a float64 percentage with one decimal place.
func FormatPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}

// FormatTemp formats a temperature in Celsius.
func FormatTemp(celsius float64) string {
	return fmt.Sprintf("%.0f°C", celsius)
}
