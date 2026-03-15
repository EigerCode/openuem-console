package models

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ent "github.com/open-uem/ent"
	"github.com/open-uem/ent/agent"
	"github.com/open-uem/ent/predicate"
	"github.com/open-uem/ent/softwareassignment"
	"github.com/open-uem/ent/softwarecatalog"
	"github.com/open-uem/ent/softwarepackage"
	"github.com/open-uem/ent/softwarerepo"
	"github.com/open-uem/ent/tenant"
	"github.com/open-uem/openuem-console/internal/common/s3storage"
	sd "github.com/open-uem/openuem-console/internal/common/softwaredeployment"
)

// GetAgentWithRelations fetches an agent with its site (including tenant) and tags eagerly loaded.
func (m *Model) GetAgentWithRelations(agentID string) (*ent.Agent, error) {
	return m.Client.Agent.Query().
		Where(agent.IDEQ(agentID)).
		WithSite(func(q *ent.SiteQuery) {
			q.WithTenant()
		}).
		WithTags().
		Only(context.Background())
}

// GetEffectiveAssignments resolves all software assignments for an agent
// by aggregating assignments from site, tags, and direct agent assignments.
// The agent must be loaded with WithSite(WithTenant) and WithTags (via GetAgentWithRelations).
func (m *Model) GetEffectiveAssignments(agentObj *ent.Agent, platform string) ([]sd.AssignmentInfo, error) {
	ctx := context.Background()
	var results []sd.AssignmentInfo

	// Use eagerly loaded edges (Site is a non-unique edge, so it's a slice)
	if len(agentObj.Edges.Site) == 0 {
		return nil, fmt.Errorf("agent has no site loaded (use GetAgentWithRelations)")
	}
	agentSite := agentObj.Edges.Site[0]

	agentTenant := agentSite.Edges.Tenant
	if agentTenant == nil {
		return nil, fmt.Errorf("agent site has no tenant loaded (use GetAgentWithRelations)")
	}

	// Build query predicates
	predicates := []predicate.SoftwareAssignment{
		softwareassignment.HasTenantWith(tenant.ID(agentTenant.ID)),
		softwareassignment.Active(true),
	}

	// Filter by platform if specified
	if platform == "darwin" {
		predicates = append(predicates, softwareassignment.PackagePlatformEQ(softwareassignment.PackagePlatformDarwin))
	} else if platform == "windows" {
		predicates = append(predicates, softwareassignment.PackagePlatformEQ(softwareassignment.PackagePlatformWindows))
	}

	// Get all active assignments for this tenant
	assignments, err := m.Client.SoftwareAssignment.Query().
		Where(predicates...).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not query assignments: %w", err)
	}

	// Get agent's tag IDs from eagerly loaded tags
	tagIDs := make(map[int]bool)
	for _, t := range agentObj.Edges.Tags {
		tagIDs[t.ID] = true
	}

	siteIDStr := strconv.Itoa(agentSite.ID)

	// Filter assignments that apply to this agent
	for _, a := range assignments {
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
			applies = a.TargetID == agentObj.ID
		}

		if applies {
			results = append(results, sd.AssignmentInfo{
				PackageName:    a.PackageName,
				AssignmentType: string(a.AssignmentType),
			})
		}
	}

	return results, nil
}

// GetAgentCatalogs returns the catalog ring names applicable to an agent.
// Uses the effective ring (agent > tag > site > default "broad") to determine
// which single catalog the agent should use.
func (m *Model) GetAgentCatalogs(agent *ent.Agent) ([]string, error) {
	ring, _ := m.GetEffectiveRing(agent.ID)
	return []string{ring}, nil
}

