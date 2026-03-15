package handlers

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// CatalogItem represents a single package entry in a Munki/CIMIAN catalog.
type CatalogItem struct {
	Name                  string           `yaml:"name" plist:"name"`
	Version               string           `yaml:"version" plist:"version"`
	DisplayName           string           `yaml:"display_name,omitempty" plist:"display_name"`
	Description           string           `yaml:"description,omitempty" plist:"description"`
	Category              string           `yaml:"category,omitempty" plist:"category"`
	Developer             string           `yaml:"developer,omitempty" plist:"developer"`
	IconName              string           `yaml:"icon_name,omitempty" plist:"icon_name"`
	InstallerItemLocation string           `yaml:"installer_item_location" plist:"installer_item_location"`
	InstallerItemHash     string           `yaml:"installer_item_hash,omitempty" plist:"installer_item_hash"`
	InstallerItemSize     int64            `yaml:"installer_item_size,omitempty" plist:"installer_item_size"`
	InstallerType         string           `yaml:"installer_type,omitempty" plist:"installer_type"`
	MinOSVersion          string           `yaml:"minimum_os_version,omitempty" plist:"minimum_os_version"`
	MaxOSVersion          string           `yaml:"maximum_os_version,omitempty" plist:"maximum_os_version"`
	RestartAction         string           `yaml:"RestartAction,omitempty" plist:"RestartAction"`
	UnattendedInstall     bool             `yaml:"unattended_install,omitempty" plist:"unattended_install"`
	UnattendedUninstall   bool             `yaml:"unattended_uninstall,omitempty" plist:"unattended_uninstall"`
	BlockingApplications  []string         `yaml:"blocking_applications,omitempty" plist:"blocking_applications"`
	SupportedArch         []string         `yaml:"supported_architectures,omitempty" plist:"supported_architectures"`
	Installs              []map[string]any `yaml:"installs,omitempty" plist:"installs"`
	Receipts              []map[string]any `yaml:"receipts,omitempty" plist:"receipts"`
	PreInstallScript      string           `yaml:"-" plist:"-"`
	PostInstallScript     string           `yaml:"-" plist:"-"`
}

