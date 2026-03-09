package models

import (
	"context"
	"fmt"
	"strconv"
	"time"

	ent "github.com/open-uem/ent"
	"github.com/open-uem/ent/softwareassignment"
	"github.com/open-uem/ent/softwarecatalog"
	"github.com/open-uem/ent/softwarepackage"
	"github.com/open-uem/ent/softwarerepo"
	"github.com/open-uem/ent/tenant"
	"github.com/open-uem/openuem-console/internal/common/s3storage"
	sd "github.com/open-uem/openuem-console/internal/common/softwaredeployment"
)

// GetAgentWithRelations fetches an agent with its site and tags.
func (m *Model) GetAgentWithRelations(agentID string) (*ent.Agent, error) {
	return m.Client.Agent.Get(context.Background(), agentID)
}

// GetEffectiveAssignments resolves all software assignments for an agent
// by aggregating assignments from site, tags, and direct agent assignments.
func (m *Model) GetEffectiveAssignments(agent *ent.Agent) ([]sd.AssignmentInfo, error) {
	ctx := context.Background()
	var results []sd.AssignmentInfo

	// Get site for this agent
	agentSite, err := agent.QuerySite().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get agent site: %w", err)
	}

	// Get tenant for this agent
	agentTenant, err := agentSite.QueryTenant().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get agent tenant: %w", err)
	}

	// Get all active assignments for this tenant
	assignments, err := m.Client.SoftwareAssignment.Query().
		Where(
			softwareassignment.HasTenantWith(tenant.ID(agentTenant.ID)),
			softwareassignment.Active(true),
		).
		WithPackage().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not query assignments: %w", err)
	}

	// Get agent's tag IDs
	tagIDs := make(map[int]bool)
	tags, err := agent.QueryTags().All(ctx)
	if err == nil {
		for _, t := range tags {
			tagIDs[t.ID] = true
		}
	}

	siteIDStr := strconv.Itoa(agentSite.ID)

	// Filter assignments that apply to this agent
	for _, a := range assignments {
		pkg := a.Edges.Package
		if pkg == nil {
			continue
		}

		applies := false
		switch a.TargetType {
		case softwareassignment.TargetTypeSite:
			applies = a.TargetID == siteIDStr
		case softwareassignment.TargetTypeTag:
			tagID, err := strconv.Atoi(a.TargetID)
			if err == nil {
				applies = tagIDs[tagID]
			}
		case softwareassignment.TargetTypeAgent:
			applies = a.TargetID == agent.ID
		}

		if applies {
			results = append(results, sd.AssignmentInfo{
				PackageName:    pkg.Name,
				AssignmentType: string(a.AssignmentType),
			})
		}
	}

	return results, nil
}

// GetAgentCatalogs returns the catalog ring names applicable to an agent.
func (m *Model) GetAgentCatalogs(agent *ent.Agent) ([]string, error) {
	ctx := context.Background()

	agentSite, err := agent.QuerySite().Only(ctx)
	if err != nil {
		return []string{"broad"}, nil
	}

	agentTenant, err := agentSite.QueryTenant().Only(ctx)
	if err != nil {
		return []string{"broad"}, nil
	}

	catalogs, err := m.Client.SoftwareCatalog.Query().
		Where(softwarecatalog.HasTenantWith(tenant.ID(agentTenant.ID))).
		Order(ent.Asc(softwarecatalog.FieldRingOrder)).
		All(ctx)
	if err != nil || len(catalogs) == 0 {
		return []string{"broad"}, nil
	}

	names := make([]string, 0, len(catalogs))
	for _, c := range catalogs {
		names = append(names, c.Name)
	}
	return names, nil
}

// GetAgentTenantID returns the tenant ID for an agent.
func (m *Model) GetAgentTenantID(agentID string) (int, error) {
	ctx := context.Background()

	agent, err := m.Client.Agent.Get(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("agent not found: %w", err)
	}

	agentSite, err := agent.QuerySite().Only(ctx)
	if err != nil {
		return 0, fmt.Errorf("agent site not found: %w", err)
	}

	agentTenant, err := agentSite.QueryTenant().Only(ctx)
	if err != nil {
		return 0, fmt.Errorf("agent tenant not found: %w", err)
	}

	return agentTenant.ID, nil
}

// GetCatalogPackages returns all packages in the given ring for a tenant,
// merging global and tenant packages (tenant takes priority).
func (m *Model) GetCatalogPackages(tenantID int, ring string) ([]sd.CatalogPackageInfo, error) {
	ctx := context.Background()

	packages, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasCatalogsWith(softwarecatalog.Name(ring)),
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
		).
		WithRepo().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not query catalog packages: %w", err)
	}

	// Build result with tenant packages taking priority over global
	seen := make(map[string]bool)
	var result []sd.CatalogPackageInfo

	for _, pkg := range packages {
		key := pkg.Name + "-" + pkg.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		repoType := "tenant"
		if pkg.Source == softwarepackage.SourceGlobal {
			repoType = "global"
		}

		item := sd.CatalogPackageInfo{
			Name:                pkg.Name,
			Version:             pkg.Version,
			DisplayName:         pkg.DisplayName,
			Description:         pkg.Description,
			Category:            pkg.Category,
			Developer:           pkg.Developer,
			InstallerPath:       pkg.InstallerPath,
			InstallerType:       string(pkg.InstallerType),
			ChecksumSHA256:      pkg.ChecksumSha256,
			SizeBytes:           pkg.SizeBytes,
			MinOSVersion:        pkg.MinOsVersion,
			MaxOSVersion:        pkg.MaxOsVersion,
			RestartAction:       string(pkg.RestartAction),
			UnattendedInstall:   pkg.UnattendedInstall,
			UnattendedUninstall: pkg.UnattendedUninstall,
			RepoType:            repoType,
		}
		result = append(result, item)
	}

	return result, nil
}

// GetPresignedURL generates a pre-signed S3 URL for downloading a package.
func (m *Model) GetPresignedURL(ctx context.Context, tenantID int, path, repoType string) (string, error) {
	// Find the repo configuration for this tenant and repo type
	repo, err := m.Client.SoftwareRepo.Query().
		Where(
			softwarerepo.HasTenantWith(tenant.ID(tenantID)),
			softwarerepo.RepoTypeEQ(softwarerepo.RepoType(repoType)),
		).
		First(ctx)
	if err != nil {
		return "", fmt.Errorf("repo not found for tenant %d type %s: %w", tenantID, repoType, err)
	}

	// Create S3 client
	client, err := s3storage.New(s3storage.Config{
		Endpoint:  repo.Endpoint,
		Bucket:    repo.Bucket,
		Region:    repo.Region,
		AccessKey: repo.AccessKey,
		SecretKey: repo.SecretKey,
		BasePath:  repo.BasePath,
	})
	if err != nil {
		return "", fmt.Errorf("could not create S3 client: %w", err)
	}

	ttl := time.Duration(repo.PresignTTLSeconds) * time.Second
	return client.PresignedGetURL(ctx, path, ttl)
}
