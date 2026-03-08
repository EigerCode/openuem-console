package models

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/open-uem/ent"
	entAgent "github.com/open-uem/ent/agent"
	"github.com/open-uem/ent/mdmcommand"
	"github.com/open-uem/ent/nanohubpushcertificate"
	"github.com/open-uem/ent/site"
	"github.com/open-uem/ent/tenant"
)

// GetNanoHubSettings retrieves the global NanoHub settings (singleton).
func (m *Model) GetNanoHubSettings() (*ent.NanoHubSettings, error) {
	return m.Client.NanoHubSettings.Query().First(context.Background())
}

// GetOrCreateNanoHubSettings retrieves or creates default NanoHub settings.
func (m *Model) GetOrCreateNanoHubSettings() (*ent.NanoHubSettings, error) {
	s, err := m.Client.NanoHubSettings.Query().First(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return m.Client.NanoHubSettings.Create().Save(context.Background())
		}
		return nil, err
	}
	return s, nil
}

// SaveNanoHubSettings upserts the global NanoHub settings.
func (m *Model) SaveNanoHubSettings(serverURL, username, password, caCerFile, scepURL, scepChallenge, mdmURL, enrollmentProfileID string) error {
	s, err := m.GetOrCreateNanoHubSettings()
	if err != nil {
		return err
	}
	return m.Client.NanoHubSettings.UpdateOneID(s.ID).
		SetServerURL(serverURL).
		SetUsername(username).
		SetPassword(password).
		SetCaCerFile(caCerFile).
		SetScepURL(scepURL).
		SetScepChallenge(scepChallenge).
		SetMdmURL(mdmURL).
		SetEnrollmentProfileID(enrollmentProfileID).
		Exec(context.Background())
}

// ClearNanoHubSettings deletes all NanoHub settings.
func (m *Model) ClearNanoHubSettings() error {
	_, err := m.Client.NanoHubSettings.Delete().Exec(context.Background())
	return err
}

// SaveNanoHubVendorCert saves the vendor private key and certificate in global settings.
func (m *Model) SaveNanoHubVendorCert(keyPEM, certPEM string) error {
	s, err := m.GetOrCreateNanoHubSettings()
	if err != nil {
		return err
	}
	return m.Client.NanoHubSettings.UpdateOneID(s.ID).
		SetVendorPrivateKeyPem(keyPEM).
		SetVendorCertPem(certPEM).
		Exec(context.Background())
}

// ClearNanoHubVendorCert clears all vendor certificate data from global settings.
func (m *Model) ClearNanoHubVendorCert() error {
	s, err := m.GetOrCreateNanoHubSettings()
	if err != nil {
		return err
	}
	return m.Client.NanoHubSettings.UpdateOneID(s.ID).
		SetVendorPrivateKeyPem("").
		SetVendorCertPem("").
		Exec(context.Background())
}

// SaveNanoHubCommand tracks a sent MDM command.
func (m *Model) SaveNanoHubCommand(uuid, cmdType, agentID string) error {
	return m.Client.MDMCommand.Create().
		SetID(uuid).
		SetType(cmdType).
		SetAgentID(agentID).
		Exec(context.Background())
}

// GetNanoHubCommand returns the command type for a given command UUID.
func (m *Model) GetNanoHubCommand(uuid string) (*ent.MDMCommand, error) {
	return m.Client.MDMCommand.Get(context.Background(), uuid)
}

// RemoveNanoHubCommand deletes a tracked command.
func (m *Model) RemoveNanoHubCommand(uuid string) error {
	return m.Client.MDMCommand.DeleteOneID(uuid).Exec(context.Background())
}

// IsNanoHubAgent checks if an agent is of type NanoHub.
func (m *Model) IsNanoHubAgent(agentID string) bool {
	a, err := m.Client.Agent.Get(context.Background(), agentID)
	if err != nil {
		return false
	}
	return a.EndpointType.String() == "NanoHub"
}

// CountNanoHubCommands returns the number of pending MDM commands.
func (m *Model) CountNanoHubCommands() (int, error) {
	return m.Client.MDMCommand.Query().Where(mdmcommand.TypeNEQ("")).Count(context.Background())
}

// --- Push Certificate Management (per Tenant) ---

// GetNanoHubPushCert retrieves the push certificate for a tenant.
func (m *Model) GetNanoHubPushCert(tenantID int) (*ent.NanoHubPushCertificate, error) {
	return m.Client.NanoHubPushCertificate.Query().
		Where(nanohubpushcertificate.HasTenantWith(tenant.ID(tenantID))).
		Only(context.Background())
}

// GetOrCreateNanoHubPushCert retrieves or creates a push certificate record for a tenant.
func (m *Model) GetOrCreateNanoHubPushCert(tenantID int) (*ent.NanoHubPushCertificate, error) {
	pc, err := m.Client.NanoHubPushCertificate.Query().
		Where(nanohubpushcertificate.HasTenantWith(tenant.ID(tenantID))).
		Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return m.Client.NanoHubPushCertificate.Create().
				AddTenantIDs(tenantID).
				Save(context.Background())
		}
		return nil, err
	}
	return pc, nil
}

// SaveNanoHubPushCertCSR saves the generated private key and CSR for a tenant.
func (m *Model) SaveNanoHubPushCertCSR(tenantID int, privateKeyPEM, csrPEM string) error {
	pc, err := m.GetOrCreateNanoHubPushCert(tenantID)
	if err != nil {
		return err
	}
	return m.Client.NanoHubPushCertificate.UpdateOneID(pc.ID).
		SetPrivateKeyPem(privateKeyPEM).
		SetCsrPem(csrPEM).
		Exec(context.Background())
}

