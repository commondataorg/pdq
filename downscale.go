package pdq

// computeJaroszFilterWindowSize calculates the window size for the Jarosz filter.
// This implements the formula: (oldDim + 2*newDim - 1) / (2*newDim)
func computeJaroszFilterWindowSize(oldDimension, newDimension int) int {
	return (oldDimension + 2*newDimension - 1) / (2 * newDimension)
}

// box1DFloat applies a 1D box filter to a vector.
// The filter uses a sliding window approach with 4 phases:
// 1. Accumulate initial sum (no writes)
// 2. Write with growing window
// 3. Write with full window (add and subtract)
// 4. Write with shrinking window (subtract only)
func box1DFloat(invec, outvec []float32, vectorLength, stride, fullWindowSize int) {
	halfWindowSize := (fullWindowSize + 2) / 2 // 7->4, 8->5

	phase1Nreps := halfWindowSize - 1
	phase2Nreps := fullWindowSize - halfWindowSize + 1
	phase3Nreps := vectorLength - fullWindowSize
	phase4Nreps := halfWindowSize - 1

	li := 0 // Index of left edge of read window, for subtracts
	ri := 0 // Index of right edge of read window, for adds
	oi := 0 // Index into output vector

	var sum float32
	currentWindowSize := 0

	// PHASE 1: ACCUMULATE FIRST SUM NO WRITES
	for range phase1Nreps {
		sum += invec[ri]
		currentWindowSize++
		ri += stride
	}

	// PHASE 2: INITIAL WRITES WITH SMALL WINDOW
	for range phase2Nreps {
		sum += invec[ri]
		currentWindowSize++
		outvec[oi] = sum / float32(currentWindowSize)
		ri += stride
		oi += stride
	}

	// PHASE 3: WRITES WITH FULL WINDOW
	for range phase3Nreps {
		sum += invec[ri]
		sum -= invec[li]
		outvec[oi] = sum / float32(currentWindowSize)
		li += stride
		ri += stride
		oi += stride
	}

	// PHASE 4: FINAL WRITES WITH SMALL WINDOW
	for range phase4Nreps {
		sum -= invec[li]
		currentWindowSize--
		outvec[oi] = sum / float32(currentWindowSize)
		li += stride
		oi += stride
	}
}

// boxAlongRowsScalarFloat applies the 1D box filter along each row of the
// matrix, one row at a time.
func boxAlongRowsScalarFloat(in, out []float32, numRows, numCols, windowSize int) {
	for i := range numRows {
		offset := i * numCols
		box1DFloat(in[offset:], out[offset:], numCols, 1, windowSize)
	}
}

// boxAlongRowsFloat applies the 1D box filter along each row, bit-identical
// to boxAlongRowsScalarFloat. A row's running sum is a serial chain of
// dependent adds, so it advances four rows in lockstep to give the CPU
// independent chains to overlap. The sums are separate locals rather than an
// array so they stay in registers.
func boxAlongRowsFloat(in, out []float32, numRows, numCols, windowSize int) {
	i := 0
	for ; i+4 <= numRows; i += 4 {
		boxRows4(in, out, numCols, windowSize, i*numCols)
	}
	for ; i < numRows; i++ {
		offset := i * numCols
		box1DFloat(in[offset:], out[offset:], numCols, 1, windowSize)
	}
}

// boxRows4 runs the 4-phase box filter over the four rows starting at flat
// offset base, with the same phase structure as box1DFloat.
func boxRows4(in, out []float32, numCols, fullWindowSize, base int) {
	halfWindowSize := (fullWindowSize + 2) / 2

	phase1Nreps := halfWindowSize - 1
	phase2Nreps := fullWindowSize - halfWindowSize + 1
	phase3Nreps := numCols - fullWindowSize
	phase4Nreps := halfWindowSize - 1

	r0 := in[base : base+numCols]
	r1 := in[base+numCols : base+2*numCols]
	r2 := in[base+2*numCols : base+3*numCols]
	r3 := in[base+3*numCols : base+4*numCols]
	o0 := out[base : base+numCols]
	o1 := out[base+numCols : base+2*numCols]
	o2 := out[base+2*numCols : base+3*numCols]
	o3 := out[base+3*numCols : base+4*numCols]

	var s0, s1, s2, s3 float32
	li, ri, oi := 0, 0, 0
	currentWindowSize := 0

	for range phase1Nreps {
		s0 += r0[ri]
		s1 += r1[ri]
		s2 += r2[ri]
		s3 += r3[ri]
		currentWindowSize++
		ri++
	}

	for range phase2Nreps {
		s0 += r0[ri]
		s1 += r1[ri]
		s2 += r2[ri]
		s3 += r3[ri]
		currentWindowSize++
		d := float32(currentWindowSize)
		o0[oi] = s0 / d
		o1[oi] = s1 / d
		o2[oi] = s2 / d
		o3[oi] = s3 / d
		ri++
		oi++
	}

	d := float32(currentWindowSize)
	for range phase3Nreps {
		s0 += r0[ri]
		s0 -= r0[li]
		s1 += r1[ri]
		s1 -= r1[li]
		s2 += r2[ri]
		s2 -= r2[li]
		s3 += r3[ri]
		s3 -= r3[li]
		o0[oi] = s0 / d
		o1[oi] = s1 / d
		o2[oi] = s2 / d
		o3[oi] = s3 / d
		li++
		ri++
		oi++
	}

	for range phase4Nreps {
		s0 -= r0[li]
		s1 -= r1[li]
		s2 -= r2[li]
		s3 -= r3[li]
		currentWindowSize--
		d := float32(currentWindowSize)
		o0[oi] = s0 / d
		o1[oi] = s1 / d
		o2[oi] = s2 / d
		o3[oi] = s3 / d
		li++
		oi++
	}
}

