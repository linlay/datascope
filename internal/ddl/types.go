// Package ddl plans and executes SQLite metadata changes independently of query and Action SQL.
package ddl

type Column struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	NotNull    bool    `json:"not_null"`
	Default    *string `json:"default,omitempty"`
	PrimaryKey int     `json:"primary_key"`
	Hidden     int     `json:"hidden"`
	SQL        string  `json:"sql,omitempty"`
}

type Definition struct {
	SQL          string   `json:"sql,omitempty"`
	Columns      []Column `json:"columns,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	Unique       bool     `json:"unique,omitempty"`
	Where        string   `json:"where,omitempty"`
	Strict       bool     `json:"strict,omitempty"`
	WithoutRowID bool     `json:"without_rowid,omitempty"`
}

type Object struct {
	ObjectType   string     `json:"object_type"`
	Name         string     `json:"name"`
	ParentName   string     `json:"parent_name"`
	SQL          string     `json:"sql"`
	Definition   Definition `json:"definition"`
	Dependencies []string   `json:"dependencies"`
}

type Change struct {
	Operation  string     `json:"operation"`
	ObjectType string     `json:"object_type"`
	Name       string     `json:"name"`
	ParentName string     `json:"parent_name,omitempty"`
	NewName    string     `json:"new_name,omitempty"`
	Definition Definition `json:"definition"`
}

type Difference struct {
	ObjectType string `json:"object_type"`
	Name       string `json:"name"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

type Plan struct {
	ID                string       `json:"plan_id"`
	DatabaseID        string       `json:"database_id"`
	ActorID           int64        `json:"actor_id"`
	Revision          int          `json:"revision"`
	FileIdentity      string       `json:"file_identity"`
	CreatedAt         string       `json:"created_at"`
	BeforeFingerprint string       `json:"before_fingerprint"`
	AfterFingerprint  string       `json:"after_fingerprint"`
	SQL               []string     `json:"sql"`
	Diff              []Difference `json:"diff"`
	Warnings          []string     `json:"warnings"`
	RequiresRebuild   bool         `json:"requires_rebuild"`
}

type Execution struct {
	ID              string       `json:"execution_id"`
	PlanID          string       `json:"plan_id"`
	RequestID       string       `json:"request_id"`
	DatabaseID      string       `json:"database_id"`
	ActorID         int64        `json:"actor_id"`
	Status          string       `json:"status"`
	StartedAt       string       `json:"started_at"`
	FinishedAt      string       `json:"finished_at,omitempty"`
	Error           string       `json:"error,omitempty"`
	BackupID        string       `json:"backup_id,omitempty"`
	DiscoveryStatus string       `json:"discovery_status"`
	SQL             []string     `json:"sql"`
	Diff            []Difference `json:"diff"`
}
