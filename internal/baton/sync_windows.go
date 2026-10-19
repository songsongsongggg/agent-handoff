//go:build windows

package baton

// Windows is a compile-only candidate. Unix directory fsync semantics are not
// claimed here; see DESIGN.md before enabling a Windows release.
func syncDir(string) error { return nil }
