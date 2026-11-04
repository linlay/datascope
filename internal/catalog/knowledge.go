package catalog

import "slices"

func (d Database) KnowledgeAvailable() bool {
	if d.KnowledgeEnabled != nil {
		return *d.KnowledgeEnabled
	}
	return slices.Contains(d.Tools, "search_knowledge") || slices.Contains(d.Tools, "read_knowledge")
}
