package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// GetManifest generates a dynamic manifest for the given serial number.
// The manifest aggregates software assignments from site, tags, and direct agent assignments.
// Format: Plist for Munki (macOS), YAML for CIMIAN (Windows).
func (h *Handler) GetManifest(c echo.Context) error {
	serial := c.Param("serial")
	if serial == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "serial number required")
	}
	// Strip file extensions (CIMIAN appends .yaml, Munki may append .plist)
	serial = strings.TrimSuffix(serial, ".yaml")
	serial = strings.TrimSuffix(serial, ".plist")

	// Verify mTLS client certificate and validate CN matches the requested serial
	certAgentID, err := h.extractAgentID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	if certAgentID != serial {
		return echo.NewHTTPError(http.StatusForbidden, "certificate CN does not match requested manifest")
	}

	log.Printf("[REPO]: manifest request for agent %s", serial)

	// Determine platform from User-Agent header
	platform := h.detectPlatform(c)

	// Look up the agent by serial (which is the agent UUID when Munki uses
	// UseClientCertificateCNAsClientIdentifier=True, sending the cert CN as identifier)
	agent, err := h.Model.GetAgentWithRelations(serial)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "agent not found")
	}

	// Collect all assignments for this agent (site + tags + direct), filtered by platform
	assignments, err := h.Model.GetEffectiveAssignments(agent, platform)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not resolve assignments")
	}

	// Get the catalogs (rings) assigned to this agent
	catalogs, err := h.Model.GetAgentCatalogs(agent)
	if err != nil {
		catalogs = []string{"broad"}
	}

	// Build manifest data
	manifest := buildManifest(serial, catalogs, assignments)

	// Return in appropriate format
	switch platform {
	case "darwin":
		return c.Blob(http.StatusOK, "application/xml", manifest.ToPlist())
	case "windows":
		return c.Blob(http.StatusOK, "application/x-yaml", manifest.ToYAML())
	default:
		return c.Blob(http.StatusOK, "application/xml", manifest.ToPlist())
	}
}

// extractAgentID gets the agent ID from the mTLS client certificate CN.
func (h *Handler) extractAgentID(c echo.Context) (string, error) {
	if c.Request().TLS == nil || len(c.Request().TLS.PeerCertificates) == 0 {
		return "", fmt.Errorf("no client certificate provided")
	}
	cert := c.Request().TLS.PeerCertificates[0]
	cn := cert.Subject.CommonName
	if cn == "" {
		return "", fmt.Errorf("client certificate has no common name")
	}
	return cn, nil
}

// extractTenantID gets the tenant ID from the mTLS client certificate OU field.
func (h *Handler) extractTenantID(c echo.Context) (string, error) {
	if c.Request().TLS == nil || len(c.Request().TLS.PeerCertificates) == 0 {
		return "", fmt.Errorf("no client certificate provided")
	}
	cert := c.Request().TLS.PeerCertificates[0]
	if len(cert.Subject.OrganizationalUnit) == 0 || cert.Subject.OrganizationalUnit[0] == "" {
		return "", fmt.Errorf("client certificate has no organizational unit (tenant ID)")
	}
	return cert.Subject.OrganizationalUnit[0], nil
}

// detectPlatform determines the client platform from the User-Agent header.
func (h *Handler) detectPlatform(c echo.Context) string {
	ua := strings.ToLower(c.Request().UserAgent())
	switch {
	case strings.Contains(ua, "darwin") || strings.Contains(ua, "munki") || strings.Contains(ua, "macos"):
		return "darwin"
	case strings.Contains(ua, "windows") || strings.Contains(ua, "cimian"):
		return "windows"
	default:
		return "darwin"
	}
}

// HealthCheck returns a simple OK response.
func (h *Handler) HealthCheck(c echo.Context) error {
	return c.String(http.StatusOK, "OK")
}