// GetCatalog generates a catalog for the given rollout ring.
// The catalog includes pre-signed S3 URLs in installer_item_location.
func (h *Handler) GetCatalog(c echo.Context) error {
	catalog := c.Param("catalog")
	if catalog == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "catalog name required")
	}
	// Strip file extensions (CIMIAN appends .yaml, Munki may append .plist)
	catalog = strings.TrimSuffix(catalog, ".yaml")
	catalog = strings.TrimSuffix(catalog, ".plist")
	catalog = strings.ToLower(catalog)

	// Validate catalog name
	validCatalogs := map[string]bool{"test": true, "first": true, "fast": true, "broad": true}
	if !validCatalogs[catalog] {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid catalog: must be test, first, fast, or broad")
	}

	// Extract agent ID and tenant ID from mTLS client certificate
	agentID, err := h.extractAgentID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}

	var tenantID int
	tenantIDStr, err := h.extractTenantID(c)
	if err == nil {
		tenantID, err = strconv.Atoi(tenantIDStr)
	}
	if err != nil {
		// Fallback: try to resolve tenant from agent ID via database
		tenantID, err = h.Model.GetAgentTenantID(agentID)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "could not determine tenant")
		}
	}

	platform := h.detectPlatform(c)
	log.Printf("[REPO]: catalog request from agent %s for catalog %s (tenant %d, platform %s)", agentID, catalog, tenantID, platform)

	// Get all packages in this catalog (merged: tenant packages override global)
	packages, err := h.Model.GetCatalogPackages(tenantID, catalog, platform)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not generate catalog")
	}

	// Build catalog entries with relative paths for installer_item_location.
	// Munki fetches packages via /repo/pkgs/<installer_item_location>,
	// so the path must be relative — the /repo/pkgs handler generates
	// pre-signed S3 URLs and redirects at download time.
	var plistDicts []string
	var yamlDicts []string
	items := make([]CatalogItem, 0, len(packages))
	usePkginfoData := false

	for _, pkg := range packages {
		if pkg.PkginfoData != "" {
			usePkginfoData = true
			if platform == "windows" {
				// Inject installer location/hash/size into YAML pkginfo
				dict := injectYAMLCatalogFields(pkg.PkginfoData, pkg.InstallerPath, pkg.ChecksumSHA256, pkg.SizeBytes, pkg.IconName)
				yamlDicts = append(yamlDicts, dict)
			} else {
				// Inject runtime fields into the pkginfo plist dict
				dict := injectCatalogFields(pkg.PkginfoData, pkg.InstallerPath, pkg.ChecksumSHA256, pkg.SizeBytes)
				plistDicts = append(plistDicts, dict)
			}
		} else {
			// Fallback: build CatalogItem from structured fields
			item := CatalogItem{
				Name:                  pkg.Name,
				Version:               pkg.Version,
				DisplayName:           pkg.DisplayName,
				Description:           pkg.Description,
				Category:              pkg.Category,
				Developer:             pkg.Developer,
				IconName:              pkg.IconName,
				InstallerItemLocation: pkg.InstallerPath,
				InstallerItemHash:     pkg.ChecksumSHA256,
				InstallerItemSize:     pkg.SizeBytes / 1024, // Munki expects KB
				InstallerType:         mapToMunkiInstallerType(pkg.InstallerType),
				MinOSVersion:          pkg.MinOSVersion,
				MaxOSVersion:          pkg.MaxOSVersion,
				UnattendedInstall:     pkg.UnattendedInstall,
				UnattendedUninstall:   pkg.UnattendedUninstall,
			}

			item.PreInstallScript = pkg.PreInstallScript
			item.PostInstallScript = pkg.PostInstallScript

			if pkg.RestartAction != "" && pkg.RestartAction != "none" {
				item.RestartAction = pkg.RestartAction
			}

			if pkg.InstallsItems != "" {
				var installs []map[string]any
				if err := json.Unmarshal([]byte(pkg.InstallsItems), &installs); err == nil {
					item.Installs = installs
				}
			}
			if pkg.Receipts != "" {
				var receipts []map[string]any
				if err := json.Unmarshal([]byte(pkg.Receipts), &receipts); err == nil {
					item.Receipts = receipts
				}
			}
			if pkg.BlockingApps != "" {
				var apps []string
				if err := json.Unmarshal([]byte(pkg.BlockingApps), &apps); err == nil {
					item.BlockingApplications = apps
				}
			}
			if pkg.SupportedArchitectures != "" {
				var archs []string
				if err := json.Unmarshal([]byte(pkg.SupportedArchitectures), &archs); err == nil {
					item.SupportedArch = archs
				}
			}

			items = append(items, item)
		}
	}

	// Return in appropriate format
	switch platform {
	case "darwin":
		if usePkginfoData && len(plistDicts) > 0 {
			// Build catalog from pkginfo_data dicts + any legacy items
			return c.Blob(http.StatusOK, "application/xml", catalogFromPkginfoDicts(plistDicts, items))
		}
		return c.Blob(http.StatusOK, "application/xml", catalogToPlist(items))
	case "windows":
		if usePkginfoData && len(yamlDicts) > 0 {
			return c.Blob(http.StatusOK, "application/x-yaml", catalogFromYAMLDicts(yamlDicts, items))
		}
		return c.Blob(http.StatusOK, "application/x-yaml", catalogToYAML(items))
	default:
		if usePkginfoData && len(plistDicts) > 0 {
			return c.Blob(http.StatusOK, "application/xml", catalogFromPkginfoDicts(plistDicts, items))
		}
		return c.Blob(http.StatusOK, "application/xml", catalogToPlist(items))
	}
}

// injectCatalogFields takes a pkginfo plist XML (single <dict>) and injects
// installer_item_location, installer_item_hash, and installer_item_size.
// It inserts these keys just before the closing </dict> tag.
func injectCatalogFields(pkginfoData, presignedURL, checksumSHA256 string, sizeBytes int64) string {
	// Extract the <dict>...</dict> content from the plist
	dictStart := strings.Index(pkginfoData, "<dict>")
	dictEnd := strings.LastIndex(pkginfoData, "</dict>")
	if dictStart == -1 || dictEnd == -1 {
		return pkginfoData
	}

	var b strings.Builder
	// Write everything up to </dict>
	b.WriteString(pkginfoData[dictStart : dictEnd])
	// Inject runtime catalog fields
	writePlistString(&b, "installer_item_location", presignedURL)
	if checksumSHA256 != "" {
		writePlistString(&b, "installer_item_hash", checksumSHA256)
	}
	if sizeBytes > 0 {
		b.WriteString(fmt.Sprintf("\t<key>installer_item_size</key>\n\t<integer>%d</integer>\n", sizeBytes/1024))
	}
	b.WriteString("</dict>")
	return b.String()
}

// catalogFromPkginfoDicts builds a plist catalog from raw pkginfo dict strings
// plus any legacy CatalogItem entries.
func catalogFromPkginfoDicts(dicts []string, legacyItems []CatalogItem) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">`)
	b.WriteString("\n<array>\n")

	for _, dict := range dicts {
		b.WriteString(dict)
		b.WriteString("\n")
	}

	// Append any legacy items (packages without pkginfo_data)
	if len(legacyItems) > 0 {
		legacy := catalogToPlistInner(legacyItems)
		b.WriteString(legacy)
	}

	b.WriteString("</array>\n</plist>\n")
	return []byte(b.String())
}

