package handlers

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/EigerCode/openuem-console/internal/views/admin_views"
	"github.com/EigerCode/openuem-console/internal/views/partials"
)

func (h *Handler) ListEnrollmentTokens(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	tokens, err := h.Model.GetEnrollmentTokens(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	sites, err := h.Model.GetSites(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	agentsExists, err := h.Model.AgentsExists(commonInfo)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	serversExists, err := h.Model.ServersExists()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	return RenderView(c, admin_views.EnrollmentTokensIndex(" | Enrollment",
		admin_views.EnrollmentTokens(c, tokens, sites, "", agentsExists, serversExists, commonInfo),
		commonInfo))
}

func (h *Handler) CreateEnrollmentToken(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), true))
	}

	description := c.FormValue("description")
	tokenValue := uuid.New().String()

	maxUses := 0
	if v := c.FormValue("max_uses"); v != "" {
		maxUses, _ = strconv.Atoi(v)
	}

	var siteID *int
	if v := c.FormValue("site_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err == nil && id > 0 {
			siteID = &id
		}
	}

	var expiresAt *time.Time
	if v := c.FormValue("expires_at"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err == nil {
			expiresAt = &t
		}
	}

	_, err = h.Model.CreateEnrollmentToken(tenantID, siteID, description, tokenValue, maxUses, expiresAt)
	if err != nil {
		log.Printf("[ERROR]: could not create enrollment token: %v", err)
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.ListEnrollmentTokens(c)
}

func (h *Handler) DeleteEnrollmentToken(c echo.Context) error {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", true))
	}

	err = h.Model.DeleteEnrollmentToken(tokenID)
	if err != nil {
		log.Printf("[ERROR]: could not delete enrollment token: %v", err)
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.ListEnrollmentTokens(c)
}

func (h *Handler) ToggleEnrollmentToken(c echo.Context) error {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", true))
	}

	active := c.FormValue("active") == "true"

	err = h.Model.ToggleEnrollmentToken(tokenID, active)
	if err != nil {
		log.Printf("[ERROR]: could not toggle enrollment token: %v", err)
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.ListEnrollmentTokens(c)
}

func (h *Handler) DownloadInstallerScript(c echo.Context) error {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", true))
	}

	platform := c.QueryParam("platform")
	if platform != "linux" && platform != "windows" {
		platform = "linux"
	}

	token, err := h.Model.GetEnrollmentTokenByID(tokenID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Read CA certificate
	caCertData, err := os.ReadFile(h.CACertPath)
	if err != nil {
		log.Printf("[ERROR]: could not read CA certificate: %v", err)
		return RenderError(c, partials.ErrorMessage("Could not read CA certificate", true))
	}
	caCertB64 := base64.StdEncoding.EncodeToString(caCertData)

	tenantID := ""
	if token.Edges.Tenant != nil {
		tenantID = strconv.Itoa(token.Edges.Tenant.ID)
	}

	siteID := ""
	if token.Edges.Site != nil {
		siteID = strconv.Itoa(token.Edges.Site.ID)
	}

	var script string
	var filename string

	if platform == "windows" {
		script = generateWindowsScript(h.NATSServers, tenantID, siteID, token.Token, caCertB64)
		filename = fmt.Sprintf("openuem-enroll-%s.ps1", token.Token[:8])
	} else {
		script = generateLinuxScript(h.NATSServers, tenantID, siteID, token.Token, caCertB64)
		filename = fmt.Sprintf("openuem-enroll-%s.sh", token.Token[:8])
	}

	scriptPath := filepath.Join(h.DownloadDir, filename)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return RenderError(c, partials.ErrorMessage("Could not create script file", true))
	}

	return c.Attachment(scriptPath, filename)
}

