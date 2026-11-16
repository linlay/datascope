package catalog

import "slices"

var ActionTools = []string{"list_actions", "describe_action", "invoke_action"}

func (d *Database) SetActionTools(enabled bool) {
	d.Tools = slices.DeleteFunc(slices.Clone(d.Tools), func(name string) bool { return slices.Contains(ActionTools, name) })
	if enabled {
		d.Tools = append(d.Tools, ActionTools...)
	}
}
func (d Database) ViewsAvailable() bool {
	if d.ViewsEnabled != nil {
		return *d.ViewsEnabled
	}
	for _, v := range d.Views {
		if v.Published {
			return true
		}
	}
	return false
}