// catalogToPlist serializes catalog items as a Munki-compatible plist array.
func catalogToPlist(items []CatalogItem) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">`)
	b.WriteString("\n<array>\n")
	b.WriteString(catalogToPlistInner(items))
	b.WriteString("</array>\n</plist>\n")
	return []byte(b.String())
}

// catalogToPlistInner generates the <dict> entries for catalog items without the plist wrapper.
func catalogToPlistInner(items []CatalogItem) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString("<dict>\n")
		writePlistString(&b, "name", item.Name)
		writePlistString(&b, "version", item.Version)
		if item.DisplayName != "" {
			writePlistString(&b, "display_name", item.DisplayName)
		}
		if item.Description != "" {
			writePlistString(&b, "description", item.Description)
		}
		if item.Category != "" {
			writePlistString(&b, "category", item.Category)
		}
		if item.Developer != "" {
			writePlistString(&b, "developer", item.Developer)
		}
		if item.IconName != "" {
			writePlistString(&b, "icon_name", item.IconName)
		}
		writePlistString(&b, "installer_item_location", item.InstallerItemLocation)
		if item.InstallerItemHash != "" {
			writePlistString(&b, "installer_item_hash", item.InstallerItemHash)
		}
		if item.InstallerItemSize > 0 {
			b.WriteString(fmt.Sprintf("\t<key>installer_item_size</key>\n\t<integer>%d</integer>\n", item.InstallerItemSize))
		}
		if item.InstallerType != "" {
			writePlistString(&b, "installer_type", item.InstallerType)
		}
		if item.MinOSVersion != "" {
			writePlistString(&b, "minimum_os_version", item.MinOSVersion)
		}
		if item.MaxOSVersion != "" {
			writePlistString(&b, "maximum_os_version", item.MaxOSVersion)
		}
		if item.RestartAction != "" {
			writePlistString(&b, "RestartAction", item.RestartAction)
		}
		if item.UnattendedInstall {
			b.WriteString("\t<key>unattended_install</key>\n\t<true/>\n")
		}
		if item.UnattendedUninstall {
			b.WriteString("\t<key>unattended_uninstall</key>\n\t<true/>\n")
		}
		if len(item.BlockingApplications) > 0 {
			writePlistArray(&b, "blocking_applications", item.BlockingApplications)
		}
		if len(item.SupportedArch) > 0 {
			writePlistArray(&b, "supported_architectures", item.SupportedArch)
		}
		if len(item.Installs) > 0 {
			writePlistDictArray(&b, "installs", item.Installs)
		}
		if len(item.Receipts) > 0 {
			writePlistDictArray(&b, "receipts", item.Receipts)
		}
		b.WriteString("</dict>\n")
	}
	return b.String()
}

// CimianCatalogItem represents a catalog item in CIMIAN's expected YAML format
// with a nested installer block instead of flat Munki-style fields.
type CimianCatalogItem struct {
	Name                 string           `yaml:"name"`
	Version              string           `yaml:"version"`
	DisplayName          string           `yaml:"display_name,omitempty"`
	Description          string           `yaml:"description,omitempty"`
	Category             string           `yaml:"category,omitempty"`
	Developer            string           `yaml:"developer,omitempty"`
	IconName             string           `yaml:"icon_name,omitempty"`
	Installer            CimianInstaller  `yaml:"installer"`
	Installs             []map[string]any `yaml:"installs,omitempty"`
	SupportedArch        []string         `yaml:"supported_architectures,omitempty"`
	MinOSVersion         string           `yaml:"minimum_os_version,omitempty"`
	MaxOSVersion         string           `yaml:"maximum_os_version,omitempty"`
	UnattendedInstall    bool             `yaml:"unattended_install"`
	UnattendedUninstall  bool             `yaml:"unattended_uninstall"`
	BlockingApplications []string         `yaml:"blocking_applications,omitempty"`
	PreinstallScript     string           `yaml:"preinstall_script,omitempty"`
	PostinstallScript    string           `yaml:"postinstall_script,omitempty"`
}

// CimianInstaller represents the nested installer block in CIMIAN's format.
type CimianInstaller struct {
	Type     string `yaml:"type"`
	Location string `yaml:"location"`
	Hash     string `yaml:"hash,omitempty"`
	Size     int64  `yaml:"size,omitempty"`
}

