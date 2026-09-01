//go:build test

package store

// SetBeforeSaveHook installs a synchronization point for deterministic tests.
func (s *Store) SetBeforeSaveHook(hook func()) {
	s.beforeSaveHook = hook
}
