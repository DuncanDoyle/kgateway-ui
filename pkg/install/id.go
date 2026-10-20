package install

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateID reports whether id may be an install ID (D-016): one URL path
// segment of unreserved characters (A-Z a-z 0-9 . _ ~ -), and neither "." nor
// "..", which URL normalisation would remove.
func ValidateID(id string) error {
	switch id {
	case "":
		return errors.New("install ID is empty")
	case ".", "..":
		return fmt.Errorf("install ID %q is a dot segment, which URL normalisation removes", id)
	}
	for _, r := range id {
		if !unreserved(r) {
			return fmt.Errorf("install ID %q contains %q; only A-Z a-z 0-9 . _ ~ - are allowed", id, r)
		}
	}
	return nil
}

func unreserved(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._~-", r)
}

// ParsePinned turns an --install value into an install ID. It accepts the ID
// itself or "namespace/name", which becomes "<namespace>.<name>"; empty pins
// nothing.
func ParsePinned(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	id := s
	if ns, name, ok := strings.Cut(s, "/"); ok {
		if ns == "" || name == "" || strings.Contains(name, "/") {
			return "", fmt.Errorf("--install %q: want an install ID or namespace/name", s)
		}
		id = ns + "." + name
	}
	if err := ValidateID(id); err != nil {
		return "", fmt.Errorf("--install %q: %w", s, err)
	}
	return id, nil
}
