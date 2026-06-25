//go:build !darwin

package notify

// fire is a no-op on non-macOS platforms; desktop notifications are only
// supported on macOS. It never fails.
func fire(title, sound string) error { return nil }
