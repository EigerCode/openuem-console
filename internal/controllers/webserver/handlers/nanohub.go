package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/views/admin_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

// enqueueNanoHubCommand sends a command plist to NanoHub and tracks it.
func (h *Handler) enqueueNanoHubCommand(serverURL, username, password, deviceID, commandType string) error {
	cmdUUID := uuid.New().String()

	commandPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Command</key>
	<dict>
		<key>RequestType</key>
		<string>%s</string>
	</dict>
	<key>CommandUUID</key>
	<string>%s</string>
</dict>
</plist>`, commandType, cmdUUID)

	// Save command tracking
	if err := h.Model.SaveNanoHubCommand(cmdUUID, commandType, deviceID); err != nil {
		return fmt.Errorf("could not save command tracking: %w", err)
	}

	// Send to NanoHub API (NanoMDM endpoints are mounted under /api/v1/nanomdm/)
	baseURL := strings.TrimSuffix(serverURL, "/")
	url := fmt.Sprintf("%s/api/v1/nanomdm/enqueue/%s", baseURL, deviceID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(commandPlist)))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("User-Agent", "openuem-console")
	req.Header.Set("Content-Type", "application/xml")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not send command to NanoHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("NanoHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[INFO]: enqueued %s command (%s) for device %s", commandType, cmdUUID, deviceID)
	return nil
}

// enqueueRemoveProfileCommand sends a RemoveProfile command to NanoHub.
// The profileIdentifier must match the PayloadIdentifier of the installed enrollment profile.
func (h *Handler) enqueueRemoveProfileCommand(serverURL, username, password, deviceID, profileIdentifier string) error {
	cmdUUID := uuid.New().String()

	commandPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Command</key>
	<dict>
		<key>RequestType</key>
		<string>RemoveProfile</string>
		<key>Identifier</key>
		<string>%s</string>
	</dict>
	<key>CommandUUID</key>
	<string>%s</string>
</dict>
</plist>`, profileIdentifier, cmdUUID)

	if err := h.Model.SaveNanoHubCommand(cmdUUID, "RemoveProfile", deviceID); err != nil {
		return fmt.Errorf("could not save command tracking: %w", err)
	}

	baseURL := strings.TrimSuffix(serverURL, "/")
	url := fmt.Sprintf("%s/api/v1/nanomdm/enqueue/%s", baseURL, deviceID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(commandPlist)))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("User-Agent", "openuem-console")
	req.Header.Set("Content-Type", "application/xml")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not send RemoveProfile to NanoHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("NanoHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[INFO]: enqueued RemoveProfile command (%s) for device %s with profile %s", cmdUUID, deviceID, profileIdentifier)
	return nil
}

// NanoHubSettings shows the global NanoHub settings page.
// GET /admin/nanohub
func (h *Handler) NanoHubSettings(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}
	commonInfo.TenantID = "-1"

	settings, err := h.Model.GetOrCreateNanoHubSettings()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.could_not_get_settings"), false))
	}

	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()

	return RenderView(c, admin_views.NanoHubSettingsIndex(" | NanoHub Settings", admin_views.NanoHubGlobalSettings(c, settings, agentsExists, serversExists, commonInfo), commonInfo))
}

// SaveNanoHubSettings saves the global NanoHub settings.
// POST /admin/nanohub
func (h *Handler) SaveNanoHubSettings(c echo.Context) error {
	serverURL := c.FormValue("server_url")
	username := c.FormValue("username")
	password := c.FormValue("password")
	caCerFile := c.FormValue("ca_cer_file")
	scepURL := c.FormValue("scep_url")
	scepChallenge := c.FormValue("scep_challenge")
	mdmURL := c.FormValue("mdm_url")
	enrollmentProfileID := c.FormValue("enrollment_profile_id")
	if enrollmentProfileID == "" {
		enrollmentProfileID = "com.openuem.mdm.enrollment"
	}

	if err := h.Model.SaveNanoHubSettings(serverURL, username, password, caCerFile, scepURL, scepChallenge, mdmURL, enrollmentProfileID); err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf(i18n.T(c.Request().Context(), "nanohub.settings_not_saved"), err.Error()), false))
	}

	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "nanohub.settings_saved")))
}

