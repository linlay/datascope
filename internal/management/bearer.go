package management

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Empty HeaderName uses Authorization: Bearer. Custom API key headers must
// never interfere with routing, protocol negotiation or platform cookies.
func validTokenHeader(name string) bool {
	if name == "" {
		return true
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mcp-") || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "sec-") {
		return false
	}
	switch lower {
	case "authorization", "host", "cookie", "set-cookie", "content-length", "content-type", "accept", "connection", "transfer-encoding", "upgrade", "te", "trailer", "keep-alive", "last-event-id", "origin":
		return false
	}
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}

func (r ResolvedAuth) verifyStaticToken(database, token string) (BusinessAccount, error) {
	if token == "" || len(token) > 65536 || r.Policy.BusinessUserID <= 0 {
		return BusinessAccount{}, ErrDenied
	}
	if r.Provider.SecretEnv == "" {
		if r.store == nil || !r.store.verifyDatabaseAPIKey(context.Background(), database, token) {
			return BusinessAccount{}, ErrDenied
		}
	} else {
		// Compatibility for explicitly configured external deployments. New keys
		// are generated into each database's auth directory, never its YAML or env.
		secret := os.Getenv(r.Provider.SecretEnv)
		if len(secret) < 16 {
			return BusinessAccount{}, ErrDenied
		}
		want, got := sha256.Sum256([]byte(secret)), sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
			return BusinessAccount{}, ErrDenied
		}
	}
	return BusinessAccount{ID: externalID("token:"+database, r.Provider.ID), DatabaseID: database, Username: "token:" + r.Provider.ID, Name: r.Provider.Name, BusinessUserID: r.Policy.BusinessUserID, Permissions: r.Policy.Permissions, Active: true, AuthVersion: int64(r.Revision)}, nil
}

type oneIDKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// Public keys are administrator-pinned config, never loaded from token claims.
// Accept SPKI and PKCS#1 RSA public keys; reject private keys and extra blocks.
func parseOneIDPublicKey(raw string) (*rsa.PublicKey, error) {
	if len(raw) > 64*1024 {
		return nil, fmt.Errorf("OneID PEM 公钥不能超过 64 KiB")
	}
	data := []byte(strings.TrimSpace(raw))
	block, rest := pem.Decode(data)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || len(block.Headers) != 0 || !strings.HasPrefix(string(data), "-----BEGIN "+block.Type+"-----") {
		return nil, fmt.Errorf("OneID 需要单个有效 PEM 公钥")
	}
	var key *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err == nil {
			key, _ = parsed.(*rsa.PublicKey)
		}
	case "RSA PUBLIC KEY":
		key, _ = x509.ParsePKCS1PublicKey(block.Bytes)
	}
	if key == nil || key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.E < 3 || key.E > 2147483647 || key.E%2 == 0 {
		return nil, fmt.Errorf("OneID 需要 2048–8192 位 RSA 公钥（PUBLIC KEY 或 RSA PUBLIC KEY）")
	}
	return key, nil
}

var oneIDKeys = struct {
	sync.Mutex
	entries map[string]struct {
		keys    []oneIDKey
		expires time.Time
	}
}{entries: make(map[string]struct {
	keys    []oneIDKey
	expires time.Time
})}

// A short bounded cache avoids network I/O on every tool call. Unknown key IDs
// do not force refreshes; rotations become visible within one minute.
func loadOneIDKeys(ctx context.Context, endpoint string) ([]oneIDKey, error) {
	oneIDKeys.Lock()
	defer oneIDKeys.Unlock()
	if entry, ok := oneIDKeys.entries[endpoint]; ok && time.Now().Before(entry.expires) {
		return entry.keys, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, ErrDenied
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OneID 公钥服务暂不可用")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, ErrDenied
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	var set struct {
		Keys []oneIDKey `json:"keys"`
	}
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &set) != nil || len(set.Keys) == 0 || len(set.Keys) > 32 {
		return nil, ErrDenied
	}
	if len(oneIDKeys.entries) >= 64 {
		clear(oneIDKeys.entries)
	}
	oneIDKeys.entries[endpoint] = struct {
		keys    []oneIDKey
		expires time.Time
	}{set.Keys, time.Now().Add(time.Minute)}
	return set.Keys, nil
}

func verifyOneID(ctx context.Context, p AuthProvider, token string) (map[string]any, error) {
	if token == "" || len(token) > 65536 {
		return nil, ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrDenied
	}
	decode := base64.RawURLEncoding.DecodeString
	header, err := decode(parts[0])
	var h struct {
		Alg  string   `json:"alg"`
		Kid  string   `json:"kid"`
		Crit []string `json:"crit"`
	}
	if err != nil || json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || len(h.Crit) != 0 {
		return nil, ErrDenied
	}
	signature, err := decode(parts[2])
	if err != nil {
		return nil, ErrDenied
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	verified := false
	// Exactly one configured trust source, with no fallback on verification failure.
	hasJWKS, hasPEM := strings.TrimSpace(p.JWKSURL) != "", strings.TrimSpace(p.PublicKeyPEM) != ""
	if hasJWKS == hasPEM {
		return nil, ErrDenied
	}
	if hasPEM {
		key, err := parseOneIDPublicKey(p.PublicKeyPEM)
		if err != nil {
			return nil, ErrDenied
		}
		verified = rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
	} else {
		keys, err := loadOneIDKeys(ctx, p.JWKSURL)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if k.Kty != "RSA" || k.Kid != h.Kid || k.Alg != "" && k.Alg != "RS256" || k.Use != "" && k.Use != "sig" {
				continue
			}
			n, e1 := decode(k.N)
			e, e2 := decode(k.E)
			if e1 != nil || e2 != nil || len(n) < 256 || len(n) > 1024 || len(e) == 0 || len(e) > 4 {
				continue
			}
			exponent := new(big.Int).SetBytes(e).Int64()
			if exponent < 3 || exponent > 2147483647 || exponent%2 == 0 {
				continue
			}
			key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
			if key.N.BitLen() >= 2048 && rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil {
				verified = true
				break
			}
		}
	}
	if !verified {
		return nil, ErrDenied
	}
	raw, err := decode(parts[1])
	if err != nil {
		return nil, ErrDenied
	}
	claims := map[string]any{}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&claims) != nil || dec.Decode(new(any)) != io.EOF {
		return nil, ErrDenied
	}
	number := func(name string) (int64, bool) {
		n, ok := claims[name].(json.Number)
		v, err := n.Int64()
		return v, ok && err == nil
	}
	now := time.Now().Unix()
	exp, ok := number("exp")
	if !ok || exp <= now || claims["iss"] != p.Issuer {
		return nil, ErrDenied
	}
	for _, name := range []string{"nbf", "iat"} {
		if _, exists := claims[name]; exists {
			if v, ok := number(name); !ok || v > now {
				return nil, ErrDenied
			}
		}
	}
	audience := claims["aud"] == p.Audience
	if list, ok := claims["aud"].([]any); ok {
		for _, v := range list {
			if v == p.Audience {
				audience = true
			}
		}
		if len(list) > 1 && claims["azp"] != p.Audience {
			audience = false
		}
	}
	if !audience || claimString(claims["sub"]) == "" {
		return nil, ErrDenied
	}
	if active, exists := claims["active"]; exists && active != true {
		return nil, ErrDenied
	}
	return claims, nil
}
