// Package connectors embeds distributable connector sources in the admin binary.
package connectors

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
)

//go:embed datascope-admin
var packages embed.FS

// AdminArchive bundles the skill and config for this deployment. No user
// credentials are included; the importing client completes OAuth separately.
func AdminArchive(endpoint string) ([]byte, error) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	err := fs.WalkDir(packages, "datascope-admin", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := packages.ReadFile(path)
		if err != nil {
			return err
		}
		if path == "datascope-admin/mcp.json" && endpoint != "" {
			var config struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &config); err != nil {
				return err
			}
			config.Servers["main"]["url"] = endpoint
			data, err = json.MarshalIndent(config, "", "  ")
			if err != nil {
				return err
			}
		}
		file, err := archive.Create(path)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	})
	if err != nil {
		archive.Close()
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
