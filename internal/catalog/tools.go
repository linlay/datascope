package catalog

type ToolParameter struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description" yaml:"description"`
}
type BusinessTool struct {
	Name        string                   `json:"name" yaml:"name"`
	Description string                   `json:"description" yaml:"description"`
	SQL         string                   `json:"sql" yaml:"sql"`
	Parameters  map[string]ToolParameter `json:"parameters" yaml:"parameters"`
}
