package management

import (
	"crypto/hmac"
	"crypto/sha256"
)

// MaskingKey derives a separate purpose-specific key from the existing private
// per-database key. Reads never regenerate missing keys or change pseudonyms.
func (s *Store) MaskingKey(id string, create bool) ([]byte, error) {
	key, err := s.credentialKey(id, create)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("datascope/masking/v1/" + id))
	return mac.Sum(nil), nil
}