// RenewNanoHubPushCertCSR saves a new CSR while keeping the existing certificate active.
// The old certificate remains valid until the admin uploads the renewed one.
func (m *Model) RenewNanoHubPushCertCSR(tenantID int, privateKeyPEM, csrPEM string) error {
	pc, err := m.GetOrCreateNanoHubPushCert(tenantID)
	if err != nil {
		return err
	}
	return m.Client.NanoHubPushCertificate.UpdateOneID(pc.ID).
		SetPrivateKeyPem(privateKeyPEM).
		SetCsrPem(csrPEM).
		Exec(context.Background())
}

// SaveNanoHubPushCert saves the uploaded push certificate for a tenant and clears the CSR.
func (m *Model) SaveNanoHubPushCert(tenantID int, certPEM, apnsTopic string, expiresAt time.Time) error {
	pc, err := m.GetOrCreateNanoHubPushCert(tenantID)
	if err != nil {
		return err
	}
	now := time.Now()
	return m.Client.NanoHubPushCertificate.UpdateOneID(pc.ID).
		SetCertificatePem(certPEM).
		SetApnsTopic(apnsTopic).
		SetExpiresAt(expiresAt).
		SetUploadedAt(now).
		SetCsrPem("").
		Exec(context.Background())
}

// UpsertNanoHubAgent creates or updates an agent entry for a NanoHub-managed Apple device.
// If an enrollment token is provided, the agent is assigned to the token's site/tenant.
// Otherwise it falls back to the first tenant's default site.
func (m *Model) UpsertNanoHubAgent(udid, deviceName, model, serialNumber, osVersion, enrollmentToken string) error {
	ctx := context.Background()

	// Check if agent already exists
	existing, err := m.Client.Agent.Get(ctx, udid)
	if err == nil {
		// Update existing agent
		now := time.Now()
		return m.Client.Agent.UpdateOneID(existing.ID).
			SetHostname(deviceName).
			SetLastContact(now).
			Exec(ctx)
	}

	if !ent.IsNotFound(err) {
		return fmt.Errorf("check existing agent: %w", err)
	}

	// Determine site from enrollment token
	var siteID int
	if enrollmentToken != "" {
		token, err := m.GetEnrollmentTokenByValue(enrollmentToken)
		if err == nil {
			// Token has a specific site assigned
			if token.Edges.Site != nil {
				siteID = token.Edges.Site.ID
			} else if token.Edges.Tenant != nil {
				// Token only has tenant — use its default site
				s, err := m.Client.Site.Query().
					Where(site.IsDefault(true), site.HasTenantWith(tenant.ID(token.Edges.Tenant.ID))).
					Only(ctx)
				if err == nil {
					siteID = s.ID
				}
			}

			// Increment token usage
			if siteID > 0 {
				if err := m.IncrementEnrollmentTokenUses(enrollmentToken); err != nil {
					log.Printf("[WARN]: could not increment enrollment token uses: %v", err)
				}
			}
		} else {
			log.Printf("[WARN]: invalid enrollment token %q during NanoHub enrollment: %v", enrollmentToken, err)
		}
	}

	// Fallback: first tenant's default site
	if siteID == 0 {
		log.Println("[WARN]: no valid enrollment token, falling back to first tenant's default site")
		tenants, err := m.Client.Tenant.Query().All(ctx)
		if err != nil || len(tenants) == 0 {
			return fmt.Errorf("no tenants found: %w", err)
		}
		s, err := m.Client.Site.Query().
			Where(site.IsDefault(true), site.HasTenantWith(tenant.ID(tenants[0].ID))).
			Only(ctx)
		if err != nil {
			return fmt.Errorf("get default site: %w", err)
		}
		siteID = s.ID
	}

	// Determine agent status based on tenant's auto-admit setting
	agentStatus := entAgent.AgentStatusWaitingForAdmission
	s, err := m.Client.Site.Get(ctx, siteID)
	if err == nil {
		tenantID, err := s.QueryTenant().OnlyID(ctx)
		if err == nil {
			autoAdmit, err := m.GetDefaultAutoAdmitAgents(fmt.Sprintf("%d", tenantID))
			if err == nil && autoAdmit {
				agentStatus = entAgent.AgentStatusEnabled
			}
		}
	}

	// Create agent with NanoHub endpoint type
	now := time.Now()
	return m.Client.Agent.Create().
		SetID(udid).
		SetHostname(deviceName).
		SetOs("macOS").
		SetNickname(deviceName).
		SetEndpointType(entAgent.EndpointTypeNanoHub).
		SetAgentStatus(agentStatus).
		SetFirstContact(now).
		SetLastContact(now).
		AddSiteIDs(siteID).
		Exec(ctx)
}

// GetTenantByAPNsTopic finds a tenant by the APNs topic of its push certificate.
func (m *Model) GetTenantByAPNsTopic(apnsTopic string) (*ent.Tenant, error) {
	pc, err := m.Client.NanoHubPushCertificate.Query().
		Where(nanohubpushcertificate.ApnsTopic(apnsTopic)).
		WithTenant().
		Only(context.Background())
	if err != nil {
		return nil, err
	}
	if pc.Edges.Tenant == nil || len(pc.Edges.Tenant) == 0 {
		return nil, fmt.Errorf("no tenant found for APNs topic %s", apnsTopic)
	}
	return pc.Edges.Tenant[0], nil
}
