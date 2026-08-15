package index

import (
	"fmt"

	"github.com/imgoci/go/internal/jcs"
)

// VerifyCanonical applies spec §6 rule 10 to the original index bytes.
//
// It delegates to [jcs.Verify] with a generic JSON tree so unknown members
// remain visible to the RFC 8785 transform. Callers that also need rules 1–9
// must run [Decode] and [Validate] themselves; [VerifyCanonical] does not.
func VerifyCanonical(b []byte) error {
	parsed, err := decodeJSON(b)
	if err != nil {
		return fmt.Errorf("spec §6 rule %d: %w", specRuleCanonical, err)
	}
	if err := jcs.Verify(b, parsed); err != nil {
		return fmt.Errorf("spec §6 rule %d: %w", specRuleCanonical, err)
	}
	return nil
}
