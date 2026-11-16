package view

import (
	"strings"
	"testing"
)

func TestGenericTemplatesExcludeBusinessExamples(t *testing.T) {
	templates := Templates()
	if len(templates) != 2 {
		t.Fatalf("got %d generic templates", len(templates))
	}
	for _, v := range templates {
		if err := v.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, businessText := range []string{"证券", "securities", "fund_account", "customer_no", "880000"} {
			if strings.Contains(v.ID+v.Name+v.Description+v.HTML, businessText) {
				t.Fatalf("template %s contains business content %q", v.ID, businessText)
			}
		}
	}
}
