package report

import (
	"strings"
	"testing"
)

// TestSheetNotesDoNotClaimVISDKServerDiffers keeps the worksheet notes that
// `vsfleet explain rvtools` prints in step with the VI SDK Server column,
// which is written as RVTools writes it: the host without the URL scheme.
func TestSheetNotesDoNotClaimVISDKServerDiffers(t *testing.T) {
	profile, err := Profile()
	if err != nil {
		t.Fatalf("building the compatibility profile: %v", err)
	}
	stale := []string{
		"URL with scheme",
		"VI SDK Server differed",
		"except VI SDK Server",
	}
	for _, spec := range profile {
		t.Run(spec.Name, func(t *testing.T) {
			for _, phrase := range stale {
				if strings.Contains(spec.Note, phrase) {
					t.Errorf("note says %q, but VI SDK Server is written without the scheme, as RVTools does", phrase)
				}
			}
		})
	}
}