// GetAgentTenantID returns the tenant ID for an agent.
func (m *Model) GetAgentTenantID(agentID string) (int, error) {
	ctx := context.Background()

	a, err := m.Client.Agent.Get(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("agent not found: %w", err)
	}

	agentSite, err := a.QuerySite().First(ctx)
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
// including subscribed global packages whose global ref has been promoted to this ring.
// Tenant's own packages take priority over global packages with the same name.
func (m *Model) GetCatalogPackages(tenantID int, ring string, platform string) ([]sd.CatalogPackageInfo, error) {
	ctx := context.Background()

	ringOrder, ok := ringOrderMap[ring]
	if !ok {
		ringOrder = 3
	}

	// Build platform predicate
	var platformPredicate []predicate.SoftwarePackage
	if platform == "darwin" {
		platformPredicate = append(platformPredicate, softwarepackage.PlatformEQ(softwarepackage.PlatformDarwin))
	} else if platform == "windows" {
		platformPredicate = append(platformPredicate, softwarepackage.PlatformEQ(softwarepackage.PlatformWindows))
	}

	// 1. Tenant's own packages (source=upload) that are in this ring
	ownQuery := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasCatalogsWith(softwarecatalog.Name(ring)),
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceUpload),
		)
	if len(platformPredicate) > 0 {
		ownQuery = ownQuery.Where(platformPredicate...)
	}
	ownPackages, err := ownQuery.
		WithRepo().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not query own catalog packages: %w", err)
	}

	seen := make(map[string]bool)
	var result []sd.CatalogPackageInfo

	// Own packages first (take priority)
	for _, pkg := range ownPackages {
		key := pkg.Name + "-" + pkg.Version
		seen[key] = true
		result = append(result, buildCatalogPackageInfo(pkg, "tenant"))
	}

	// 2. Subscribed global packages — check the GLOBAL package's ring status
	subQuery := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
		)
	if len(platformPredicate) > 0 {
		subQuery = subQuery.Where(platformPredicate...)
	}
	subscriptions, err := subQuery.
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs().WithRepo()
		}).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not query subscribed packages: %w", err)
	}

	for _, sub := range subscriptions {
		globalPkg := sub.Edges.GlobalRef
		if globalPkg == nil {
			continue
		}

		key := globalPkg.Name + "-" + globalPkg.Version
		if seen[key] {
			continue // Tenant's own package takes priority
		}

		// Check if global package has been promoted to a ring accessible for this ring
		available := false
		for _, cat := range globalPkg.Edges.Catalogs {
			if cat.RingOrder <= ringOrder {
				available = true
				break
			}
		}
		if !available {
			continue
		}

		seen[key] = true
		result = append(result, buildCatalogPackageInfo(globalPkg, "global"))
	}

	// 3. Legacy imports (source=global) — keep backwards compatibility
	legacyQuery := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasCatalogsWith(softwarecatalog.Name(ring)),
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobal),
		)
	if len(platformPredicate) > 0 {
		legacyQuery = legacyQuery.Where(platformPredicate...)
	}
	legacyPackages, err := legacyQuery.
		WithRepo().
		All(ctx)
	if err == nil {
		for _, pkg := range legacyPackages {
			key := pkg.Name + "-" + pkg.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, buildCatalogPackageInfo(pkg, "global"))
		}
	}

	return result, nil
}

// buildCatalogPackageInfo converts a SoftwarePackage to a CatalogPackageInfo.
func buildCatalogPackageInfo(pkg *ent.SoftwarePackage, repoType string) sd.CatalogPackageInfo {
	return sd.CatalogPackageInfo{
		Name:                   pkg.Name,
		Version:                pkg.Version,
		DisplayName:            pkg.DisplayName,
		Description:            pkg.Description,
		Category:               pkg.Category,
		Developer:              pkg.Developer,
		InstallerPath:          pkg.InstallerPath,
		InstallerType:          installerTypeFromPath(pkg.InstallerPath),
		ChecksumSHA256:         pkg.ChecksumSha256,
		SizeBytes:              pkg.SizeBytes,
		MinOSVersion:           pkg.MinOsVersion,
		MaxOSVersion:           pkg.MaxOsVersion,
		RestartAction:          string(pkg.RestartAction),
		UnattendedInstall:      pkg.UnattendedInstall,
		UnattendedUninstall:    pkg.UnattendedUninstall,
		IconName:               pkg.IconName,
		RepoType:               repoType,
		InstallsItems:          pkg.InstallsItems,
		Receipts:               pkg.Receipts,
		BlockingApps:           pkg.BlockingApps,
		SupportedArchitectures: pkg.SupportedArchitectures,
		PkginfoData:            pkg.PkginfoData,
		PreInstallScript:       pkg.PreInstallScript,
		PostInstallScript:      pkg.PostInstallScript,
	}
}

// installerTypeFromPath derives the file type from the installer path extension.
func installerTypeFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "pkg", "dmg", "msi", "exe", "msix", "appx":
		return ext
	default:
		return ""
	}
}

// GetPackageRepoType returns the repo type ("global" or "tenant") for a package
// identified by its installer path within a given tenant.
func (m *Model) GetPackageRepoType(tenantID int, installerPath string) (string, error) {
	ctx := context.Background()

	pkg, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.InstallerPathEQ(installerPath),
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
		).
		First(ctx)
	if err != nil {
		// Try global packages
		pkg, err = m.Client.SoftwarePackage.Query().
			Where(softwarepackage.InstallerPathEQ(installerPath)).
			First(ctx)
		if err != nil {
			return "", fmt.Errorf("package not found: %w", err)
		}
	}

	if pkg.Source == softwarepackage.SourceUpload {
		return "tenant", nil
	}
	return "global", nil
}