func generateLinuxScript(natsServers, tenantID, siteID, token, caCertB64 string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("# OpenUEM Agent Enrollment Script\n")
	sb.WriteString("# Generated: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	sb.WriteString("set -e\n\n")

	sb.WriteString("CONFIG_DIR=\"/etc/openuem-agent\"\n")
	sb.WriteString("CERT_DIR=\"$CONFIG_DIR/certificates\"\n\n")

	sb.WriteString("# Create directories\n")
	sb.WriteString("mkdir -p \"$CERT_DIR\"\n\n")

	sb.WriteString("# Write CA certificate\n")
	sb.WriteString(fmt.Sprintf("echo '%s' | base64 -d > \"$CERT_DIR/ca.cer\"\n\n", caCertB64))

	sb.WriteString("# Write configuration\n")
	sb.WriteString("cat > \"$CONFIG_DIR/openuem.ini\" << 'OPENUEM_EOF'\n")
	sb.WriteString("[Agent]\n")
	sb.WriteString("UUID=\n")
	sb.WriteString("Enabled=true\n")
	sb.WriteString("ExecuteTaskEveryXMinutes=5\n")
	sb.WriteString("Debug=false\n")
	sb.WriteString("DefaultFrequency=5\n")
	sb.WriteString("SFTPPort=2022\n")
	sb.WriteString("VNCProxyPort=5900\n")
	sb.WriteString("SFTPDisabled=false\n")
	sb.WriteString("RemoteAssistanceDisabled=false\n")
	sb.WriteString(fmt.Sprintf("TenantID=%s\n", tenantID))
	sb.WriteString(fmt.Sprintf("SiteID=%s\n", siteID))
	sb.WriteString(fmt.Sprintf("EnrollmentToken=%s\n", token))
	sb.WriteString("\n[NATS]\n")
	sb.WriteString(fmt.Sprintf("NATSServers=%s\n", natsServers))
	sb.WriteString("\n[Certificates]\n")
	sb.WriteString("CACert=$CERT_DIR/ca.cer\n")
	sb.WriteString("OPENUEM_EOF\n\n")

	sb.WriteString("echo \"OpenUEM Agent configured successfully.\"\n")
	sb.WriteString("echo \"Start the agent service to begin enrollment.\"\n")

	return sb.String()
}

func generateWindowsScript(natsServers, tenantID, siteID, token, caCertB64 string) string {
	var sb strings.Builder
	sb.WriteString("# OpenUEM Agent Enrollment Script (Windows)\n")
	sb.WriteString("# Generated: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	sb.WriteString("$ErrorActionPreference = 'Stop'\n\n")

	sb.WriteString("$ConfigDir = \"$env:ProgramData\\openuem-agent\"\n")
	sb.WriteString("$CertDir = \"$ConfigDir\\certificates\"\n\n")

	sb.WriteString("# Create directories\n")
	sb.WriteString("New-Item -ItemType Directory -Force -Path $CertDir | Out-Null\n\n")

	sb.WriteString("# Write CA certificate\n")
	sb.WriteString(fmt.Sprintf("$caCertB64 = '%s'\n", caCertB64))
	sb.WriteString("$caCertBytes = [System.Convert]::FromBase64String($caCertB64)\n")
	sb.WriteString("[System.IO.File]::WriteAllBytes(\"$CertDir\\ca.cer\", $caCertBytes)\n\n")

	sb.WriteString("# Write configuration\n")
	sb.WriteString("$config = @\"\n")
	sb.WriteString("[Agent]\n")
	sb.WriteString("UUID=\n")
	sb.WriteString("Enabled=true\n")
	sb.WriteString("ExecuteTaskEveryXMinutes=5\n")
	sb.WriteString("Debug=false\n")
	sb.WriteString("DefaultFrequency=5\n")
	sb.WriteString("SFTPPort=2022\n")
	sb.WriteString("VNCProxyPort=5900\n")
	sb.WriteString("SFTPDisabled=false\n")
	sb.WriteString("RemoteAssistanceDisabled=false\n")
	sb.WriteString(fmt.Sprintf("TenantID=%s\n", tenantID))
	sb.WriteString(fmt.Sprintf("SiteID=%s\n", siteID))
	sb.WriteString(fmt.Sprintf("EnrollmentToken=%s\n", token))
	sb.WriteString("\n[NATS]\n")
	sb.WriteString(fmt.Sprintf("NATSServers=%s\n", natsServers))
	sb.WriteString("\n[Certificates]\n")
	sb.WriteString("CACert=$CertDir\\ca.cer\n")
	sb.WriteString("\"@\n\n")

	sb.WriteString("$config | Set-Content -Path \"$ConfigDir\\openuem.ini\" -Encoding UTF8\n\n")

	sb.WriteString("Write-Host 'OpenUEM Agent configured successfully.'\n")
	sb.WriteString("Write-Host 'Start the agent service to begin enrollment.'\n")

	return sb.String()
}

func (h *Handler) listEnrollmentTokensWithError(c echo.Context, commonInfo *partials.CommonInfo, errMsg string) error {
	tenantID, _ := strconv.Atoi(commonInfo.TenantID)
	tokens, _ := h.Model.GetEnrollmentTokens(tenantID)
	sites, _ := h.Model.GetSites(tenantID)
	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()

	return RenderView(c, admin_views.EnrollmentTokensIndex(" | Enrollment",
		admin_views.EnrollmentTokens(c, tokens, sites, errMsg, agentsExists, serversExists, commonInfo),
		commonInfo))
}
