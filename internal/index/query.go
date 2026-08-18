package index

import (
	"errors"
	"fmt"
	"strings"
)

// validateRoleList reports whether roles is a valid spec section 7.1 role list.
// Nil is valid and means "omitted". A non-nil list must be non-empty and free
// of duplicates, and every value must be a basic token.
func validateRoleList(roles []string) error {
	if roles == nil {
		return nil
	}
	if len(roles) == 0 {
		return errors.New("must be non-empty when present")
	}
	return validateUniqueBasicTokens(roles, "role")
}

// validateUniqueBasicTokens reports whether values are unique basic tokens.
func validateUniqueBasicTokens(values []string, kind string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateBasicToken(value); err != nil {
			return fmt.Errorf("%s %q: %w", kind, value, err)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

// validateArchitecture reports whether s is one basic token or two basic
// tokens separated by a slash, per spec section 5.3.
func validateArchitecture(s string) error {
	left, right, ok := strings.Cut(s, "/")
	if !ok {
		return validateBasicToken(s)
	}
	if strings.Contains(right, "/") {
		return fmt.Errorf("%q: architecture must be one basic token or two separated by /", s)
	}
	if err := validateBasicToken(left); err != nil {
		return fmt.Errorf("architecture first token: %w", err)
	}
	if err := validateBasicToken(right); err != nil {
		return fmt.Errorf("architecture second token: %w", err)
	}
	return nil
}

// validateBasicToken reports whether s matches spec section 5.3 basic-token
// syntax: 1 to 128 ASCII bytes of ^[a-z0-9]+([._-][a-z0-9]+)*$.
func validateBasicToken(s string) error {
	if !isBasicToken(s) {
		return fmt.Errorf("%q is not a basic token", s)
	}
	return nil
}

// canonicalUsageQuery canonicalizes a query usage list, which must not contain
// duplicates. field names the query field in the error.
func canonicalUsageQuery(values []string, field string) (string, error) {
	if err := uniqueUsageTokens(values); err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	canonical, err := CanonicalizeUsage(values)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}

	return canonical, nil
}

// uniqueUsageTokens reports the first duplicated usage token. Token syntax is
// left to [CanonicalizeUsage] so List and Resolve share one grammar.
func uniqueUsageTokens(values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate usage %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
