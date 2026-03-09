package core

import (
	"testing"
)

func TestParsePlanSummary(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   *PlanSummary
	}{
		{
			name:   "normal plan with changes",
			output: "Plan: 88 to add, 1 to change, 0 to destroy.",
			want:   &PlanSummary{Add: 88, Change: 1, Destroy: 0},
		},
		{
			name:   "destroy plan",
			output: "Plan: 0 to add, 0 to change, 5 to destroy.",
			want:   &PlanSummary{Add: 0, Change: 0, Destroy: 5},
		},
		{
			name:   "all non-zero",
			output: "Plan: 3 to add, 2 to change, 1 to destroy.",
			want:   &PlanSummary{Add: 3, Change: 2, Destroy: 1},
		},
		{
			// terraform outputs color codes when -no-color is not set
			name:   "with ANSI color codes",
			output: "Plan: \x1b[32m88\x1b[0m to add, \x1b[33m1\x1b[0m to change, \x1b[31m0\x1b[0m to destroy.",
			want:   &PlanSummary{Add: 88, Change: 1, Destroy: 0},
		},
		{
			name:   "embedded in full plan output",
			output: "Terraform will perform the following actions:\n\n  # aws_instance.example will be created\n\nPlan: 3 to add, 2 to change, 1 to destroy.\n\nChanges to Outputs:",
			want:   &PlanSummary{Add: 3, Change: 2, Destroy: 1},
		},
		{
			name:   "no changes",
			output: "No changes. Your infrastructure matches the configuration.",
			want:   nil,
		},
		{
			name:   "empty string",
			output: "",
			want:   nil,
		},
		{
			name:   "unrelated output",
			output: "Initializing the backend...\nInitializing provider plugins...",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePlanSummary(tt.output)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parsePlanSummary() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("parsePlanSummary() = nil, want %+v", tt.want)
				return
			}
			if got.Add != tt.want.Add || got.Change != tt.want.Change || got.Destroy != tt.want.Destroy {
				t.Errorf("parsePlanSummary() = {Add:%d Change:%d Destroy:%d}, want {Add:%d Change:%d Destroy:%d}",
					got.Add, got.Change, got.Destroy,
					tt.want.Add, tt.want.Change, tt.want.Destroy)
			}
		})
	}
}
