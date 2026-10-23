package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

func TestAssessmentDeleteOutput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantText string
	}{
		{name: "text", args: []string{"assessment", "delete", "nightly", "--force"}, wantText: "deleted assessment nightly\n"},
		{name: "json", args: []string{"-o", "json", "assessment", "delete", "nightly", "--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := seedHistory(t, time.Now().UTC().Truncate(time.Second), "", assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success"})
			if _, _, err := runCLI(t, dbPath, "assessment", "update", "latest", "--label", "nightly"); err != nil {
				t.Fatalf("label run: %v", err)
			}
			stdout, _, err := runCLI(t, dbPath, tc.args...)
			if err != nil {
				t.Fatalf("assessment delete: %v", err)
			}
			if tc.wantText != "" {
				if stdout != tc.wantText {
					t.Fatalf("stdout = %q, want %q", stdout, tc.wantText)
				}
			} else {
				var got struct {
					Deleted assessment.Run `json:"deleted"`
				}
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("delete -o json is not JSON: %v\n%s", err, stdout)
				}
				if got.Deleted.ID == 0 || got.Deleted.Label != "nightly" {
					t.Fatalf("deleted = %+v, want the labelled run", got.Deleted)
				}
			}
			s, err := assessment.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			runs, err := s.Runs(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != 0 {
				t.Fatalf("runs after delete = %d, want 0", len(runs))
			}
		})
	}
}
