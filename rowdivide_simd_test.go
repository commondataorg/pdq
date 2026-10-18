//go:build goexperiment.simd

package pdq

import "testing"

// TestBoxAlongRows_BothDivides runs the row filter with its divides inline
// and deferred, whichever one this machine selects.
func TestBoxAlongRows_BothDivides(t *testing.T) {
	saved := vectorDivide
	t.Cleanup(func() { vectorDivide = saved })
	for _, v := range []bool{false, true} {
		vectorDivide = v
		checkBoxRowsMatchScalar(t)
	}
}