// boxAlongColsScalarFloat applies the 1D box filter down each column one column
// at a time. Each call strides through memory by numCols, which is cache-hostile
// for wide images; it is retained as the reference oracle for the column
// filters.
func boxAlongColsScalarFloat(in, out []float32, numRows, numCols, windowSize int) {
	for j := range numCols {
		box1DFloat(in[j:], out[j:], numRows, numCols, windowSize)
	}
}

// boxAlongColsSweepFloat applies the 1D box filter down each column,
// bit-identical to boxAlongColsScalarFloat. It sweeps whole rows at a time,
// holding one running sum per column, so every pass reads and writes rows
// sequentially and the per-column updates are independent.
func boxAlongColsSweepFloat(in, out []float32, numRows, numCols, fullWindowSize int) {
	halfWindowSize := (fullWindowSize + 2) / 2

	phase1Nreps := halfWindowSize - 1
	phase2Nreps := fullWindowSize - halfWindowSize + 1
	phase3Nreps := numRows - fullWindowSize
	phase4Nreps := halfWindowSize - 1

	sum := make([]float32, numCols)
	row := func(i int) []float32 { return in[i*numCols : (i+1)*numCols] }
	orow := func(i int) []float32 { return out[i*numCols : (i+1)*numCols] }
	li, ri, oi := 0, 0, 0
	currentWindowSize := 0

	for range phase1Nreps {
		r := row(ri)
		for c := range sum {
			sum[c] += r[c]
		}
		currentWindowSize++
		ri++
	}

	for range phase2Nreps {
		r, o := row(ri), orow(oi)
		currentWindowSize++
		d := float32(currentWindowSize)
		for c := range sum {
			sum[c] += r[c]
			o[c] = sum[c] / d
		}
		ri++
		oi++
	}

	d := float32(currentWindowSize)
	for range phase3Nreps {
		r, l, o := row(ri), row(li), orow(oi)
		for c := range sum {
			sum[c] += r[c]
			sum[c] -= l[c]
			o[c] = sum[c] / d
		}
		li++
		ri++
		oi++
	}

	for range phase4Nreps {
		l, o := row(li), orow(oi)
		currentWindowSize--
		d := float32(currentWindowSize)
		for c := range sum {
			sum[c] -= l[c]
			o[c] = sum[c] / d
		}
		li++
		oi++
	}
}

// jaroszFilterFloat applies the Jarosz filter (tent filter) to the input buffer.
// This consists of nreps passes of X then Y box filters.
// After filtering, buffer1 contains the result.
func jaroszFilterFloat(buffer1, buffer2 []float32, numRows, numCols, windowSizeAlongRows, windowSizeAlongCols, nreps int) {
	for range nreps {
		boxAlongRowsFloat(buffer1, buffer2, numRows, numCols, windowSizeAlongRows)
		boxAlongColsFloat(buffer2, buffer1, numRows, numCols, windowSizeAlongCols)
	}
}

// decimateFloat downsamples the input matrix to the output size by sampling
// at the center of each output cell.
func decimateFloat(in []float32, inNumRows, inNumCols int, out []float32, outNumRows, outNumCols int) {
	for outi := range outNumRows {
		ini := int((float64(outi) + 0.5) * float64(inNumRows) / float64(outNumRows))
		for outj := range outNumCols {
			inj := int((float64(outj) + 0.5) * float64(inNumCols) / float64(outNumCols))
			out[outi*outNumCols+outj] = in[ini*inNumCols+inj]
		}
	}
}
