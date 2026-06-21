//go:build !darwin

package notify

// fire is a no-op on non-macOS platforms.
func fire(title, sound string) {}
