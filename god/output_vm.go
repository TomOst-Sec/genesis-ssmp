package god

// OutputMode controls how strictly the Output VM enforces Edit IR output.
type OutputMode string

const (
	OutputSoft   OutputMode = "soft"   // warn on diff_fallback, accept
	OutputMedium OutputMode = "medium" // reject diff_fallback, allow retry
	OutputHard   OutputMode = "hard"   // reject anything not edit_ir/macro_ops, no retry
)

// OutputVMConfig configures the Output VM enforcement layer.
type OutputVMConfig struct {
	Mode OutputMode
}

// DefaultOutputVMConfig returns the soft (current behavior) default.
func DefaultOutputVMConfig() OutputVMConfig {
	return OutputVMConfig{Mode: OutputSoft}
}

// OutputVMResult describes the outcome of output enforcement.
type OutputVMResult struct {
	Accepted   bool   `json:"accepted"`
	Mode       string `json:"mode"`
	OutputType string `json:"output_type"`
	Reason     string `json:"reason,omitempty"`
}

// EnforceOutput checks an AngelResponse against the configured output mode.
func EnforceOutput(config OutputVMConfig, resp *AngelResponse) OutputVMResult {
	result := OutputVMResult{
		Mode:       string(config.Mode),
		OutputType: resp.OutputType,
	}

	switch config.Mode {
	case OutputSoft:
		result.Accepted = true
		if resp.OutputType == "diff_fallback" {
			result.Reason = "warning: diff_fallback accepted in soft mode"
		}

	case OutputMedium:
		if resp.OutputType == "edit_ir" || resp.OutputType == "macro_ops" {
			result.Accepted = true
		} else {
			result.Accepted = false
			result.Reason = "diff_fallback rejected in medium mode; retry with edit_ir"
		}

	case OutputHard:
		if resp.OutputType == "edit_ir" || resp.OutputType == "macro_ops" {
			result.Accepted = true
		} else {
			result.Accepted = false
			result.Reason = "only edit_ir or macro_ops accepted in hard mode; no retry"
		}
	}

	return result
}
