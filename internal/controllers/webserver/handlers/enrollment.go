package handlers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
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

func (h *Handler) DownloadConfigZIP(c echo.Context) error {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", true))
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

	// Derive external NATS URL from Domain + port from internal NATSServers
	externalNATS := deriveExternalNATSURL(h.NATSServers, h.Domain)

	iniContent := generateConfigINI(externalNATS, token.Token)

	// Create ZIP in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add openuem.ini
	fw, err := zw.Create("openuem.ini")
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Could not create ZIP file", true))
	}
	if _, err := fw.Write([]byte(iniContent)); err != nil {
		return RenderError(c, partials.ErrorMessage("Could not write config to ZIP", true))
	}

	// Add certificates/ca.cer
	fw, err = zw.Create("certificates/ca.cer")
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Could not create ZIP file", true))
	}
	if _, err := fw.Write(caCertData); err != nil {
		return RenderError(c, partials.ErrorMessage("Could not write certificate to ZIP", true))
	}

	if err := zw.Close(); err != nil {
		return RenderError(c, partials.ErrorMessage("Could not finalize ZIP file", true))
	}

	filename := fmt.Sprintf("altiview-config-%s.zip", token.Token[:8])
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	return c.Blob(200, "application/zip", buf.Bytes())
}

// PublicDownloadConfig serves config ZIP without session auth.
// The enrollment token value in the URL acts as authentication.
func (h *Handler) PublicDownloadConfig(c echo.Context) error {
	tokenValue := c.Param("token")
	if tokenValue == "" {
		return c.String(http.StatusBadRequest, "missing token")
	}

	token, err := h.Model.GetEnrollmentTokenByValue(tokenValue)
	if err != nil {
		return c.String(http.StatusNotFound, "invalid token")
	}

	if !token.Active {
		return c.String(http.StatusForbidden, "token is inactive")
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return c.String(http.StatusForbidden, "token has expired")
	}
	if token.MaxUses > 0 && token.CurrentUses >= token.MaxUses {
		return c.String(http.StatusForbidden, "token usage limit reached")
	}

	platform := c.QueryParam("platform")
	switch platform {
	case "linux", "macos", "windows":
	default:
		platform = "linux"
	}

	caCertData, err := os.ReadFile(h.CACertPath)
	if err != nil {
		log.Printf("[ERROR]: could not read CA certificate: %v", err)
		return c.String(http.StatusInternalServerError, "could not read CA certificate")
	}

	externalNATS := deriveExternalNATSURL(h.NATSServers, h.Domain)
	iniContent := generatePlatformConfigINI(platform, externalNATS, token.Token)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	fw, err := zw.Create("openuem.ini")
	if err != nil {
		return c.String(http.StatusInternalServerError, "could not create ZIP")
	}
	if _, err := fw.Write([]byte(iniContent)); err != nil {
		return c.String(http.StatusInternalServerError, "could not write config")
	}

	fw, err = zw.Create("certificates/ca.cer")
	if err != nil {
		return c.String(http.StatusInternalServerError, "could not create ZIP")
	}
	if _, err := fw.Write(caCertData); err != nil {
		return c.String(http.StatusInternalServerError, "could not write certificate")
	}

	if err := zw.Close(); err != nil {
		return c.String(http.StatusInternalServerError, "could not finalize ZIP")
	}

	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="openuem-config-%s.zip"`, tokenValue[:8]))
	return c.Blob(http.StatusOK, "application/zip", buf.Bytes())
}

func (h *Handler) GetInstallCommand(c echo.Context) error {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", true))
	}

	platform := c.QueryParam("platform")
	switch platform {
	case "linux", "macos-amd64", "macos-arm64", "windows":
	default:
		platform = "linux"
	}

	token, err := h.Model.GetEnrollmentTokenByID(tokenID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Build console base URL from request
	consoleURL := fmt.Sprintf("https://%s", c.Request().Host)

	var command string
	var platformLabel string

	switch platform {
	case "linux":
		command = generateLinuxOneLiner(consoleURL, token.Token)
		platformLabel = "Linux"
	case "macos-amd64":
		command = generateMacOSOneLiner(consoleURL, token.Token, "amd64")
		platformLabel = "macOS Intel"
	case "macos-arm64":
		command = generateMacOSOneLiner(consoleURL, token.Token, "arm64")
		platformLabel = "macOS ARM"
	case "windows":
		command = generateWindowsOneLiner(consoleURL, token.Token)
		platformLabel = "Windows"
	}

	return RenderView(c, admin_views.InstallCommand(command, platformLabel))
}

func generateLinuxOneLiner(consoleURL, token string) string {
	return fmt.Sprintf(
		`sudo bash -c 'curl -fsSL "%s/api/enroll/%s/config?platform=linux" -o /tmp/c.zip && unzip -o /tmp/c.zip -d /etc/openuem-agent/ && curl -fsSL "%s/altiview-agent-linux-amd64.deb" -o /tmp/a.deb && dpkg -i /tmp/a.deb && rm /tmp/c.zip /tmp/a.deb'`,
		consoleURL, token, agentReleaseBaseURL,
	)
}

func generateMacOSOneLiner(consoleURL, token, arch string) string {
	return fmt.Sprintf(
		`sudo bash -c 'curl -fsSL "%s/api/enroll/%s/config?platform=macos" -o /tmp/c.zip && unzip -o /tmp/c.zip -d /Library/OpenUEMAgent/etc/openuem-agent/ && curl -fsSL "%s/altiview-agent-darwin-%s.pkg" -o /tmp/a.pkg && installer -pkg /tmp/a.pkg -target / && rm /tmp/c.zip /tmp/a.pkg'`,
		consoleURL, token, agentReleaseBaseURL, arch,
	)
}

func generateWindowsOneLiner(consoleURL, token string) string {
	return fmt.Sprintf(
		`$d="$env:ProgramFiles\EigerCode\AltiviewAgent"; Invoke-WebRequest '%s/api/enroll/%s/config?platform=windows' -OutFile "$env:TEMP\c.zip"; Expand-Archive "$env:TEMP\c.zip" $d -Force; Invoke-WebRequest '%s/altiview-agent-windows-amd64.msi' -OutFile "$env:TEMP\a.msi"; Start-Process msiexec "/i `+"`\""+`$env:TEMP\a.msi`+"`\""+` /qn" -Wait; Remove-Item "$env:TEMP\c.zip","$env:TEMP\a.msi"`,
		consoleURL, token, agentReleaseBaseURL,
	)
}

