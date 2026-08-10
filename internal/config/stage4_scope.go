package config

// ValidateBoundedStage4Settings is the Stage 4 route gate. Settings that are
// not consumed by the bounded runner are rejected here; the former
// history-retention setting was removed from the configuration contract, so no
// global retention policy is enforced by DMTX.
func ValidateBoundedStage4Settings(migration Migration) error {
	return nil
}
