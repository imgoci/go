package index

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	// maxUsageBytes is the spec §5.3 maximum length of a present
	// io.imgoci.usage value.
	maxUsageBytes = 4096
	// usageInstall is the spec §5.4 install usage token.
	usageInstall = "install"
	// usageInstallOffline is the spec §5.4 install-offline usage token.
	usageInstallOffline = "install-offline"
)

// CanonicalizeUsage validates tokens, sorts, de-duplicates, and joins with commas.
//
// An empty or nil input is the empty usage set and returns "", nil. The caller's
// slice is not mutated. Duplicates collapse silently. Each token must be a
// spec §5.3 basic token, and the joined value must not exceed [maxUsageBytes].
func CanonicalizeUsage(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	tokens := slices.Clone(values)
	for _, token := range tokens {
		if !isBasicToken(token) {
			return "", fmt.Errorf("usage token %q is not a basic token", token)
		}
	}
	slices.Sort(tokens)
	tokens = slices.Compact(tokens)
	value := strings.Join(tokens, ",")
	if len(value) > maxUsageBytes {
		return "", fmt.Errorf("usage value exceeds %d ASCII bytes", maxUsageBytes)
	}
	return value, nil
}

// ValidateUsage validates a present serialized io.imgoci.usage annotation.
//
// The empty string is invalid: the empty usage set is represented by omitting
// the annotation. Tokens must be unique basic tokens in strictly ascending
// UTF-8 byte order, separated by a single comma with no whitespace, and the
// complete value must not exceed [maxUsageBytes].
func ValidateUsage(value string) error {
	if value == "" {
		return errors.New("usage value must not be empty")
	}
	if len(value) > maxUsageBytes {
		return fmt.Errorf("usage value exceeds %d ASCII bytes", maxUsageBytes)
	}
	prev := ""
	first := true
	for token := range strings.SplitSeq(value, ",") {
		if token == "" {
			return errors.New("usage value contains an empty token")
		}
		if !isBasicToken(token) {
			return fmt.Errorf("usage token %q is not a basic token", token)
		}
		if !first && token <= prev {
			return errors.New("usage tokens must be in strictly ascending UTF-8 byte order")
		}
		prev = token
		first = false
	}
	return nil
}

// ValidateUsageRelationship rejects a usage set that contains install-offline
// without install.
func ValidateUsageRelationship(canonical string) error {
	hasInstall := false
	hasInstallOffline := false
	for token := range strings.SplitSeq(canonical, ",") {
		switch token {
		case usageInstall:
			hasInstall = true
		case usageInstallOffline:
			hasInstallOffline = true
		}
	}
	if hasInstallOffline && !hasInstall {
		return fmt.Errorf("%s requires %s", usageInstallOffline, usageInstall)
	}
	return nil
}