// DeleteNanoHubSettings deletes the global NanoHub settings.
// DELETE /admin/nanohub
func (h *Handler) DeleteNanoHubSettings(c echo.Context) error {
	if err := h.Model.ClearNanoHubSettings(); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}
	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "nanohub.settings_deleted")))
}

// --- Vendor Certificate Management (global) ---

// UploadVendorCert handles the upload of the MDM vendor certificate and private key.
// POST /admin/nanohub/vendor
func (h *Handler) UploadVendorCert(c echo.Context) error {
	certFile, err := c.FormFile("vendor_cert")
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.no_file_selected"), false))
	}

	keyFile, err := c.FormFile("vendor_key")
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.no_file_selected"), false))
	}

	// Read certificate file
	certSrc, err := certFile.Open()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}
	defer certSrc.Close()
	certData, err := io.ReadAll(certSrc)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	// Try to parse as PEM first, then DER
	certPEM := string(certData)
	block, _ := pem.Decode(certData)
	if block == nil {
		// Might be DER-encoded, try to parse and convert to PEM
		cert, err := x509.ParseCertificate(certData)
		if err != nil {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.vendor_cert_invalid"), false))
		}
		certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	} else {
		// Validate the PEM certificate
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.vendor_cert_invalid"), false))
		}
	}

	// Read private key file
	keySrc, err := keyFile.Open()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}
	defer keySrc.Close()
	keyData, err := io.ReadAll(keySrc)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	// Validate the private key PEM
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.vendor_key_invalid"), false))
	}

	// Check if the key is encrypted (DEK-Info header present, e.g. from mdmctl -password=secret)
	if x509.IsEncryptedPEMBlock(keyBlock) { //nolint:staticcheck
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.vendor_key_encrypted"), false))
	}

	// Try PKCS1, PKCS8, then EC
	if _, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err != nil {
		if _, err2 := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err2 != nil {
			if _, err3 := x509.ParseECPrivateKey(keyBlock.Bytes); err3 != nil {
				return RenderError(c, partials.ErrorMessage(
					fmt.Sprintf("%s (PKCS1: %v, PKCS8: %v, EC: %v)",
						i18n.T(c.Request().Context(), "nanohub.vendor_key_invalid"), err, err2, err3), false))
			}
		}
	}

	if err := h.Model.SaveNanoHubVendorCert(string(keyData), certPEM); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "nanohub.vendor_cert_uploaded")))
}

// DeleteVendorCert clears all vendor certificate data.
// DELETE /admin/nanohub/vendor
func (h *Handler) DeleteVendorCert(c echo.Context) error {
	if err := h.Model.ClearNanoHubVendorCert(); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}
	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "nanohub.vendor_cert_deleted")))
}

// NanoHubPowerOff sends a ShutDownDevice command to a NanoHub agent.
// POST /computers/:uuid/power/nanohuboff
func (h *Handler) NanoHubPowerOff(c echo.Context) error {
	agentID := c.Param("uuid")

	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.could_not_get_settings"), false))
	}

	if err := h.enqueueNanoHubCommand(settings.ServerURL, settings.Username, settings.Password, agentID, "ShutDownDevice"); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	return c.NoContent(http.StatusOK)
}

// NanoHubReboot sends a RestartDevice command to a NanoHub agent.
// POST /computers/:uuid/power/nanohubreboot
func (h *Handler) NanoHubReboot(c echo.Context) error {
	agentID := c.Param("uuid")

	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.could_not_get_settings"), false))
	}

	if err := h.enqueueNanoHubCommand(settings.ServerURL, settings.Username, settings.Password, agentID, "RestartDevice"); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	return c.NoContent(http.StatusOK)
}

// --- Push Certificate Management (per Tenant) ---

