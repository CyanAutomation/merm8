package rules

// IntPtr returns a pointer to v for issue location fields.
func IntPtr(v int) *int {
	return &v
}