// injectYAMLCatalogFields takes a pkginfo YAML string and injects
// installer location, hash, and size into the nested installer block.
func injectYAMLCatalogFields(pkginfoData, installerPath, checksumSHA256 string, sizeBytes int64, iconName string) string {
	var item map[string]any
	if err := yaml.Unmarshal([]byte(pkginfoData), &item); err != nil {
		return pkginfoData
	}

	// Ensure installer block exists
	installer, ok := item["installer"].(map[string]any)
	if !ok {
		installer = map[string]any{}
	}
	installer["location"] = installerPath
	if checksumSHA256 != "" {
		installer["hash"] = checksumSHA256
	}
	if sizeBytes > 0 {
		installer["size"] = sizeBytes / 1024 // CIMIAN expects KB
	}
	item["installer"] = installer

	if iconName != "" {
		item["icon_name"] = iconName
	}

	data, err := yaml.Marshal(item)
	if err != nil {
		return pkginfoData
	}
	return string(data)
}

// catalogFromYAMLDicts builds a YAML catalog from raw pkginfo YAML strings
// plus any legacy CatalogItem entries.
func catalogFromYAMLDicts(dicts []string, legacyItems []CatalogItem) []byte {
	var allItems []map[string]any

	// Parse each pkginfo YAML dict
	for _, dict := range dicts {
		var item map[string]any
		if err := yaml.Unmarshal([]byte(dict), &item); err == nil {
			allItems = append(allItems, item)
		}
	}

	// Convert legacy items to map format and append
	if len(legacyItems) > 0 {
		legacyYAML := catalogToYAML(legacyItems)
		var legacyMaps []map[string]any
		if err := yaml.Unmarshal(legacyYAML, &legacyMaps); err == nil {
			allItems = append(allItems, legacyMaps...)
		}
	}

	data, err := yaml.Marshal(allItems)
	if err != nil {
		return []byte("# error generating catalog\n")
	}
	return data
}

// catalogToYAML serializes catalog items as CIMIAN-compatible YAML.
func catalogToYAML(items []CatalogItem) []byte {
	cimianItems := make([]CimianCatalogItem, 0, len(items))
	for _, item := range items {
		ci := CimianCatalogItem{
			Name:                 item.Name,
			Version:              item.Version,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			Category:             item.Category,
			Developer:            item.Developer,
			IconName:             item.IconName,
			Installer: CimianInstaller{
				Type:     item.InstallerType,
				Location: item.InstallerItemLocation,
				Hash:     item.InstallerItemHash,
				Size:     item.InstallerItemSize * 1024, // convert back from KB to bytes
			},
			Installs:             item.Installs,
			SupportedArch:        item.SupportedArch,
			MinOSVersion:         item.MinOSVersion,
			MaxOSVersion:         item.MaxOSVersion,
			UnattendedInstall:    item.UnattendedInstall,
			UnattendedUninstall:  item.UnattendedUninstall,
			BlockingApplications: item.BlockingApplications,
			PreinstallScript:     item.PreInstallScript,
			PostinstallScript:    item.PostInstallScript,
		}
		cimianItems = append(cimianItems, ci)
	}
	data, err := yaml.Marshal(cimianItems)
	if err != nil {
		return []byte("# error generating catalog\n")
	}
	return data
}

func writePlistString(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<string>%s</string>\n", html.EscapeString(key), html.EscapeString(value))
}

// writePlistDictArray writes an array of dicts to plist (for installs/receipts).
func writePlistDictArray(b *strings.Builder, key string, items []map[string]any) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<array>\n", html.EscapeString(key))
	for _, item := range items {
		b.WriteString("\t\t<dict>\n")
		for k, v := range item {
			escapedKey := html.EscapeString(k)
			switch val := v.(type) {
			case string:
				fmt.Fprintf(b, "\t\t\t<key>%s</key>\n\t\t\t<string>%s</string>\n", escapedKey, html.EscapeString(val))
			case float64:
				if val == float64(int64(val)) {
					fmt.Fprintf(b, "\t\t\t<key>%s</key>\n\t\t\t<integer>%d</integer>\n", escapedKey, int64(val))
				} else {
					fmt.Fprintf(b, "\t\t\t<key>%s</key>\n\t\t\t<real>%f</real>\n", escapedKey, val)
				}
			case bool:
				if val {
					fmt.Fprintf(b, "\t\t\t<key>%s</key>\n\t\t\t<true/>\n", escapedKey)
				} else {
					fmt.Fprintf(b, "\t\t\t<key>%s</key>\n\t\t\t<false/>\n", escapedKey)
				}
			}
		}
		b.WriteString("\t\t</dict>\n")
	}
	b.WriteString("\t</array>\n")
}

// mapToMunkiInstallerType converts internal file-type values to Munki-valid installer_type values.
func mapToMunkiInstallerType(fileType string) string {
	switch fileType {
	case "dmg":
		return "copy_from_dmg"
	case "pkg":
		// pkg is the Munki default — no installer_type needed
		return ""
	default:
		return fileType
	}
}