// NanoHubPushCertPage shows the push certificate management page for a tenant.
// GET /tenant/:tenant/admin/nanohub
func (h *Handler) NanoHubPushCertPage(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	pushCert, err := h.Model.GetOrCreateNanoHubPushCert(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	// Check if vendor certificate is configured
	vendorCertConfigured := false
	if settings, err := h.Model.GetNanoHubSettings(); err == nil {
		vendorCertConfigured = settings.VendorCertPem != "" && settings.VendorPrivateKeyPem != ""
	}

	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()

	return RenderView(c, admin_views.NanoHubPushCertIndex(" | Apple Push Certificate",
		admin_views.NanoHubPushCert(c, pushCert, vendorCertConfigured, agentsExists, serversExists, commonInfo),
		commonInfo))
}

// GenerateNanoHubCSR generates an RSA key pair and CSR for APNs push certificate.
// POST /tenant/:tenant/admin/nanohub/csr
func (h *Handler) GenerateNanoHubCSR(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	// Generate RSA 2048 key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not generate RSA key: %v", err), false))
	}

	// Create CSR
	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("%s MDM Push Certificate", h.OrgName),
			Organization: []string{h.OrgName},
		},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, privateKey)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not create CSR: %v", err), false))
	}

	// Encode to PEM
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})

	// Save to DB
	if err := h.Model.SaveNanoHubPushCertCSR(tenantID, string(privateKeyPEM), string(csrPEM)); err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not save CSR: %v", err), false))
	}

	// Redirect back to the push cert page (CSR is used internally for signing)
	return h.NanoHubPushCertPage(c)
}

// SignAndDownloadPushRequest signs the push CSR with the vendor cert and assembles
// the PushCertificateRequest.plist for upload to identity.apple.com.
// POST /tenant/:tenant/admin/nanohub/sign
func (h *Handler) SignAndDownloadPushRequest(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	// Load push cert record (needs CSR)
	pushCert, err := h.Model.GetNanoHubPushCert(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.push_cert_not_found"), false))
	}
	if pushCert.CsrPem == "" {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.no_csr_available"), false))
	}

	// Load global settings (needs vendor key + cert)
	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.could_not_get_settings"), false))
	}
	if settings.VendorCertPem == "" || settings.VendorPrivateKeyPem == "" {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.vendor_cert_required"), false))
	}

	// Build the PushCertificateRequest.plist
	plistData, err := BuildPushCertificateRequest(settings.VendorPrivateKeyPem, settings.VendorCertPem, pushCert.CsrPem)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("%s: %v", i18n.T(c.Request().Context(), "nanohub.sign_failed"), err), false))
	}

	safeOrgName := strings.ToLower(strings.ReplaceAll(h.OrgName, " ", "-"))
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-PushCertificateRequest.plist"`, safeOrgName))
	return c.Blob(http.StatusOK, "application/octet-stream", plistData)
}

// UploadNanoHubPushCert handles the upload of an Apple Push Certificate (.pem).
// POST /tenant/:tenant/admin/nanohub/pushcert
func (h *Handler) UploadNanoHubPushCert(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	file, err := c.FormFile("push_cert")
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.no_file_selected"), false))
	}

	src, err := file.Open()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	// Try PEM first, then DER
	certPEM := string(data)
	if block, _ := pem.Decode(data); block == nil {
		// Might be DER-encoded
		cert, err := x509.ParseCertificate(data)
		if err != nil {
			return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not parse push certificate: %v", err), false))
		}
		certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	}

	// Extract APNs topic and expiry from certificate
	apnsTopic, expiresAt, err := extractAPNsTopicFromCert(certPEM)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not parse push certificate: %v", err), false))
	}

	// Save to DB
	if err := h.Model.SaveNanoHubPushCert(tenantID, certPEM, apnsTopic, expiresAt); err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not save push certificate: %v", err), false))
	}

	// Push cert+key to NanoHub API if settings are configured
	if err := h.pushCertToNanoHub(tenantID); err != nil {
		log.Printf("[WARN]: could not push certificate to NanoHub API: %v", err)
	}

	return h.NanoHubPushCertPage(c)
}

