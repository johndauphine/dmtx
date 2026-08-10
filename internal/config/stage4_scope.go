package config

// ValidateBoundedStage4Settings is retained as the Stage 4 route gate. The
// current bounded runner has no additional global settings to reject; the
// former history-retention setting was removed from the configuration
// contract, so no global retention policy is enforced by DMTX.
func ValidateBoundedStage4Settings(migration Migration) error {
	return nil
}
