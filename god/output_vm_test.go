package god

import "testing"

func TestOutputVMSoft(t *testing.T) {
	config := OutputVMConfig{Mode: OutputSoft}

	// edit_ir accepted
	r1 := EnforceOutput(config, &AngelResponse{OutputType: "edit_ir"})
	if !r1.Accepted {
		t.Error("soft mode should accept edit_ir")
	}

	// diff_fallback accepted with warning
	r2 := EnforceOutput(config, &AngelResponse{OutputType: "diff_fallback"})
	if !r2.Accepted {
		t.Error("soft mode should accept diff_fallback")
	}
	if r2.Reason == "" {
		t.Error("soft mode should warn on diff_fallback")
	}

	// macro_ops accepted
	r3 := EnforceOutput(config, &AngelResponse{OutputType: "macro_ops"})
	if !r3.Accepted {
		t.Error("soft mode should accept macro_ops")
	}
}

func TestOutputVMMedium(t *testing.T) {
	config := OutputVMConfig{Mode: OutputMedium}

	// edit_ir accepted
	r1 := EnforceOutput(config, &AngelResponse{OutputType: "edit_ir"})
	if !r1.Accepted {
		t.Error("medium mode should accept edit_ir")
	}

	// macro_ops accepted
	r2 := EnforceOutput(config, &AngelResponse{OutputType: "macro_ops"})
	if !r2.Accepted {
		t.Error("medium mode should accept macro_ops")
	}

	// diff_fallback rejected
	r3 := EnforceOutput(config, &AngelResponse{OutputType: "diff_fallback"})
	if r3.Accepted {
		t.Error("medium mode should reject diff_fallback")
	}
	if r3.Reason == "" {
		t.Error("rejection should include reason")
	}
}

func TestOutputVMHard(t *testing.T) {
	config := OutputVMConfig{Mode: OutputHard}

	// edit_ir accepted
	r1 := EnforceOutput(config, &AngelResponse{OutputType: "edit_ir"})
	if !r1.Accepted {
		t.Error("hard mode should accept edit_ir")
	}

	// macro_ops accepted
	r2 := EnforceOutput(config, &AngelResponse{OutputType: "macro_ops"})
	if !r2.Accepted {
		t.Error("hard mode should accept macro_ops")
	}

	// diff_fallback rejected
	r3 := EnforceOutput(config, &AngelResponse{OutputType: "diff_fallback"})
	if r3.Accepted {
		t.Error("hard mode should reject diff_fallback")
	}
	if r3.Reason == "" {
		t.Error("rejection should include reason")
	}
}
