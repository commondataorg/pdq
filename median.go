// Package pdq implements the PDQ perceptual hash algorithm in pure Go.
// PDQ (Perceptual hash for Duplicate Detection Quickly) was developed by
// Meta and produces a 256-bit perceptual hash with a quality score.
package pdq

// torbenMedian computes the median of a slice of float32 values using
// Torben Mogensen's algorithm. This finds the median without sorting
// by iteratively narrowing the search range.
//
// Algorithm by Torben Mogensen, implementation based on N. Devillard's
// public domain code from the ThreatExchange PDQ implementation.
func torbenMedian(m []float32) float32 {
	n := len(m)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return m[0]
	}

	// Find initial min and max
	min, max := m[0], m[0]
	for i := 1; i < n; i++ {
		if m[i] < min {
			min = m[i]
		}
		if m[i] > max {
			max = m[i]
		}
	}

	var less, greater, equal int
	var maxltguess, mingtguess float32

	for {
		guess := (min + max) / 2
		less = 0
		greater = 0
		equal = 0
		maxltguess = min
		mingtguess = max

		for i := range n {
			if m[i] < guess {
				less++
				if m[i] > maxltguess {
					maxltguess = m[i]
				}
			} else if m[i] > guess {
				greater++
				if m[i] < mingtguess {
					mingtguess = m[i]
				}
			} else {
				equal++
			}
		}

		if less <= (n+1)/2 && greater <= (n+1)/2 {
			break
		} else if less > greater {
			max = maxltguess
		} else {
			min = mingtguess
		}
	}

	if less >= (n+1)/2 {
		return maxltguess
	} else if less+equal >= (n+1)/2 {
		return (min + max) / 2
	}
	return mingtguess
}
