package catalog

import (
	"encoding/json"
	"fmt"
	"reflect"

	"datascope-server/internal/store"
)

// MergeColumnMasking accepts managed column properties only. Client-supplied
// schema, annotations, discovery reports and versions can never replace them.
func MergeColumnMasking(current, submitted *Discovery) (*Discovery, bool, error) {
	if submitted == nil {
		return current, false, nil
	}
	if current == nil {
		return nil, false, fmt.Errorf("请先登记表结构")
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, false, err
	}
	var next Discovery
	if err = json.Unmarshal(raw, &next); err != nil {
		return nil, false, err
	}
	tables := map[string]map[string]*store.Column{}
	for i := range next.Tables {
		t := &next.Tables[i]
		cols := map[string]*store.Column{}
		for j := range t.Columns {
			cols[t.Columns[j].Name] = &t.Columns[j]
		}
		tables[t.Name] = cols
	}
	seen := map[string]bool{}
	for _, t := range submitted.Tables {
		cols, ok := tables[t.Name]
		if !ok {
			return nil, false, fmt.Errorf("表 %s 未登记", t.Name)
		}
		if seen[t.Name] {
			return nil, false, fmt.Errorf("表配置重复")
		}
		seen[t.Name] = true
		fields := map[string]bool{}
		for _, col := range t.Columns {
			target, ok := cols[col.Name]
			if !ok {
				return nil, false, fmt.Errorf("字段 %s.%s 未登记", t.Name, col.Name)
			}
			if fields[col.Name] {
				return nil, false, fmt.Errorf("字段配置重复")
			}
			fields[col.Name] = true
			if err := col.Masking.Validate(); err != nil {
				return nil, false, fmt.Errorf("字段 %s.%s: %w", t.Name, col.Name, err)
			}
			target.Masking = col.Masking
			if target.Masking != nil && target.Masking.Type == "none" {
				target.Masking = nil
			}
		}
	}
	// Compare only managed properties, so runtime discovery timestamps do not
	// turn a no-op into a configuration mutation.
	changed := false
	for i, t := range current.Tables {
		for j, c := range t.Columns {
			if !reflect.DeepEqual(c.Masking, next.Tables[i].Columns[j].Masking) {
				changed = true
			}
		}
	}
	if !changed {
		return current, false, nil
	}
	return &next, true, nil
}

func hasHMAC(d *Discovery) bool {
	if d != nil {
		for _, t := range d.Tables {
			for _, c := range t.Columns {
				if c.Masking != nil && c.Masking.Type == "hmac" {
					return true
				}
			}
		}
	}
	return false
}

// Source discovery owns physical attributes, while managed rules follow only
// exact surviving identities. A deletion/rename cannot transfer a rule.
func mergeDiscoveredColumns(old, live []store.TableSchema) []store.TableSchema {
	rules := map[string]map[string]*store.Masking{}
	for _, t := range old {
		cols := map[string]*store.Masking{}
		for _, c := range t.Columns {
			cols[c.Name] = c.Masking
		}
		rules[t.Name] = cols
	}
	next := make([]store.TableSchema, len(live))
	for i, t := range live {
		next[i] = t
		next[i].Columns = append([]store.Column(nil), t.Columns...)
		for j := range next[i].Columns {
			c := &next[i].Columns[j]
			c.Masking = rules[t.Name][c.Name]
		}
	}
	return next
}