// GetSoftwareRepos returns all software repos for a tenant.
func (m *Model) GetSoftwareRepos(tenantID int) ([]*ent.SoftwareRepo, error) {
	ctx := context.Background()

	query := m.Client.SoftwareRepo.Query()
	if tenantID > 0 {
		// Tenant context: show only repos belonging to this tenant
		query = query.Where(softwarerepo.HasTenantWith(tenant.ID(tenantID)))
	} else {
		// Global context: show only global repos (no tenant association)
		query = query.Where(softwarerepo.RepoTypeEQ(softwarerepo.RepoTypeGlobal))
	}

	return query.
		Order(ent.Asc(softwarerepo.FieldName)).
		All(ctx)
}

// GetSoftwareRepoByID returns a single software repo by ID.
func (m *Model) GetSoftwareRepoByID(repoID int) (*ent.SoftwareRepo, error) {
	return m.Client.SoftwareRepo.Get(context.Background(), repoID)
}

// CreateSoftwareRepo creates a new software repo.
func (m *Model) CreateSoftwareRepo(tenantID int, name, repoType, endpoint, bucket, region, accessKey, secretKey, basePath string, usePresigned bool, presignTTL int, isDefault bool) (*ent.SoftwareRepo, error) {
	ctx := context.Background()

	creator := m.Client.SoftwareRepo.Create().
		SetName(name).
		SetRepoType(softwarerepo.RepoType(repoType)).
		SetEndpoint(endpoint).
		SetBucket(bucket).
		SetRegion(region).
		SetAccessKey(accessKey).
		SetSecretKey(secretKey).
		SetBasePath(basePath).
		SetUsePresigned(usePresigned).
		SetPresignTTLSeconds(presignTTL).
		SetIsDefault(isDefault)

	if tenantID > 0 {
		creator = creator.SetTenantID(tenantID)
	}

	return creator.Save(ctx)
}

// UpdateSoftwareRepo updates an existing software repo.
func (m *Model) UpdateSoftwareRepo(repoID int, name, endpoint, bucket, region, accessKey, secretKey, basePath string, usePresigned bool, presignTTL int, isDefault bool) (*ent.SoftwareRepo, error) {
	ctx := context.Background()

	updater := m.Client.SoftwareRepo.UpdateOneID(repoID).
		SetName(name).
		SetEndpoint(endpoint).
		SetBucket(bucket).
		SetRegion(region).
		SetBasePath(basePath).
		SetUsePresigned(usePresigned).
		SetPresignTTLSeconds(presignTTL).
		SetIsDefault(isDefault)

	if accessKey != "" {
		updater = updater.SetAccessKey(accessKey)
	}
	if secretKey != "" {
		updater = updater.SetSecretKey(secretKey)
	}

	return updater.Save(ctx)
}

// DeleteSoftwareRepo deletes a software repo by ID.
func (m *Model) DeleteSoftwareRepo(repoID int) error {
	return m.Client.SoftwareRepo.DeleteOneID(repoID).Exec(context.Background())
}

// TestSoftwareRepoConnection tests if S3 connection works for a repo.
func (m *Model) TestSoftwareRepoConnection(endpoint, bucket, region, accessKey, secretKey, basePath string) error {
	client, err := s3storage.New(s3storage.Config{
		Endpoint:  endpoint,
		Bucket:    bucket,
		Region:    region,
		AccessKey: accessKey,
		SecretKey: secretKey,
		BasePath:  basePath,
	})
	if err != nil {
		return fmt.Errorf("could not create S3 client: %w", err)
	}

	return client.TestConnection(context.Background())
}

// GetPresignedURL generates a pre-signed S3 URL for downloading a package.
func (m *Model) GetPresignedURL(ctx context.Context, tenantID int, path, repoType string) (string, error) {
	// Find the repo configuration for this tenant and repo type
	// Global repos have no tenant association, so we query without tenant filter
	query := m.Client.SoftwareRepo.Query().
		Where(softwarerepo.RepoTypeEQ(softwarerepo.RepoType(repoType)))

	if repoType == string(softwarerepo.RepoTypeGlobal) {
		// Global repos are not associated with any tenant
		query = query.Where(softwarerepo.Not(softwarerepo.HasTenant()))
	} else {
		query = query.Where(softwarerepo.HasTenantWith(tenant.ID(tenantID)))
	}

	repo, err := query.First(ctx)
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
