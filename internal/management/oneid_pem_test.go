package management

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
	"time"
)

func TestOneIDPEMAndDirectIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	p := AuthProvider{ID: "oneid", Name: "Test", Type: "ONEID", Enabled: true, Issuer: "https://identity.example.invalid", Audience: "desktop", PublicKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), SubjectClaim: "sub", UsernameClaim: "staffno", BusinessIDClaim: "staffno"}
	if err = validateAuthProvider(p); err != nil {
		t.Fatal(err)
	}
	r := ResolvedAuth{Provider: p, Source: "DATABASE", Policy: AuthPolicy{Mode: "ONEID", Permissions: []string{"query", "knowledge.read"}}}
	sign := func(patch map[string]any) string {
		claims := map[string]any{"iss": p.Issuer, "aud": p.Audience, "exp": time.Now().Add(time.Hour).Unix(), "sub": "external-subject", "staffno": "900039", "name": "演示用户", "roles": []string{"admin"}, "permissions": []string{"raw_query"}}
		for k, v := range patch {
			claims[k] = v
		}
		h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "rotating-key"})
		body, _ := json.Marshal(claims)
		unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(body)
		hash := sha256.Sum256([]byte(unsigned))
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if e != nil {
			t.Fatal(e)
		}
		return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	for _, format := range []string{"PUBLIC KEY", "RSA PUBLIC KEY"} {
		if format == "RSA PUBLIC KEY" {
			r.Provider.PublicKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: format, Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))
		}
		a, e := r.VerifyBearer(context.Background(), "sales", sign(nil))
		if e != nil {
			t.Fatal(e)
		}
		if a.BusinessUserID != 900039 || a.Username != "900039" || a.Name != "演示用户" || strings.Join(a.Permissions, ",") != "query,knowledge.read" {
			t.Fatalf("wrong trusted identity: %+v", a)
		}
	}
	for name, patch := range map[string]map[string]any{
		"missing identity": {"staffno": nil}, "non-numeric identity": {"staffno": "external-sales"}, "expired": {"exp": time.Now().Add(-time.Minute).Unix()}, "wrong issuer": {"iss": "https://other.invalid"}, "wrong audience": {"aud": "other"}, "future nbf": {"nbf": time.Now().Add(time.Hour).Unix()}, "future iat": {"iat": time.Now().Add(time.Hour).Unix()}, "multiple aud without azp": {"aud": []string{p.Audience, "other"}}, "inactive": {"active": false},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := r.VerifyBearer(context.Background(), "sales", sign(patch)); e == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	token := sign(nil)
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"staffno":"900039"}`))
	if _, err = r.VerifyBearer(context.Background(), "sales", strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token accepted")
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	r.Provider.PublicKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&other.PublicKey)}))
	if _, err = r.VerifyBearer(context.Background(), "sales", token); err == nil {
		t.Fatal("wrong pinned key accepted")
	}
	r.Provider = p
	r.Provider.JWKSURL = "https://must-not-fetch.invalid/jwks"
	if err = validateAuthProvider(r.Provider); err == nil {
		t.Fatal("ambiguous key source accepted")
	}
	if _, err = r.VerifyBearer(context.Background(), "sales", token); err == nil {
		t.Fatal("ambiguous runtime config accepted")
	}
	r.Provider = p
	a, err := r.VerifyBearer(context.Background(), "sales", token)
	if err != nil || a.BusinessUserID != 900039 {
		t.Fatalf("direct ID compatibility: %+v %v", a, err)
	}
	// Persisted fields survive both JSON and YAML, with enterprise IDs kept as strings.
	cfg := DatabaseAuthConnection{Source: "DATABASE", Provider: &p, Access: &AuthPolicy{ID: "test", Name: "Test", Mode: "ONEID", ProviderID: p.ID}}
	for _, yamlMode := range []bool{false, true} {
		var raw []byte
		var e error
		var back DatabaseAuthConnection
		if yamlMode {
			raw, e = yaml.Marshal(cfg)
			if e == nil {
				e = yaml.Unmarshal(raw, &back)
			}
		} else {
			raw, e = json.Marshal(cfg)
			if e == nil {
				e = json.Unmarshal(raw, &back)
			}
		}
		if e != nil || back.Provider.PublicKeyPEM != p.PublicKeyPEM {
			t.Fatalf("roundtrip: %s %v", raw, e)
		}
	}
}

func TestOneIDRejectsInvalidPublicKeys(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKIXPublicKey(&ec.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "publickey.pem", "garbage", strings.Repeat("x", 65537), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecDER}))} {
		if _, err = parseOneIDPublicKey(raw); err == nil {
			t.Fatal("invalid/weak/private key accepted")
		}
	}
}
