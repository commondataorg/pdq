package pdq

// pdqImageDomainQualityMetric computes the quality score from the 64x64 downsampled image.
// Quality is based on the sum of gradient magnitudes, which indicates how much
// detail/edge content is in the image. Low quality scores indicate flat/blurry images.
func pdqImageDomainQualityMetric(buffer64x64 *[64][64]float32) int {
	gradientSum := 0

	// Vertical gradients
	for i := range 63 {
		for j := range 64 {
			u := buffer64x64[i][j]
			v := buffer64x64[i+1][j]
			d := int((u - v) * 100 / 255)
			if d < 0 {
				d = -d
			}
			gradientSum += d
		}
	}

	// Horizontal gradients
	for i := range 64 {
		for j := range 63 {
			u := buffer64x64[i][j]
			v := buffer64x64[i][j+1]
			d := int((u - v) * 100 / 255)
			if d < 0 {
				d = -d
			}
			gradientSum += d
		}
	}

	// Heuristic scaling factor
	return min(gradientSum/90, 100)
}
