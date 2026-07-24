// Package styles — sparkline renderer for dashboard history graphs.
// Uses Unicode block characters ▁▂▃▄▅▆▇█ to render time-series data
// in a compact horizontal strip.
package styles

import (
	"github.com/charmbracelet/lipgloss"
)

// sparkBlocks are the 8 Unicode block elements from shortest to tallest.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline is a fixed-capacity ring buffer that renders as a sparkline.
type Sparkline struct {
	data []float64
	cap  int
	idx  int   // next write position
	len  int   // number of valid entries
	min  float64
	max  float64
	auto bool  // true if max should dynamically adapt to peak value
}

// NewSparkline creates a sparkline buffer with the given capacity.
// min/max set the expected value range (e.g., 0–100 for percentages).
func NewSparkline(capacity int, min, max float64) *Sparkline {
	if capacity < 1 {
		capacity = 60
	}
	autoScale := max <= 0
	if autoScale {
		max = 1.0
	}
	return &Sparkline{
		data: make([]float64, capacity),
		cap:  capacity,
		min:  min,
		max:  max,
		auto: autoScale,
	}
}

// Push adds a new value to the ring buffer.
func (s *Sparkline) Push(v float64) {
	s.data[s.idx] = v
	s.idx = (s.idx + 1) % s.cap
	if s.len < s.cap {
		s.len++
	}
}

// Render draws the sparkline as a styled string with the given width and color.
func (s *Sparkline) Render(width int, color lipgloss.Color) string {
	if s.len == 0 || width <= 0 {
		return ""
	}

	// Get ordered data (oldest to newest).
	ordered := s.ordered()

	// Downsample or pad to fit width.
	values := resample(ordered, width)

	maxVal := s.max
	if s.auto {
		// Find peak in current values
		peak := 0.001
		for _, v := range values {
			if v > peak {
				peak = v
			}
		}
		maxVal = peak
	}

	span := maxVal - s.min
	if span <= 0 {
		span = 1
	}

	style := lipgloss.NewStyle().Foreground(color)
	dimStyle := lipgloss.NewStyle().Foreground(ColorBorder)

	out := make([]byte, 0, width*4)
	for _, v := range values {
		if v < 0 {
			// No data point — render dim lowest block.
			out = append(out, []byte(dimStyle.Render(string(sparkBlocks[0])))...)
			continue
		}
		// Normalize to 0..1 and map to block index.
		norm := (v - s.min) / span
		if norm < 0 {
			norm = 0
		}
		if norm > 1 {
			norm = 1
		}
		idx := int(norm * float64(len(sparkBlocks)-1))
		out = append(out, []byte(style.Render(string(sparkBlocks[idx])))...)
	}

	return string(out)
}

// ordered returns the data in chronological order (oldest first).
func (s *Sparkline) ordered() []float64 {
	result := make([]float64, s.len)
	if s.len < s.cap {
		// Buffer not full yet — data starts at 0.
		copy(result, s.data[:s.len])
	} else {
		// Buffer full — oldest entry is at s.idx.
		copy(result, s.data[s.idx:])
		copy(result[s.cap-s.idx:], s.data[:s.idx])
	}
	return result
}

// resample adjusts the data to fit the target width.
// If data is longer, it downsamples by averaging buckets.
// If shorter, it left-pads with -1 (no data).
func resample(data []float64, width int) []float64 {
	n := len(data)
	if n == 0 {
		result := make([]float64, width)
		for i := range result {
			result[i] = -1
		}
		return result
	}

	if n <= width {
		// Pad left with -1 markers.
		result := make([]float64, width)
		pad := width - n
		for i := 0; i < pad; i++ {
			result[i] = -1
		}
		copy(result[pad:], data)
		return result
	}

	// Downsample: divide data into `width` buckets and average each.
	result := make([]float64, width)
	bucketSize := float64(n) / float64(width)
	for i := 0; i < width; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > n {
			end = n
		}
		sum := 0.0
		for j := start; j < end; j++ {
			sum += data[j]
		}
		result[i] = sum / float64(end-start)
	}
	return result
}
