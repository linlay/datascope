package main

import (
	"datascope-server/internal/catalog"
	"reflect"
	"testing"
)

func TestMergePreservesCustomExamplesAndIsIdempotent(t *testing.T) {
	existing := []catalog.SQLExample{{Label: "Custom", SQL: "SELECT 42"}, {Label: "Demo", SQL: "SELECT 2"}}
	template := []catalog.SQLExample{{Label: "Demo", SQL: "SELECT 1"}, {Label: "Duplicate SQL", SQL: "SELECT 42"}, {Label: "Missing", SQL: "SELECT 3"}}
	got, added := mergeExamples(existing, template)
	want := append(append([]catalog.SQLExample(nil), existing...), template[2])
	if added != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %+v, added %d", got, added)
	}
	again, added := mergeExamples(got, template)
	if added != 0 || !reflect.DeepEqual(again, got) {
		t.Fatal("repeated restore changed examples")
	}
}
