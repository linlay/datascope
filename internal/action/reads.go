package action

// UsesMain reports the same entry-point choice used by the runtime.
func (d Definition) UsesMain() bool { return hasMain(d.Script) }

// PrivilegedReads reports policy-bypassing SQL in the statically linked action,
// including reachable libraries. It never initializes modules or executes SQL.
func (d Definition) PrivilegedReads(libraries ...Library) (map[string]bool, error) {
	_, sites, err := compileWithLibraries(d.Script, libraries, "")
	if err != nil {
		return nil, err
	}
	reads := map[string]bool{}
	for _, site := range sites {
		if (site.scope == "db" && site.method != "exec") || (site.native && (site.method == "query" || site.method == "queryOne")) {
			reads[site.sql] = true
		}
	}
	return reads, nil
}
