package views

import (
	"strings"
	"testing"
)

func TestTemplates(t *testing.T) {
	items := Templates()
	if len(items) != 4 || items[0].ID != "securities_account" {
		t.Fatal("missing securities views")
	}
	for _, v := range items {
		if err := v.Validate(); err != nil {
			t.Fatal(v.ID, err)
		}
		if strings.Contains(v.HTML, "<!--shared-->") || strings.Contains(v.HTML, "<!--securities-->") {
			t.Fatal("unresolved assets", v.ID)
		}
	}
}
