package management

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type AuthProvider struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	Issuer          string `json:"issuer,omitempty"`
	AuthorizeURL    string `json:"authorize_url,omitempty"`
	TokenURL        string `json:"token_url,omitempty"`
	UserInfoURL     string `json:"userinfo_url,omitempty"`
	AccountURL      string `json:"account_url,omitempty"`
	ClientID        string `json:"client_id,omitempty"`
	SecretEnv       string `json:"secret_env,omitempty"`
	SubjectClaim    string `json:"subject_claim,omitempty"`
	UsernameClaim   string `json:"username_claim,omitempty"`
	BusinessIDClaim string `json:"business_id_claim,omitempty"`
}
type AuthPolicy struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	ProviderID     string   `json:"provider_id,omitempty"`
	Permissions    []string `json:"permissions,omitempty"`
	BusinessUserID int64    `json:"business_user_id,omitempty"`
}
type AuthBinding struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	PolicyID   string `json:"policy_id"`
}
type AuthConfig struct {
	Revision        int            `json:"revision"`
	DefaultPolicyID string         `json:"default_policy_id"`
	Providers       []AuthProvider `json:"providers"`
	Policies        []AuthPolicy   `json:"policies"`
	Bindings        []AuthBinding  `json:"bindings"`
}
type ResolvedAuth struct {
	Policy   AuthPolicy   `json:"policy"`
	Provider AuthProvider `json:"provider"`
	Revision int          `json:"revision"`
}

