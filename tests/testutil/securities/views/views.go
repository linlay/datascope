// Package views contains opt-in views for the fictional securities example.
package views

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"datascope-server/internal/view"
)

func Templates() []view.Definition {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../examples/securities/views.yml"))
	if err != nil {
		panic(err)
	}
	var data struct {
		Views []view.Definition `yaml:"views"`
	}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		panic(err)
	}
	out := []view.Definition{}
	for _, v := range data.Views {
		if strings.HasPrefix(v.ID, "securities_") {
			out = append(out, v)
		}
	}
	return out
}