const agentReleaseBaseURL = "https://github.com/EigerCode/openuem-agent/releases/latest/download"

func generatePlatformConfigINI(platform, natsServers, token string) string {
	var sb strings.Builder
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
	sb.WriteString(fmt.Sprintf("EnrollmentToken=%s\n", token))
	sb.WriteString("\n[NATS]\n")
	sb.WriteString(fmt.Sprintf("NATSServers=%s\n", natsServers))
	sb.WriteString("\n[Certificates]\n")
	if platform == "windows" {
		sb.WriteString("CACert=C:\\Program Files\\EigerCode\\AltiviewAgent\\certificates\\ca.cer\n")
		sb.WriteString("AgentCert=C:\\Program Files\\EigerCode\\AltiviewAgent\\certificates\\agent.cer\n")
		sb.WriteString("AgentKey=C:\\Program Files\\EigerCode\\AltiviewAgent\\certificates\\agent.key\n")
		sb.WriteString("SFTPCert=C:\\Program Files\\EigerCode\\AltiviewAgent\\certificates\\sftp.cer\n")
	} else {
		sb.WriteString("CACert=certificates/ca.cer\n")
		sb.WriteString("AgentCert=certificates/agent.cer\n")
		sb.WriteString("AgentKey=certificates/agent.key\n")
		sb.WriteString("SFTPCert=certificates/sftp.cer\n")
	}
	return sb.String()
}

func generateConfigINI(natsServers, token string) string {
	var sb strings.Builder
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
	sb.WriteString(fmt.Sprintf("EnrollmentToken=%s\n", token))
	sb.WriteString("\n[NATS]\n")
	sb.WriteString(fmt.Sprintf("NATSServers=%s\n", natsServers))
	sb.WriteString("\n[Certificates]\n")
	sb.WriteString("CACert=certificates/ca.cer\n")
	sb.WriteString("AgentCert=certificates/agent.cer\n")
	sb.WriteString("AgentKey=certificates/agent.key\n")
	sb.WriteString("SFTPCert=certificates/sftp.cer\n")
	return sb.String()
}

// deriveExternalNATSURL constructs the external NATS URL using the console's
// Domain and the port from the internal NATSServers URL.
// e.g. internal "tls://nats:4433" + domain "example.com" → "tls://example.com:4433"
func deriveExternalNATSURL(internalNATS, domain string) string {
	parsed, err := url.Parse(internalNATS)
	if err != nil || domain == "" {
		return internalNATS
	}

	port := parsed.Port()
	if port == "" {
		port = "4433"
	}

	return fmt.Sprintf("%s://%s:%s", parsed.Scheme, domain, port)
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
