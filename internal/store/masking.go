package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Masking is a managed property of a registered Column, never source metadata.
type Masking struct {
	Type       string `json:"type" yaml:"type"`
	KeepPrefix int    `json:"keep_prefix,omitempty" yaml:"keepPrefix,omitempty"`
	KeepSuffix int    `json:"keep_suffix,omitempty" yaml:"keepSuffix,omitempty"`
}

func (m *Masking) Active() bool { return m != nil && m.Type != "none" }
func (m *Masking) Validate() error {
	if m == nil {
		return nil
	}
	switch m.Type {
	case "none", "full", "omit", "hmac":
		if m.KeepPrefix != 0 || m.KeepSuffix != 0 {
			return fmt.Errorf("只有部分遮盖支持保留位数")
		}
	case "partial":
		if m.KeepPrefix < 0 || m.KeepSuffix < 0 || m.KeepPrefix > 100 || m.KeepSuffix > 100 {
			return fmt.Errorf("保留位数必须为 0 到 100")
		}
	default:
		return fmt.Errorf("不支持的脱敏规则 %q", m.Type)
	}
	return nil
}

func (m *Masking) apply(value any, key []byte, domain string) (any, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if !m.Active() || value == nil {
		return value, nil
	}
	switch m.Type {
	case "full":
		return "******", nil
	case "omit":
		return nil, nil
	case "partial":
		chars := []rune(fmt.Sprint(value))
		if len(chars) == 0 {
			return "", nil
		}
		if len(chars) <= m.KeepPrefix+m.KeepSuffix {
			return strings.Repeat("*", len(chars)), nil
		}
		return string(chars[:m.KeepPrefix]) + strings.Repeat("*", len(chars)-m.KeepPrefix-m.KeepSuffix) + string(chars[len(chars)-m.KeepSuffix:]), nil
	case "hmac":
		if len(key) < 32 {
			return nil, fmt.Errorf("哈希脱敏密钥不可用")
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(domain + "\x00" + fmt.Sprint(value)))
		return "h1:" + hex.EncodeToString(mac.Sum(nil)), nil
	}
	return nil, fmt.Errorf("脱敏规则无效")
}

func (s *Store) hasMasking() bool {
	for _, t := range s.RegisteredSchema {
		for _, c := range t.Columns {
			if c.Masking.Active() {
				return true
			}
		}
	}
	return false
}

type maskedColumn struct {
	name   string
	rule   *Masking
	domain string
}

func (s *Store) applyMasking(result *QueryResult, plan []maskedColumn) error {
	if plan == nil {
		return nil
	}
	if len(plan) != len(result.Columns) {
		return fmt.Errorf("查询结果与脱敏计划不一致，请检查表结构")
	}
	for _, c := range plan {
		if err := c.rule.Validate(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	columns := []string{}
	for i, name := range result.Columns {
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("查询结果存在同名字段，请使用不同别名")
		}
		seen[strings.ToLower(name)] = true
		rule := plan[i].rule
		if rule != nil && rule.Type == "omit" {
			for _, row := range result.Rows {
				delete(row, name)
			}
			continue
		}
		columns = append(columns, name)
		if rule.Active() {
			for _, row := range result.Rows {
				v, err := rule.apply(row[name], s.MaskingKey, plan[i].domain)
				if err != nil {
					return err
				}
				row[name] = v
			}
		}
	}
	result.Columns = columns
	return nil
}

func (s *Store) maskBrowse(result *BrowseResult) error {
	if !s.hasMasking() {
		return nil
	}
	var plan []maskedColumn
	for _, t := range s.RegisteredSchema {
		if strings.EqualFold(t.Name, result.Table) {
			for _, name := range result.Columns {
				found := false
				for _, c := range t.Columns {
					if c.Name == name {
						plan = append(plan, maskedColumn{name, c.Masking, t.Name + "\x00" + c.Name})
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("字段未登记，请先确认数据库结构变化")
				}
			}
			break
		}
	}
	if plan == nil {
		return fmt.Errorf("表未登记，无法生成脱敏计划")
	}
	q := &QueryResult{Columns: result.Columns, Rows: result.Rows}
	if err := s.applyMasking(q, plan); err != nil {
		return err
	}
	result.Columns, result.Rows = q.Columns, q.Rows
	return nil
}
