package render

// The two unexported pieces the tests reach for: the template scan, which has
// to agree with entity.ParseTemplateVariables or fillers land in the wrong
// slots, and the decimal expansion, whose interesting cases the dataset does
// not reach.

// SplitTemplateForTest exposes splitTemplate.
func SplitTemplateForTest(template string) []string { return splitTemplate(template) }

// DecimalForTest exposes decimal.
func DecimalForTest(exponent, mantissa int64) string { return decimal(exponent, mantissa) }
