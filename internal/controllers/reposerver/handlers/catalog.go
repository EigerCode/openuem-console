package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// CatalogItem represents a single package entry in a Munki/CIMIAN catalog.
type CatalogItem struct {
	Name                  string   `yaml:"name" plist:"name"`
	Version               string   `yaml:"version" plist:"version"`
	DisplayName           string   `yaml:"display_name,omitempty" plist:"display_name"`
	Description           string   `yaml:"description,omitempty" plist:"description"`
	Category              string   `yaml:"category,omitempty" plist:"category"`
	Developer             string   `yaml:"developer,omitempty" plist:"developer"`
	InstallerItemLocation string   `yaml:"installer_item_location" plist:"installer_item_location"`
	InstallerItemHash     string   `yaml:"installer_item_hash,omitempty" plist:"installer_item_hash"`
	InstallerItemSize     int64    `yaml:"installer_item_size,omitempty" plist:"installer_item_size"`
	InstallerType         string   `yaml:"installer_type,omitempty" plist:"installer_type"`
	MinOSVersion          string   `yaml:"minimum_os_version,omitempty" plist:"minimum_os_version"`
	MaxOSVersion          string   `yaml:"maximum_os_version,omitempty" plist:"maximum_os_version"`
	RestartAction         string   `yaml:"RestartAction,omitempty" plist:"RestartAction"`
	UnattendedInstall     bool     `yaml:"unattended_install,omitempty" plist:"unattended_install"`
	UnattendedUninstall   bool     `yaml:"unattended_uninstall,omitempty" plist:"unattended_uninstall"`
	BlockingApplications  []string `yaml:"blocking_applications,omitempty" plist:"blocking_applications"`
	SupportedArch         []string `yaml:"supported_architectures,omitempty" plist:"supported_architectures"`
}

// GetCatalog generates a catalog for the given rollout ring.
// The catalog includes pre-signed S3 URLs in installer_item_location.
func (h *Handler) GetCatalog(c echo.Context) error {
	ring := c.Param("ring")
	if ring == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "ring name required")
	}

	// Validate ring name
	validRings := map[string]bool{"test": true, "first": true, "fast": true, "broad": true}
	if !validRings[ring] {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid ring: must be test, first, fast, or broad")
	}

	// Extract tenant from mTLS cert
	agentID, err := h.extractAgentID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}

	log.Printf("[REPO]: catalog request from agent %s for ring %s", agentID, ring)

	// Get tenant ID for this agent
	tenantID, err := h.Model.GetAgentTenantID(agentID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "agent tenant not found")
	}

	// Get all packages in this ring (merged: tenant packages override global)
	packages, err := h.Model.GetCatalogPackages(tenantID, ring)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not generate catalog")
	}

	// Generate pre-signed URLs for each package
	ctx := context.Background()
	items := make([]CatalogItem, 0, len(packages))
	for _, pkg := range packages {
		presignedURL, err := h.Model.GetPresignedURL(ctx, tenantID, pkg.InstallerPath, pkg.RepoType)
		if err != nil {
			log.Printf("[REPO]: could not generate pre-signed URL for %s: %v", pkg.Name, err)
			continue
		}

		item := CatalogItem{
			Name:                  pkg.Name,
			Version:               pkg.Version,
			DisplayName:           pkg.DisplayName,
			Description:           pkg.Description,
			Category:              pkg.Category,
			Developer:             pkg.Developer,
			InstallerItemLocation: presignedURL,
			InstallerItemHash:     pkg.ChecksumSHA256,
			InstallerItemSize:     pkg.SizeBytes / 1024, // Munki expects KB
			InstallerType:         pkg.InstallerType,
			MinOSVersion:          pkg.MinOSVersion,
			MaxOSVersion:          pkg.MaxOSVersion,
			UnattendedInstall:     pkg.UnattendedInstall,
			UnattendedUninstall:   pkg.UnattendedUninstall,
		}

		if pkg.RestartAction != "" && pkg.RestartAction != "none" {
			item.RestartAction = pkg.RestartAction
		}

		items = append(items, item)
	}

	// Detect platform and return appropriate format
	platform := h.detectPlatform(c)
	switch platform {
	case "darwin":
		return c.Blob(http.StatusOK, "application/xml", catalogToPlist(items))
	case "windows":
		return c.Blob(http.StatusOK, "application/x-yaml", catalogToYAML(items))
	default:
		return c.Blob(http.StatusOK, "application/xml", catalogToPlist(items))
	}
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
		b.WriteString("</dict>\n")
	}

	b.WriteString("</array>\n</plist>\n")
	return []byte(b.String())
}

// catalogToYAML serializes catalog items as CIMIAN-compatible YAML.
func catalogToYAML(items []CatalogItem) []byte {
	data, err := yaml.Marshal(items)
	if err != nil {
		return []byte("# error generating catalog\n")
	}
	return data
}

func writePlistString(b *strings.Builder, key, value string) {
	b.WriteString(fmt.Sprintf("\t<key>%s</key>\n\t<string>%s</string>\n", key, value))
}