func (s *Store) initAuth() error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS AUTH_PROVIDER(ID_ TEXT PRIMARY KEY,NAME_ TEXT NOT NULL,TYPE_ TEXT NOT NULL,CONFIG_JSON_ TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS AUTH_POLICY(ID_ TEXT PRIMARY KEY,NAME_ TEXT NOT NULL,MODE_ TEXT NOT NULL,PROVIDER_ID_ TEXT NOT NULL,CONFIG_JSON_ TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS AUTH_BINDING(TARGET_TYPE_ TEXT NOT NULL,TARGET_ID_ TEXT NOT NULL,POLICY_ID_ TEXT NOT NULL,PRIMARY KEY(TARGET_TYPE_,TARGET_ID_));
 CREATE TABLE IF NOT EXISTS AUTH_SETTING(ID_ INTEGER PRIMARY KEY CHECK(ID_=1),REVISION_ INTEGER NOT NULL,DEFAULT_POLICY_ID_ TEXT NOT NULL);`)
	if e != nil {
		return e
	}
	var count int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM AUTH_SETTING").Scan(&count); e != nil {
		return e
	}
	if count != 0 {
		return nil
	}
	cfg := AuthConfig{DefaultPolicyID: "demo-oauth", Providers: []AuthProvider{{ID: "platform-account", Name: "平台账号", Type: "PLATFORM_ACCOUNT", Enabled: true}, {ID: "business-account", Name: "数据源业务账号", Type: "BUSINESS_ACCOUNT", Enabled: true}, {ID: "demo-oauth", Name: "DataScope 演示 OAuth", Type: "DEMO_OAUTH", Enabled: true}}, Policies: []AuthPolicy{{ID: "platform-account", Name: "平台账号登录", Mode: "ACCOUNT", ProviderID: "platform-account"}, {ID: "demo-oauth", Name: "演示 OAuth", Mode: "OAUTH", ProviderID: "demo-oauth"}, {ID: "business-account", Name: "业务账号登录", Mode: "ACCOUNT", ProviderID: "business-account"}, {ID: "anonymous", Name: "匿名只读入口", Mode: "NONE", Permissions: []string{"query"}}}, Bindings: []AuthBinding{{TargetType: "API", TargetID: "platform", PolicyID: "platform-account"}}}
	ids, _ := s.sourceIDs()
	for _, id := range ids {
		cfg.Bindings = append(cfg.Bindings, AuthBinding{TargetType: "DATABASE", TargetID: id, PolicyID: "demo-oauth"})
	}
	return s.SaveAuthConfig(context.Background(), cfg)
}
func (s *Store) AuthConfig(ctx context.Context) (AuthConfig, error) {
	cfg := AuthConfig{Providers: []AuthProvider{}, Policies: []AuthPolicy{}, Bindings: []AuthBinding{}}
	if e := s.DB.QueryRowContext(ctx, "SELECT REVISION_,DEFAULT_POLICY_ID_ FROM AUTH_SETTING WHERE ID_=1").Scan(&cfg.Revision, &cfg.DefaultPolicyID); e != nil {
		return cfg, e
	}
	for _, table := range []string{"AUTH_PROVIDER", "AUTH_POLICY"} {
		rows, e := s.DB.QueryContext(ctx, "SELECT CONFIG_JSON_ FROM "+table+" ORDER BY ID_")
		if e != nil {
			return cfg, e
		}
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return cfg, e
			}
			if table == "AUTH_PROVIDER" {
				var p AuthProvider
				if e = json.Unmarshal([]byte(raw), &p); e != nil {
					return cfg, e
				}
				cfg.Providers = append(cfg.Providers, p)
			} else {
				var p AuthPolicy
				if e = json.Unmarshal([]byte(raw), &p); e != nil {
					return cfg, e
				}
				cfg.Policies = append(cfg.Policies, p)
			}
		}
		rows.Close()
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT TARGET_TYPE_,TARGET_ID_,POLICY_ID_ FROM AUTH_BINDING ORDER BY TARGET_TYPE_,TARGET_ID_")
	if e != nil {
		return cfg, e
	}
	defer rows.Close()
	for rows.Next() {
		var b AuthBinding
		if e = rows.Scan(&b.TargetType, &b.TargetID, &b.PolicyID); e != nil {
			return cfg, e
		}
		cfg.Bindings = append(cfg.Bindings, b)
	}
	return cfg, rows.Err()
}
func authURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}
func validateAuth(cfg AuthConfig) error {
	providers := map[string]AuthProvider{}
	for _, p := range cfg.Providers {
		if !sourceID.MatchString(p.ID) || p.Name == "" || !slices.Contains([]string{"PLATFORM_ACCOUNT", "BUSINESS_ACCOUNT", "DEMO_OAUTH", "ENTERPRISE_OAUTH", "ENTERPRISE_ACCOUNT_API"}, p.Type) {
			return fmt.Errorf("认证提供方无效: %s", p.ID)
		}
		if _, ok := providers[p.ID]; ok {
			return fmt.Errorf("认证提供方重复")
		}
		if p.Type == "ENTERPRISE_OAUTH" {
			for _, u := range []string{p.Issuer, p.AuthorizeURL, p.TokenURL, p.UserInfoURL} {
				if !authURL(u) {
					return fmt.Errorf("企业 OAuth 需要 HTTPS 或本机测试的 issuer/授权/令牌/用户信息地址")
				}
			}
			if p.ClientID == "" {
				return fmt.Errorf("企业 OAuth 需要 client_id")
			}
		}
		if p.Type == "ENTERPRISE_ACCOUNT_API" && !authURL(p.AccountURL) {
			return fmt.Errorf("企业账号认证接口地址无效")
		}
		if p.SecretEnv != "" && !regexpEnv.MatchString(p.SecretEnv) {
			return fmt.Errorf("认证密钥需使用 DATASCOPE_AUTH_ 开头的环境变量")
		}
		providers[p.ID] = p
	}
	policies := map[string]AuthPolicy{}
	for _, p := range cfg.Policies {
		if !sourceID.MatchString(p.ID) || p.Name == "" || !slices.Contains([]string{"ACCOUNT", "OAUTH", "NONE"}, p.Mode) {
			return fmt.Errorf("认证策略无效")
		}
		if _, ok := policies[p.ID]; ok {
			return fmt.Errorf("认证策略重复")
		}
		provider, ok := providers[p.ProviderID]
		if p.Mode != "NONE" {
			if !ok || !provider.Enabled {
				return fmt.Errorf("策略引用不可用的认证提供方")
			}
			if p.Mode == "ACCOUNT" && !slices.Contains([]string{"PLATFORM_ACCOUNT", "BUSINESS_ACCOUNT", "ENTERPRISE_ACCOUNT_API"}, provider.Type) || p.Mode == "OAUTH" && !slices.Contains([]string{"DEMO_OAUTH", "ENTERPRISE_OAUTH"}, provider.Type) {
				return fmt.Errorf("认证方式与提供方类型不匹配")
			}
		}
		for _, cap := range p.Permissions {
			if !slices.Contains(BusinessPermissions, cap) {
				return fmt.Errorf("未知业务能力 %s", cap)
			}
		}
		policies[p.ID] = p
	}
	if p, ok := policies[cfg.DefaultPolicyID]; ok && providers[p.ProviderID].Type == "PLATFORM_ACCOUNT" {
		return fmt.Errorf("业务默认认证不能使用平台账号")
	}
	if _, ok := policies[cfg.DefaultPolicyID]; !ok {
		return fmt.Errorf("需要有效的全局默认认证策略")
	}
	seen := map[string]bool{}
	for _, b := range cfg.Bindings {
		if !slices.Contains([]string{"API", "DATABASE"}, b.TargetType) || b.TargetID == "" {
			return fmt.Errorf("认证绑定目标无效")
		}
		p, ok := policies[b.PolicyID]
		if !ok || seen[b.TargetType+"/"+b.TargetID] {
			return fmt.Errorf("认证绑定重复或策略不存在")
		}
		if b.TargetType == "API" && (p.Mode == "NONE" || providers[p.ProviderID].Type == "BUSINESS_ACCOUNT" || providers[p.ProviderID].Type == "DEMO_OAUTH") {
			return fmt.Errorf("平台管理入口需要平台账号或企业认证")
		}
		if b.TargetType == "DATABASE" && providers[p.ProviderID].Type == "PLATFORM_ACCOUNT" {
			return fmt.Errorf("业务数据库不能使用平台管理账号认证")
		}
		seen[b.TargetType+"/"+b.TargetID] = true
	}
	if !seen["API/platform"] {
		return fmt.Errorf("必须保留平台管理入口的认证绑定")
	}
	return nil
}

var regexpEnv = regexp.MustCompile(`^DATASCOPE_AUTH_[A-Z0-9_]+$`)

func (s *Store) SaveAuthConfig(ctx context.Context, cfg AuthConfig) error {
	if e := validateAuth(cfg); e != nil {
		return e
	}
	return s.Change(ctx, "authentication.save", "", map[string]any{"revision": cfg.Revision + 1}, func(tx *sql.Tx) error {
		var revision int
		e := tx.QueryRowContext(ctx, "SELECT REVISION_ FROM AUTH_SETTING WHERE ID_=1").Scan(&revision)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if revision != cfg.Revision {
			return fmt.Errorf("认证配置已变更，请刷新后重试")
		}
		for _, table := range []string{"AUTH_BINDING", "AUTH_POLICY", "AUTH_PROVIDER"} {
			if _, e = tx.ExecContext(ctx, "DELETE FROM "+table); e != nil {
				return e
			}
		}
		for _, p := range cfg.Providers {
			raw, _ := json.Marshal(p)
			if _, e = tx.ExecContext(ctx, "INSERT INTO AUTH_PROVIDER VALUES(?,?,?,?)", p.ID, p.Name, p.Type, string(raw)); e != nil {
				return e
			}
		}
		for _, p := range cfg.Policies {
			raw, _ := json.Marshal(p)
			if _, e = tx.ExecContext(ctx, "INSERT INTO AUTH_POLICY VALUES(?,?,?,?,?)", p.ID, p.Name, p.Mode, p.ProviderID, string(raw)); e != nil {
				return e
			}
		}
		for _, b := range cfg.Bindings {
			if _, e = tx.ExecContext(ctx, "INSERT INTO AUTH_BINDING VALUES(?,?,?)", b.TargetType, b.TargetID, b.PolicyID); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO AUTH_SETTING VALUES(1,?,?) ON CONFLICT(ID_) DO UPDATE SET REVISION_=excluded.REVISION_,DEFAULT_POLICY_ID_=excluded.DEFAULT_POLICY_ID_", revision+1, cfg.DefaultPolicyID)
		return e
	})
}
func (s *Store) ResolveAuth(ctx context.Context, kind, id string) (ResolvedAuth, error) {
	cfg, e := s.AuthConfig(ctx)
	if e != nil {
		return ResolvedAuth{}, e
	}
	key := cfg.DefaultPolicyID
	if kind == "API" {
		for _, b := range cfg.Bindings {
			if b.TargetType == "API" && b.TargetID == "platform" {
				key = b.PolicyID
				break
			}
		}
	}
	for _, b := range cfg.Bindings {
		if b.TargetType == kind && b.TargetID == id {
			key = b.PolicyID
			break
		}
	}
	out := ResolvedAuth{Revision: cfg.Revision}
	for _, p := range cfg.Policies {
		if p.ID == key {
			out.Policy = p
			break
		}
	}
	for _, p := range cfg.Providers {
		if out.Policy.Mode != "NONE" && p.ID == out.Policy.ProviderID {
			out.Provider = p
			break
		}
	}
	if out.Policy.ID == "" {
		return out, ErrDenied
	}
	return out, nil
}
func (r ResolvedAuth) Fingerprint() string {
	b, _ := json.Marshal(r)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func (r ResolvedAuth) Local() bool {
	return r.Provider.Type == "BUSINESS_ACCOUNT" || r.Provider.Type == "DEMO_OAUTH"
}
func externalID(provider, subject string) int64 {
	sum := sha256.Sum256([]byte(provider + "/" + subject))
	return int64(binary.BigEndian.Uint64(sum[:8])&((1<<52)-1)) + 1
}
func (r ResolvedAuth) Identity(database string, claims map[string]any) (BusinessAccount, error) {
	subjectKey := r.Provider.SubjectClaim
	if subjectKey == "" {
		subjectKey = "sub"
	}
	subject := claimString(claims[subjectKey])
	if subject == "" {
		return BusinessAccount{}, ErrDenied
	}
	key := r.Provider.BusinessIDClaim
	if key == "" {
		key = "employee_id"
	}
	value := claimString(claims[key])
	if database == "" && value == "" {
		value = "1"
	}
	business, e := strconv.ParseInt(value, 10, 64)
	if e != nil || business <= 0 {
		return BusinessAccount{}, fmt.Errorf("企业身份缺少有效业务员工编号")
	}
	name := claimString(claims["name"])
	a := BusinessAccount{ID: externalID(r.Provider.ID, subject), DatabaseID: database, Username: subject, Name: name, BusinessUserID: business, Permissions: r.Policy.Permissions, Active: true, AuthVersion: int64(r.Revision)}
	if r.Provider.UsernameClaim != "" {
		name := claimString(claims[r.Provider.UsernameClaim])
		if name == "" {
			return BusinessAccount{}, ErrDenied
		}
		a.Username = name
	}
	return a, nil
}
func claimString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func providerRequest(ctx context.Context, p AuthProvider, endpoint, method, token string, body io.Reader) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return nil, e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if p.SecretEnv != "" {
		secret := os.Getenv(p.SecretEnv)
		if secret == "" {
			return nil, fmt.Errorf("认证接口密钥未配置")
		}
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("企业认证服务暂不可用")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, ErrDenied
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 1024*1024))
	dec.UseNumber()
	var claims map[string]any
	if e = dec.Decode(&claims); e != nil {
		return nil, ErrDenied
	}
	if active, exists := claims["active"]; exists && active != true {
		return nil, ErrDenied
	}
	return claims, nil
}
func (r ResolvedAuth) VerifyBearer(ctx context.Context, database, token string) (BusinessAccount, error) {
	if r.Policy.Mode != "OAUTH" || r.Provider.Type != "ENTERPRISE_OAUTH" || token == "" {
		return BusinessAccount{}, ErrDenied
	}
	claims, e := providerRequest(ctx, r.Provider, r.Provider.UserInfoURL, "GET", token, nil)
	if e != nil {
		return BusinessAccount{}, e
	}
	return r.Identity(database, claims)
}
func (s *Store) VerifyAccount(ctx context.Context, r ResolvedAuth, database, username, password string) (BusinessAccount, error) {
	if r.Policy.Mode != "ACCOUNT" {
		return BusinessAccount{}, ErrDenied
	}
	if r.Provider.Type == "BUSINESS_ACCOUNT" {
		return s.AuthenticateBusiness(ctx, database, username, password)
	}
	if r.Provider.Type != "ENTERPRISE_ACCOUNT_API" {
		return BusinessAccount{}, ErrDenied
	}
	raw, _ := json.Marshal(map[string]string{"username": username, "password": password})
	claims, e := providerRequest(ctx, r.Provider, r.Provider.AccountURL, "POST", "", strings.NewReader(string(raw)))
	if e != nil {
		return BusinessAccount{}, e
	}
	return r.Identity(database, claims)
}

// PlatformIdentity grants no rights from remote claims. The enterprise subject
// must already have an enabled local platform account with configured grants.
func (s *Store) PlatformIdentity(ctx context.Context, p ResolvedAuth, a BusinessAccount) (User, error) {
	name := a.Username
	u, e := s.ByUsername(ctx, name)
	if e != nil || !u.Active {
		return User{}, ErrDenied
	}
	return u, nil
}

// ExchangeCode uses PKCE and a fixed server callback; claims come only from
// the configured provider's authenticated userinfo response.
func (r ResolvedAuth) ExchangeCode(ctx context.Context, database, code, verifier, redirect string) (BusinessAccount, string, error) {
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "client_id": {r.Provider.ClientID}}
	if r.Provider.SecretEnv != "" {
		secret := os.Getenv(r.Provider.SecretEnv)
		if secret == "" {
			return BusinessAccount{}, "", ErrDenied
		}
		values.Set("client_secret", secret)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", r.Provider.TokenURL, strings.NewReader(values.Encode()))
	if e != nil {
		return BusinessAccount{}, "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return BusinessAccount{}, "", ErrDenied
	}
	defer res.Body.Close()
	var token struct {
		Access string `json:"access_token"`
		Type   string `json:"token_type"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&token) != nil || !strings.EqualFold(token.Type, "Bearer") {
		return BusinessAccount{}, "", ErrDenied
	}
	a, e := r.VerifyBearer(ctx, database, token.Access)
	return a, token.Access, e
}
