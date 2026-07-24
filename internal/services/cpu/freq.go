package cpu

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CoreFreq holds frequency and governor for a single CPU core.
type CoreFreq struct {
	FreqMHz  float64 // e.g. 3200.0
	Governor string  // e.g. "powersave", "performance", "schedutil"
}

// readCoreFreqs reads scaling frequency and governor for each CPU core from sysfs.
// Returns a slice matching core count. Defaults to 0/empty if cpufreq is unsupported (VMs/containers).
func readCoreFreqs(coreCount int) []CoreFreq {
	freqs := make([]CoreFreq, coreCount)
	for i := 0; i < coreCount; i++ {
		base := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq", i)

		// Read frequency in kHz -> convert to MHz
		if data, err := os.ReadFile(base + "/scaling_cur_freq"); err == nil {
			if khz, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				freqs[i].FreqMHz = khz / 1000.0
			}
		}

		// Read governor
		if data, err := os.ReadFile(base + "/scaling_governor"); err == nil {
			freqs[i].Governor = strings.TrimSpace(string(data))
		}
	}
	return freqs
}