// RenewNanoHubCSR generates a new CSR using the existing private key (required by Apple for renewal).
// POST /tenant/:tenant/admin/nanohub/renew
func (h *Handler) RenewNanoHubCSR(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	pushCert, err := h.Model.GetNanoHubPushCert(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.push_cert_not_found"), false))
	}

	if pushCert.PrivateKeyPem == "" {
		return RenderError(c, partials.ErrorMessage("No private key available for renewal", false))
	}

	// Parse existing private key
	block, _ := pem.Decode([]byte(pushCert.PrivateKeyPem))
	if block == nil {
		return RenderError(c, partials.ErrorMessage("Could not decode private key PEM", false))
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not parse private key: %v", err), false))
	}

	// Create new CSR with the same key
	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("%s MDM Push Certificate", h.OrgName),
			Organization: []string{h.OrgName},
		},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, privateKey)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not create renewal CSR: %v", err), false))
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})

	// Save new CSR and clear old certificate (so the view shows the sign flow)
	if err := h.Model.RenewNanoHubPushCertCSR(tenantID, pushCert.PrivateKeyPem, string(csrPEM)); err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not save renewal CSR: %v", err), false))
	}

	// Redirect back to the push cert page so user can "Sign and Download Request"
	return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/tenant/%s/admin/nanohub", commonInfo.TenantID))
}

// pushCertToNanoHub uploads the push certificate + private key to the NanoHub API.
func (h *Handler) pushCertToNanoHub(tenantID int) error {
	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return fmt.Errorf("could not get NanoHub settings: %w", err)
	}

	if settings.ServerURL == "" {
		log.Println("[WARN]: NanoHub ServerURL not configured, skipping push cert upload")
		return nil
	}

	pushCert, err := h.Model.GetNanoHubPushCert(tenantID)
	if err != nil {
		return fmt.Errorf("could not get push certificate: %w", err)
	}

	if pushCert.CertificatePem == "" || pushCert.PrivateKeyPem == "" {
		return fmt.Errorf("push certificate or private key is empty")
	}

	// NanoHub expects cert + key concatenated in the body
	body := pushCert.CertificatePem + "\n" + pushCert.PrivateKeyPem

	// NanoHub mounts NanoMDM endpoints under /api/v1/nanomdm/
	baseURL := strings.TrimSuffix(settings.ServerURL, "/")
	pushURL := baseURL + "/api/v1/nanomdm/pushcert"

	req, err := http.NewRequest("PUT", pushURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.SetBasicAuth(settings.Username, settings.Password)
	req.Header.Set("Content-Type", "application/x-pem-file")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not send push cert to NanoHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("NanoHub API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[INFO]: pushed Apple Push Certificate to NanoHub for tenant %d", tenantID)
	return nil
}

// extractAPNsTopicFromCert parses a PEM certificate and extracts the APNs topic and expiry date.
// It uses the Subject UID (UserID) field which contains the full topic in the format
// "com.apple.mgmt.External.<uuid>" — this matches how NanoMDM indexes push certificates.
func extractAPNsTopicFromCert(certPEM string) (string, time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", time.Time{}, fmt.Errorf("could not decode PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("could not parse certificate: %w", err)
	}

	// Use Subject UID (UserID OID 0.9.2342.19200300.100.1.1) as topic.
	// This contains "com.apple.mgmt.External.<uuid>" and matches NanoMDM's push cert indexing.
	var topic string
	uidOID := asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1}
	for _, name := range cert.Subject.Names {
		if name.Type.Equal(uidOID) {
			topic = fmt.Sprintf("%v", name.Value)
			break
		}
	}

	// Fallback: try Apple Push Topic OID extension (1.2.840.113635.100.8.2)
	if topic == "" {
		apnsPushOID := asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 8, 2}
		for _, ext := range cert.Extensions {
			if ext.Id.Equal(apnsPushOID) {
				var seq asn1.RawValue
				if _, err := asn1.Unmarshal(ext.Value, &seq); err == nil {
					rest := seq.Bytes
					for len(rest) > 0 {
						var val asn1.RawValue
						rest, err = asn1.Unmarshal(rest, &val)
						if err != nil {
							break
						}
						if val.Tag == asn1.TagUTF8String || val.Tag == asn1.TagOctetString {
							topic = string(val.Bytes)
							break
						}
					}
				}
				break
			}
		}
	}

	return topic, cert.NotAfter, nil
}

