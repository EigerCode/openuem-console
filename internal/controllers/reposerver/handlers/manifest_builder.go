package handlers

import (
	"fmt"
	"strings"

	sd "github.com/open-uem/openuem-console/internal/common/softwaredeployment"
	"gopkg.in/yaml.v3"
)

// ManifestData represents a Munki/CIMIAN manifest.
type ManifestData struct {
	Catalogs          []string `yaml:"catalogs" plist:"catalogs"`
	ManagedInstalls   []string `yaml:"managed_installs,omitempty" plist:"managed_installs"`
	ManagedUninstalls []string `yaml:"managed_uninstalls,omitempty" plist:"managed_uninstalls"`
	ManagedUpdates    []string `yaml:"managed_updates,omitempty" plist:"managed_updates"`
	OptionalInstalls  []string `yaml:"optional_installs,omitempty" plist:"optional_installs"`
}

// buildManifest creates a ManifestData from assignments.
func buildManifest(serial string, catalogs []string, assignments []sd.AssignmentInfo) *ManifestData {
	m := &ManifestData{
		Catalogs: catalogs,
	}

	seen := make(map[string]bool)
	for _, a := range assignments {
		key := a.AssignmentType + ":" + a.PackageName
		if seen[key] {
			continue
		}
		seen[key] = true

		switch a.AssignmentType {
		case "managed_install":
			m.ManagedInstalls = append(m.ManagedInstalls, a.PackageName)
		case "managed_uninstall":
			m.ManagedUninstalls = append(m.ManagedUninstalls, a.PackageName)
		case "optional_install":
			m.OptionalInstalls = append(m.OptionalInstalls, a.PackageName)
		case "managed_update":
			m.ManagedUpdates = append(m.ManagedUpdates, a.PackageName)
		}
	}

	return m
}

// ToPlist serializes the manifest as a Munki-compatible plist.
func (m *ManifestData) ToPlist() []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">`)
	b.WriteString("\n<dict>\n")

	writePlistArray(&b, "catalogs", m.Catalogs)
	writePlistArray(&b, "managed_installs", m.ManagedInstalls)
	writePlistArray(&b, "managed_uninstalls", m.ManagedUninstalls)
	writePlistArray(&b, "managed_updates", m.ManagedUpdates)
	writePlistArray(&b, "optional_installs", m.OptionalInstalls)

	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

// ToYAML serializes the manifest as CIMIAN-compatible YAML.
func (m *ManifestData) ToYAML() []byte {
	data, err := yaml.Marshal(m)
	if err != nil {
		return []byte("# error generating manifest\n")
	}
	return data
}

func writePlistArray(b *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("\t<key>%s</key>\n", key))
	b.WriteString("\t<array>\n")
	for _, item := range items {
		b.WriteString(fmt.Sprintf("\t\t<string>%s</string>\n", item))
	}
	b.WriteString("\t</array>\n")
}