// --- MDM Enrollment Profile Generation ---

// DownloadMDMProfile generates and downloads a .mobileconfig enrollment profile.
// GET /tenant/:tenant/admin/enrollment/:id/mdmprofile
func (h *Handler) DownloadMDMProfile(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"), false))
	}

	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid token ID", false))
	}

	token, err := h.Model.GetEnrollmentTokenByID(tokenID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), false))
	}

	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.could_not_get_settings"), false))
	}

	if settings.ScepURL == "" || settings.MdmURL == "" {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.config_incomplete"), false))
	}

	pushCert, err := h.Model.GetNanoHubPushCert(tenantID)
	if err != nil || pushCert.ApnsTopic == "" {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nanohub.push_cert_missing"), false))
	}

	// Build MDM URL with token
	mdmURL := settings.MdmURL
	if strings.Contains(mdmURL, "?") {
		mdmURL += "&token=" + token.Token
	} else {
		mdmURL += "?token=" + token.Token
	}

	// Use branding product name if available, otherwise org name
	profileName := h.OrgName
	if commonInfo.Branding != nil && commonInfo.Branding.ProductName != "" {
		profileName = commonInfo.Branding.ProductName
	}

	profileData, err := generateMobileConfig(profileName, pushCert.ApnsTopic, settings.ScepURL, settings.ScepChallenge, mdmURL, settings.EnrollmentProfileID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(fmt.Sprintf("Could not generate profile: %v", err), false))
	}

	safeProfileName := strings.ToLower(strings.ReplaceAll(profileName, " ", "-"))
	filename := fmt.Sprintf("%s-mdm-%s.mobileconfig", safeProfileName, token.Token[:8])
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	return c.Blob(http.StatusOK, "application/x-apple-aspen-config", profileData)
}

// PublicDownloadMDMProfile serves an MDM enrollment profile via public API.
// GET /api/enroll/:token/mdmprofile
func (h *Handler) PublicDownloadMDMProfile(c echo.Context) error {
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

	// Determine tenant from token
	tenantID := 0
	if token.Edges.Tenant != nil {
		tenantID = token.Edges.Tenant.ID
	}
	if tenantID == 0 {
		return c.String(http.StatusInternalServerError, "token has no tenant association")
	}

	settings, err := h.Model.GetNanoHubSettings()
	if err != nil {
		return c.String(http.StatusInternalServerError, "NanoHub not configured")
	}

	if settings.ScepURL == "" || settings.MdmURL == "" {
		return c.String(http.StatusInternalServerError, "NanoHub SCEP or MDM URL not configured")
	}

	pushCert, err := h.Model.GetNanoHubPushCert(tenantID)
	if err != nil || pushCert.ApnsTopic == "" {
		return c.String(http.StatusInternalServerError, "No push certificate for this organization")
	}

	// Build MDM URL with token
	mdmURL := settings.MdmURL
	if strings.Contains(mdmURL, "?") {
		mdmURL += "&token=" + tokenValue
	} else {
		mdmURL += "?token=" + tokenValue
	}

	// Use branding product name if available, otherwise org name
	profileName := h.OrgName
	branding, _ := h.Model.GetOrCreateBranding()
	if branding != nil && branding.ProductName != "" {
		profileName = branding.ProductName
	}

	profileData, err := generateMobileConfig(profileName, pushCert.ApnsTopic, settings.ScepURL, settings.ScepChallenge, mdmURL, settings.EnrollmentProfileID)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("could not generate profile: %v", err))
	}

	safeProfileName := strings.ToLower(strings.ReplaceAll(profileName, " ", "-"))
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-mdm-%s.mobileconfig"`, safeProfileName, tokenValue[:8]))
	return c.Blob(http.StatusOK, "application/x-apple-aspen-config", profileData)
}

const mobileConfigTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadContent</key>
			<dict>
				<key>Key Type</key>
				<string>RSA</string>
				<key>Key Usage</key>
				<integer>5</integer>
				<key>Keysize</key>
				<integer>2048</integer>
				<key>URL</key>
				<string>{{.ScepURL}}</string>
				{{- if .ScepChallenge}}
				<key>Challenge</key>
				<string>{{.ScepChallenge}}</string>
				{{- end}}
				<key>Subject</key>
				<array>
					<array>
						<array>
							<string>O</string>
							<string>{{.OrgName}}</string>
						</array>
					</array>
					<array>
						<array>
							<string>CN</string>
							<string>{{.OrgName}} SCEP Device Identity</string>
						</array>
					</array>
				</array>
			</dict>
			<key>PayloadDescription</key>
			<string>Configures SCEP</string>
			<key>PayloadDisplayName</key>
			<string>SCEP</string>
			<key>PayloadIdentifier</key>
			<string>com.openuem.mdm.scep.{{.ScepPayloadUUID}}</string>
			<key>PayloadType</key>
			<string>com.apple.security.scep</string>
			<key>PayloadUUID</key>
			<string>{{.ScepPayloadUUID}}</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
		<dict>
			<key>AccessRights</key>
			<integer>8191</integer>
			<key>CheckInURL</key>
			<string>{{.MdmURL}}</string>
			<key>CheckOutWhenRemoved</key>
			<true/>
			<key>IdentityCertificateUUID</key>
			<string>{{.ScepPayloadUUID}}</string>
			<key>PayloadDescription</key>
			<string>Configures MDM</string>
			<key>PayloadDisplayName</key>
			<string>MDM Profile</string>
			<key>PayloadIdentifier</key>
			<string>com.openuem.mdm.mdm.{{.MdmPayloadUUID}}</string>
			<key>PayloadType</key>
			<string>com.apple.mdm</string>
			<key>PayloadUUID</key>
			<string>{{.MdmPayloadUUID}}</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>ServerCapabilities</key>
			<array>
				<string>com.apple.mdm.per-user-connections</string>
			</array>
			<key>ServerURL</key>
			<string>{{.MdmURL}}</string>
			<key>SignMessage</key>
			<true/>
			<key>Topic</key>
			<string>{{.ApnsTopic}}</string>
		</dict>
	</array>
	<key>PayloadDescription</key>
	<string>{{.OrgName}} MDM Enrollment</string>
	<key>PayloadDisplayName</key>
	<string>{{.OrgName}} MDM</string>
	<key>PayloadIdentifier</key>
	<string>{{.EnrollmentProfileID}}</string>
	<key>PayloadOrganization</key>
	<string>{{.OrgName}}</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>{{.ProfileUUID}}</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

type mobileConfigData struct {
	OrgName             string
	ApnsTopic           string
	ScepURL             string
	ScepChallenge       string
	MdmURL              string
	EnrollmentProfileID string
	ProfileUUID         string
	ScepPayloadUUID     string
	MdmPayloadUUID      string
}

func generateMobileConfig(orgName, apnsTopic, scepURL, scepChallenge, mdmURL, enrollmentProfileID string) ([]byte, error) {
	tmpl, err := template.New("mobileconfig").Parse(mobileConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("could not parse template: %w", err)
	}

	data := mobileConfigData{
		OrgName:             orgName,
		ApnsTopic:           apnsTopic,
		ScepURL:             scepURL,
		ScepChallenge:       scepChallenge,
		MdmURL:              mdmURL,
		EnrollmentProfileID: enrollmentProfileID,
		ProfileUUID:         uuid.New().String(),
		ScepPayloadUUID:     uuid.New().String(),
		MdmPayloadUUID:      uuid.New().String(),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("could not execute template: %w", err)
	}

	return buf.Bytes(), nil
}
