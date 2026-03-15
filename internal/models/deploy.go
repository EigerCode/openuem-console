package models

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ent "github.com/open-uem/ent"
	"github.com/open-uem/ent/agent"
	"github.com/open-uem/ent/predicate"
	"github.com/open-uem/ent/softwareassignment"
	"github.com/open-uem/ent/softwarecatalog"
	"github.com/open-uem/ent/softwareinstalllog"
	"github.com/open-uem/ent/softwarepackage"
	"github.com/open-uem/ent/softwarerepo"
	"github.com/open-uem/ent/tag"
	"github.com/open-uem/ent/tenant"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

// GetPackagesByPage returns paginated software packages for a tenant.
func (m *Model) GetPackagesByPage(p partials.PaginationAndSort, tenantID string) ([]*ent.SoftwarePackage, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	query := m.Client.SoftwarePackage.Query().
		Where(softwarepackage.HasTenantWith(tenant.ID(id))).
		WithCatalogs().
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs()
		}).
		Limit(p.PageSize).
		Offset((p.CurrentPage - 1) * p.PageSize)

	switch p.SortBy {
	case "name":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwarepackage.FieldName))
		} else {
			query = query.Order(ent.Desc(softwarepackage.FieldName))
		}
	case "version":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwarepackage.FieldVersion))
		} else {
			query = query.Order(ent.Desc(softwarepackage.FieldVersion))
		}
	case "platform":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwarepackage.FieldPlatform))
		} else {
			query = query.Order(ent.Desc(softwarepackage.FieldPlatform))
		}
	case "category":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwarepackage.FieldCategory))
		} else {
			query = query.Order(ent.Desc(softwarepackage.FieldCategory))
		}
	default:
		query = query.Order(ent.Desc(softwarepackage.FieldCreated))
	}

	return query.All(context.Background())
}

// CountPackages returns the total number of packages for a tenant.
func (m *Model) CountPackages(tenantID string) (int, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return 0, err
	}

	return m.Client.SoftwarePackage.Query().
		Where(softwarepackage.HasTenantWith(tenant.ID(id))).
		Count(context.Background())
}

// PackageGroup represents a group of packages with the same name and platform.
type PackageGroup struct {
	Name          string
	Platform      string
	DisplayName   string
	Category      string
	LatestVersion string
	VersionCount  int
	Catalogs      []string
	IconName      string
	HasPkginfo    bool
	Developer     string
	HasUploading  bool
}

// PackageVersionEntry represents a single version of a package.
type PackageVersionEntry struct {
	ID          int
	Version     string
	Catalogs    []string
	IsUploading bool
	Source      string
}

// PackageListEntry represents a grouped package with all its versions.
type PackageListEntry struct {
	Name        string
	DisplayName string
	Platform    string
	Category    string
	Developer   string
	IconName    string
	Versions    []PackageVersionEntry
	HasUploading bool
}

// GetPackageList returns packages grouped by name+platform, each with all versions and catalog info.
func (m *Model) GetPackageList(tenantID string, catalogFilter string) ([]PackageListEntry, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	packages, err := m.Client.SoftwarePackage.Query().
		Where(softwarepackage.HasTenantWith(tenant.ID(id))).
		WithCatalogs().
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs()
		}).
		Order(ent.Asc(softwarepackage.FieldName), ent.Desc(softwarepackage.FieldVersion)).
		All(context.Background())
	if err != nil {
		return nil, err
	}

	type groupKey struct{ Name, Platform string }
	groupMap := make(map[groupKey]*PackageListEntry)
	var groupOrder []groupKey

	for _, pkg := range packages {
		// Collect catalogs
		catalogs := pkg.Edges.Catalogs
		if len(catalogs) == 0 {
			if ref := pkg.Edges.GlobalRef; ref != nil {
				catalogs = ref.Edges.Catalogs
			}
		}
		var catNames []string
		for _, cat := range catalogs {
			catNames = append(catNames, cat.Name)
		}

		// Apply catalog filter
		if catalogFilter != "" {
			found := false
			for _, cn := range catNames {
				if cn == catalogFilter {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		key := groupKey{pkg.Name, string(pkg.Platform)}
		entry, exists := groupMap[key]
		if !exists {
			entry = &PackageListEntry{
				Name:        pkg.Name,
				DisplayName: pkg.DisplayName,
				Platform:    string(pkg.Platform),
				Category:    pkg.Category,
				Developer:   pkg.Developer,
				IconName:    pkg.IconName,
			}
			groupMap[key] = entry
			groupOrder = append(groupOrder, key)
		}

		isUploading := pkg.Status == softwarepackage.StatusUploading
		if isUploading {
			entry.HasUploading = true
		}

		entry.Versions = append(entry.Versions, PackageVersionEntry{
			ID:          pkg.ID,
			Version:     pkg.Version,
			Catalogs:    catNames,
			IsUploading: isUploading,
			Source:      string(pkg.Source),
		})
	}

	result := make([]PackageListEntry, 0, len(groupOrder))
	for _, key := range groupOrder {
		result = append(result, *groupMap[key])
	}
	return result, nil
}

// GetPackageGroups returns packages grouped by (name, platform) for a tenant.
func (m *Model) GetPackageGroups(tenantID string) ([]PackageGroup, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	packages, err := m.Client.SoftwarePackage.Query().
		Where(softwarepackage.HasTenantWith(tenant.ID(id))).
		WithCatalogs().
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs()
		}).
		Order(ent.Desc(softwarepackage.FieldCreated)).
		All(context.Background())
	if err != nil {
		return nil, err
	}

	// Group by (name, platform)
	type groupKey struct{ Name, Platform string }
	groupMap := make(map[groupKey]*PackageGroup)
	var groupOrder []groupKey

	for _, pkg := range packages {
		key := groupKey{pkg.Name, string(pkg.Platform)}
		g, exists := groupMap[key]
		if !exists {
			g = &PackageGroup{
				Name:          pkg.Name,
				Platform:      string(pkg.Platform),
				DisplayName:   pkg.DisplayName,
				Category:      pkg.Category,
				LatestVersion: pkg.Version,
				IconName:      pkg.IconName,
				HasPkginfo:    pkg.PkginfoData != "",
				Developer:     pkg.Developer,
			}
			groupMap[key] = g
			groupOrder = append(groupOrder, key)
		}
		g.VersionCount++
		if pkg.Status == softwarepackage.StatusUploading {
			g.HasUploading = true
		}

		// Collect catalogs from this version
		catalogs := pkg.Edges.Catalogs
		if len(catalogs) == 0 {
			if ref := pkg.Edges.GlobalRef; ref != nil {
				catalogs = ref.Edges.Catalogs
			}
		}
		for _, cat := range catalogs {
			found := false
			for _, existing := range g.Catalogs {
				if existing == cat.Name {
					found = true
					break
				}
			}
			if !found {
				g.Catalogs = append(g.Catalogs, cat.Name)
			}
		}
	}

	// Return in order
	result := make([]PackageGroup, 0, len(groupOrder))
	for _, key := range groupOrder {
		result = append(result, *groupMap[key])
	}
	return result, nil
}

// CountPackageGroups returns the number of distinct (name, platform) groups for a tenant.
func (m *Model) CountPackageGroups(tenantID string) (int, error) {
	groups, err := m.GetPackageGroups(tenantID)
	if err != nil {
		return 0, err
	}
	return len(groups), nil
}

// GetPackageVersions returns all versions of a package for a tenant.
func (m *Model) GetPackageVersions(tenantID int, name, platform string) ([]*ent.SoftwarePackage, error) {
	return m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.NameEQ(name),
			softwarepackage.PlatformEQ(softwarepackage.Platform(platform)),
		).
		WithCatalogs().
		WithRepo().
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs()
		}).
		Order(ent.Desc(softwarepackage.FieldCreated)).
		All(context.Background())
}

// GetDistinctPackageNames returns distinct (name, platform) pairs for use in assignment forms.
func (m *Model) GetDistinctPackageNames(tenantID int) ([]PackageGroup, error) {
	packages, err := m.Client.SoftwarePackage.Query().
		Where(softwarepackage.HasTenantWith(tenant.ID(tenantID))).
		Order(ent.Asc(softwarepackage.FieldName)).
		All(context.Background())
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []PackageGroup
	for _, pkg := range packages {
		key := pkg.Name + "|" + string(pkg.Platform)
		if !seen[key] {
			seen[key] = true
			displayName := pkg.DisplayName
			if displayName == "" {
				displayName = pkg.Name
			}
			result = append(result, PackageGroup{
				Name:        pkg.Name,
				Platform:    string(pkg.Platform),
				DisplayName: displayName,
			})
		}
	}
	return result, nil
}

// GetPackageAssignmentsByName returns assignments for a package name and platform.
func (m *Model) GetPackageAssignmentsByName(packageName, platform string, tenantID int) ([]*ent.SoftwareAssignment, error) {
	return m.Client.SoftwareAssignment.Query().
		Where(
			softwareassignment.PackageNameEQ(packageName),
			softwareassignment.PackagePlatformEQ(softwareassignment.PackagePlatform(platform)),
			softwareassignment.HasTenantWith(tenant.ID(tenantID)),
		).
		Order(ent.Asc(softwareassignment.FieldTargetType, softwareassignment.FieldTargetID)).
		All(context.Background())
}

// GetPackageByID returns a software package by ID with its catalogs and repo edges.
func (m *Model) GetPackageByID(packageID int) (*ent.SoftwarePackage, error) {
	return m.Client.SoftwarePackage.Query().
		Where(softwarepackage.ID(packageID)).
		WithCatalogs().
		WithRepo().
		WithTenant().
		Only(context.Background())
}

// CreatePackage creates a new software package.
func (m *Model) CreatePackage(tenantID int, name, displayName, version, platform, installerPath, category, developer, description string, sizeBytes int64, checksumSHA256 string, unattendedInstall bool, pkginfoData string, repoID int, catalogIDs []int, iconName string) (*ent.SoftwarePackage, error) {
	ctx := context.Background()

	creator := m.Client.SoftwarePackage.Create().
		SetName(name).
		SetDisplayName(displayName).
		SetVersion(version).
		SetPlatform(softwarepackage.Platform(platform)).
		SetInstallerPath(installerPath).
		SetCategory(category).
		SetDeveloper(developer).
		SetDescription(description).
		SetSizeBytes(sizeBytes).
		SetChecksumSha256(checksumSHA256).
		SetUnattendedInstall(unattendedInstall).
		SetPkginfoData(pkginfoData).
		SetIconName(iconName).
		SetStatus(softwarepackage.StatusUploading).
		SetRepoID(repoID).
		SetTenantID(tenantID).
		AddCatalogIDs(catalogIDs...)

	// If the repo is global, mark the package source as global
	if repoID > 0 {
		repo, err := m.Client.SoftwareRepo.Get(ctx, repoID)
		if err == nil && repo.RepoType == softwarerepo.RepoTypeGlobal {
			creator = creator.SetSource(softwarepackage.SourceGlobal)
		}
	}

	return creator.Save(ctx)
}

// UpdatePackage updates an existing software package's metadata.
func (m *Model) UpdatePackage(packageID int, name, displayName, version, platform, category, developer, description string, unattendedInstall bool, pkginfoData string, catalogIDs []int, iconName string) (*ent.SoftwarePackage, error) {
	ctx := context.Background()

	updater := m.Client.SoftwarePackage.UpdateOneID(packageID).
		SetName(name).
		SetDisplayName(displayName).
		SetVersion(version).
		SetPlatform(softwarepackage.Platform(platform)).
		SetCategory(category).
		SetDeveloper(developer).
		SetDescription(description).
		SetUnattendedInstall(unattendedInstall).
		SetPkginfoData(pkginfoData).
		ClearCatalogs().
		AddCatalogIDs(catalogIDs...)

	if iconName != "" {
		updater = updater.SetIconName(iconName)
	}

	return updater.Save(ctx)
}

// CountPackagesByName returns the number of packages with the given name, excluding the specified ID.
func (m *Model) CountPackagesByName(name string, excludeID int) (int, error) {
	return m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.NameEQ(name),
			softwarepackage.IDNEQ(excludeID),
		).
		Count(context.Background())
}

// SetPackageStatus updates the status of a software package.
func (m *Model) SetPackageStatus(packageID int, status softwarepackage.Status) error {
	return m.Client.SoftwarePackage.UpdateOneID(packageID).SetStatus(status).Exec(context.Background())
}

// DeletePackage deletes a software package by ID.
func (m *Model) DeletePackage(packageID int) error {
	return m.Client.SoftwarePackage.DeleteOneID(packageID).Exec(context.Background())
}

// GetGlobalPackageFamilies returns global package families (name+platform) that the given tenant
// has not yet subscribed to, grouped the same way as GetPackageGroups.
func (m *Model) GetGlobalPackageFamilies(tenantID int) ([]PackageGroup, error) {
	ctx := context.Background()

	// Get all global packages (source=global)
	globalPkgs, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.SourceEQ(softwarepackage.SourceGlobal),
		).
		WithCatalogs().
		Order(ent.Asc(softwarepackage.FieldName)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	// Get families this tenant already subscribes to (by name+platform)
	subscribedFamilies := make(map[string]bool)
	subs, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
		).
		All(ctx)
	if err == nil {
		for _, sub := range subs {
			key := sub.Name + "|" + string(sub.Platform)
			subscribedFamilies[key] = true
		}
	}

	// Group global packages by name+platform, excluding already subscribed families
	familyMap := make(map[string]*PackageGroup)
	var familyOrder []string
	for _, pkg := range globalPkgs {
		key := pkg.Name + "|" + string(pkg.Platform)
		if subscribedFamilies[key] {
			continue
		}
		g, exists := familyMap[key]
		if !exists {
			g = &PackageGroup{
				Name:        pkg.Name,
				Platform:    string(pkg.Platform),
				DisplayName: pkg.DisplayName,
				Category:    pkg.Category,
			}
			familyMap[key] = g
			familyOrder = append(familyOrder, key)
		}
		g.VersionCount++
		if g.LatestVersion == "" || compareVersions(pkg.Version, g.LatestVersion) > 0 {
			g.LatestVersion = pkg.Version
			if pkg.DisplayName != "" {
				g.DisplayName = pkg.DisplayName
			}
			if pkg.Category != "" {
				g.Category = pkg.Category
			}
		}
	}

	result := make([]PackageGroup, 0, len(familyOrder))
	for _, key := range familyOrder {
		result = append(result, *familyMap[key])
	}
	return result, nil
}

// ImportGlobalPackage creates a reference to a global package in a tenant's scope.
func (m *Model) ImportGlobalPackage(tenantID int, globalPackageID int, catalogIDs []int) (*ent.SoftwarePackage, error) {
	ctx := context.Background()

	// Get the global package
	globalPkg, err := m.Client.SoftwarePackage.Query().
		Where(softwarepackage.ID(globalPackageID)).
		WithRepo().
		Only(ctx)
	if err != nil {
		return nil, err
	}

	// Get the tenant's default repo (or use the global repo)
	repoID := 0
	if globalPkg.Edges.Repo != nil {
		repoID = globalPkg.Edges.Repo.ID
	}

	creator := m.Client.SoftwarePackage.Create().
		SetName(globalPkg.Name).
		SetDisplayName(globalPkg.DisplayName).
		SetVersion(globalPkg.Version).
		SetPlatform(globalPkg.Platform).
		SetInstallerPath(globalPkg.InstallerPath).
		SetCategory(globalPkg.Category).
		SetDeveloper(globalPkg.Developer).
		SetDescription(globalPkg.Description).
		SetSizeBytes(globalPkg.SizeBytes).
		SetChecksumSha256(globalPkg.ChecksumSha256).
		SetUnattendedInstall(globalPkg.UnattendedInstall).
		SetUnattendedUninstall(globalPkg.UnattendedUninstall).
		SetSource(softwarepackage.SourceGlobal).
		SetTenantID(tenantID).
		AddCatalogIDs(catalogIDs...)

	if repoID > 0 {
		creator = creator.SetRepoID(repoID)
	}

	return creator.Save(ctx)
}

// GetPackageInstallLogs returns install logs for a specific package.
func (m *Model) GetPackageInstallLogs(packageID int, limit int) ([]*ent.SoftwareInstallLog, error) {
	return m.Client.SoftwareInstallLog.Query().
		Where(softwareinstalllog.HasPackageWith(softwarepackage.ID(packageID))).
		WithAgent().
		Order(ent.Desc(softwareinstalllog.FieldCreated)).
		Limit(limit).
		All(context.Background())
}

// GetCatalogs returns all software catalogs for a tenant, ordered by ring_order.
// CatalogPackageEntry represents a package within a catalog ring view.
type CatalogPackageEntry struct {
	Name        string
	DisplayName string
	Version     string
	Platform    string
	IconName    string
	Developer   string
	IsSubscribed bool
}

// CatalogRing represents a catalog ring with its packages (own + subscribed).
type CatalogRing struct {
	Catalog  *ent.SoftwareCatalog
	Packages []CatalogPackageEntry
}

func (m *Model) GetCatalogs(tenantID string) ([]*ent.SoftwareCatalog, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	return m.Client.SoftwareCatalog.Query().
		Where(softwarecatalog.HasTenantWith(tenant.ID(id))).
		WithPackages().
		Order(ent.Asc(softwarecatalog.FieldRingOrder)).
		All(context.Background())
}

// GetCatalogRings returns catalog rings with both own and subscribed packages.
func (m *Model) GetCatalogRings(tenantID string) ([]CatalogRing, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	catalogs, err := m.Client.SoftwareCatalog.Query().
		Where(softwarecatalog.HasTenantWith(tenant.ID(id))).
		WithPackages().
		Order(ent.Asc(softwarecatalog.FieldRingOrder)).
		All(context.Background())
	if err != nil {
		return nil, err
	}

	// Fetch subscribed global packages with their catalogs
	subscriptions, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(id)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
		).
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs()
		}).
		All(context.Background())
	if err != nil {
		subscriptions = nil // non-fatal
	}

	var rings []CatalogRing
	for _, cat := range catalogs {
		ring := CatalogRing{Catalog: cat}
		seen := make(map[string]bool)

		// Own packages directly assigned to this catalog
		for _, pkg := range cat.Edges.Packages {
			key := pkg.Name + "-" + pkg.Version
			seen[key] = true
			ring.Packages = append(ring.Packages, CatalogPackageEntry{
				Name:        pkg.Name,
				DisplayName: pkg.DisplayName,
				Version:     pkg.Version,
				Platform:    string(pkg.Platform),
				IconName:    pkg.IconName,
				Developer:   pkg.Developer,
				IsSubscribed: false,
			})
		}

		// Subscribed global packages available in this ring
		for _, sub := range subscriptions {
			globalPkg := sub.Edges.GlobalRef
			if globalPkg == nil {
				continue
			}
			key := globalPkg.Name + "-" + globalPkg.Version
			if seen[key] {
				continue
			}
			// Check if global package has been promoted to this ring or lower
			available := false
			for _, gCat := range globalPkg.Edges.Catalogs {
				if gCat.RingOrder <= cat.RingOrder {
					available = true
					break
				}
			}
			if !available {
				continue
			}
			seen[key] = true
			ring.Packages = append(ring.Packages, CatalogPackageEntry{
				Name:        globalPkg.Name,
				DisplayName: globalPkg.DisplayName,
				Version:     globalPkg.Version,
				Platform:    string(globalPkg.Platform),
				IconName:    globalPkg.IconName,
				Developer:   globalPkg.Developer,
				IsSubscribed: true,
			})
		}

		rings = append(rings, ring)
	}

	return rings, nil
}

// InitializeDefaultCatalogs creates the default rollout rings for a tenant.
func (m *Model) InitializeDefaultCatalogs(tenantID int) error {
	ctx := context.Background()

	rings := []struct {
		Name      string
		Order     int
		IsDefault bool
	}{
		{"test", 0, false},
		{"first", 1, false},
		{"fast", 2, false},
		{"broad", 3, true},
	}

	for _, ring := range rings {
		_, err := m.Client.SoftwareCatalog.Create().
			SetName(ring.Name).
			SetRingOrder(ring.Order).
			SetIsDefault(ring.IsDefault).
			SetTenantID(tenantID).
			Save(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

// PromotePackageToCatalog adds a package to the next ring catalog.
func (m *Model) PromotePackageToCatalog(catalogID int, tenantID string) error {
	ctx := context.Background()

	tID, err := strconv.Atoi(tenantID)
	if err != nil {
		return err
	}

	// Get the current catalog
	currentCatalog, err := m.Client.SoftwareCatalog.Get(ctx, catalogID)
	if err != nil {
		return err
	}

	// Find the next ring
	nextCatalog, err := m.Client.SoftwareCatalog.Query().
		Where(
			softwarecatalog.HasTenantWith(tenant.ID(tID)),
			softwarecatalog.RingOrderEQ(currentCatalog.RingOrder+1),
		).
		Only(ctx)
	if err != nil {
		return err
	}

	// Get packages from the current catalog
	packages, err := currentCatalog.QueryPackages().All(ctx)
	if err != nil {
		return err
	}

	// Add all packages from current ring to the next ring
	pkgIDs := make([]int, len(packages))
	for i, pkg := range packages {
		pkgIDs[i] = pkg.ID
	}

	return m.Client.SoftwareCatalog.UpdateOneID(nextCatalog.ID).
		AddPackageIDs(pkgIDs...).
		Exec(ctx)
}

// GetAssignmentsByPage returns paginated software assignments for a tenant.
func (m *Model) GetAssignmentsByPage(p partials.PaginationAndSort, tenantID string) ([]*ent.SoftwareAssignment, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	query := m.Client.SoftwareAssignment.Query().
		Where(softwareassignment.HasTenantWith(tenant.ID(id))).
		Limit(p.PageSize).
		Offset((p.CurrentPage - 1) * p.PageSize)

	switch p.SortBy {
	case "package":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwareassignment.FieldPackageName))
		} else {
			query = query.Order(ent.Desc(softwareassignment.FieldPackageName))
		}
	case "assignment_type", "type":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwareassignment.FieldAssignmentType))
		} else {
			query = query.Order(ent.Desc(softwareassignment.FieldAssignmentType))
		}
	case "target_type":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwareassignment.FieldTargetType))
		} else {
			query = query.Order(ent.Desc(softwareassignment.FieldTargetType))
		}
	default:
		query = query.Order(ent.Desc(softwareassignment.FieldCreated))
	}

	return query.All(context.Background())
}

// CountAssignments returns the total number of assignments for a tenant.
func (m *Model) CountAssignments(tenantID string) (int, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return 0, err
	}

	return m.Client.SoftwareAssignment.Query().
		Where(softwareassignment.HasTenantWith(tenant.ID(id))).
		Count(context.Background())
}

// CreateAssignment creates a new software assignment by package name.
func (m *Model) CreateAssignment(tenantID int, packageName, packagePlatform, assignmentType, targetType, targetID string) (*ent.SoftwareAssignment, error) {
	return m.Client.SoftwareAssignment.Create().
		SetPackageName(packageName).
		SetPackagePlatform(softwareassignment.PackagePlatform(packagePlatform)).
		SetAssignmentType(softwareassignment.AssignmentType(assignmentType)).
		SetTargetType(softwareassignment.TargetType(targetType)).
		SetTargetID(targetID).
		SetActive(true).
		SetTenantID(tenantID).
		Save(context.Background())
}

// GetAssignmentByID returns a software assignment by ID with tenant edge loaded.
func (m *Model) GetAssignmentByID(assignmentID int) (*ent.SoftwareAssignment, error) {
	return m.Client.SoftwareAssignment.Query().
		Where(softwareassignment.ID(assignmentID)).
		WithTenant().
		Only(context.Background())
}

// DeleteAssignment deletes a software assignment by ID.
func (m *Model) DeleteAssignment(assignmentID int) error {
	return m.Client.SoftwareAssignment.DeleteOneID(assignmentID).Exec(context.Background())
}

// GetTagsForTenant returns all tags for a tenant (simple version for deploy assignments).
func (m *Model) GetTagsForTenant(tenantID int) ([]*ent.Tag, error) {
	return m.Client.Tag.Query().
		Where(tag.HasTenantWith(tenant.ID(tenantID))).
		All(context.Background())
}

// GetDeployDashboardStats returns deployment stats for the dashboard.
func (m *Model) GetDeployDashboardStats(tenantID string) (totalInstalled, totalPending, totalFailed, successRate int, err error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	ctx := context.Background()
	pkgFilter := softwareinstalllog.HasPackageWith(softwarepackage.HasTenantWith(tenant.ID(id)))

	totalInstalled, err = m.Client.SoftwareInstallLog.Query().
		Where(pkgFilter, softwareinstalllog.StatusEQ(softwareinstalllog.StatusSuccess)).
		Count(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	totalPending, err = m.Client.SoftwareInstallLog.Query().
		Where(pkgFilter, softwareinstalllog.StatusIn(
			softwareinstalllog.StatusPending,
			softwareinstalllog.StatusDownloading,
			softwareinstalllog.StatusInstalling,
		)).
		Count(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	totalFailed, err = m.Client.SoftwareInstallLog.Query().
		Where(pkgFilter, softwareinstalllog.StatusEQ(softwareinstalllog.StatusFailed)).
		Count(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	total := totalInstalled + totalFailed
	if total > 0 {
		successRate = (totalInstalled * 100) / total
	}

	return totalInstalled, totalPending, totalFailed, successRate, nil
}

// GetRecentInstallLogs returns the most recent install logs across all agents for a tenant.
func (m *Model) GetRecentInstallLogs(tenantID string, limit int) ([]*ent.SoftwareInstallLog, error) {
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	return m.Client.SoftwareInstallLog.Query().
		Where(softwareinstalllog.HasPackageWith(softwarepackage.HasTenantWith(tenant.ID(id)))).
		WithPackage().
		WithAgent().
		Order(ent.Desc(softwareinstalllog.FieldCreated)).
		Limit(limit).
		All(context.Background())
}

// GetAssignmentsForAgent returns all active assignments that apply to a specific agent
// (by direct agent target, by site, or by tag).
func (m *Model) GetAssignmentsForAgent(agentID string, tenantID string, agentOS string) ([]*ent.SoftwareAssignment, error) {
	tid, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()

	// Get agent with site and tags
	a, err := m.Client.Agent.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Collect all target conditions: agent ID, site IDs, tag IDs
	predicates := []predicate.SoftwareAssignment{
		softwareassignment.And(
			softwareassignment.TargetTypeEQ(softwareassignment.TargetTypeAgent),
			softwareassignment.TargetIDEQ(agentID),
		),
	}

	// Get site IDs
	sites, err := a.QuerySite().IDs(ctx)
	if err == nil {
		for _, siteID := range sites {
			predicates = append(predicates, softwareassignment.And(
				softwareassignment.TargetTypeEQ(softwareassignment.TargetTypeSite),
				softwareassignment.TargetIDEQ(fmt.Sprintf("%d", siteID)),
			))
		}
	}

	// Get tag IDs
	tags, err := a.QueryTags().IDs(ctx)
	if err == nil {
		for _, tagID := range tags {
			predicates = append(predicates, softwareassignment.And(
				softwareassignment.TargetTypeEQ(softwareassignment.TargetTypeTag),
				softwareassignment.TargetIDEQ(fmt.Sprintf("%d", tagID)),
			))
		}
	}

	// Build query with platform filter
	wherePredicates := []predicate.SoftwareAssignment{
		softwareassignment.HasTenantWith(tenant.ID(tid)),
		softwareassignment.ActiveEQ(true),
		softwareassignment.Or(predicates...),
	}

	// Filter by agent platform
	platform := strings.ToLower(agentOS)
	if platform == "macos" {
		platform = "darwin"
	}
	if platform == "darwin" {
		wherePredicates = append(wherePredicates, softwareassignment.PackagePlatformEQ(softwareassignment.PackagePlatformDarwin))
	} else if platform == "windows" {
		wherePredicates = append(wherePredicates, softwareassignment.PackagePlatformEQ(softwareassignment.PackagePlatformWindows))
	}

	return m.Client.SoftwareAssignment.Query().
		Where(wherePredicates...).
		Order(ent.Asc(softwareassignment.FieldPackageName)).
		All(ctx)
}

// GetInstallLogsForAgent returns install logs for a specific agent with package info.
func (m *Model) GetInstallLogsForAgent(agentID string, p partials.PaginationAndSort) ([]*ent.SoftwareInstallLog, error) {
	query := m.Client.SoftwareInstallLog.Query().
		Where(softwareinstalllog.HasAgentWith(agent.IDEQ(agentID))).
		WithPackage().
		Limit(p.PageSize).
		Offset((p.CurrentPage - 1) * p.PageSize)

	switch p.SortBy {
	case "status":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwareinstalllog.FieldStatus))
		} else {
			query = query.Order(ent.Desc(softwareinstalllog.FieldStatus))
		}
	case "action":
		if p.SortOrder == "asc" {
			query = query.Order(ent.Asc(softwareinstalllog.FieldAction))
		} else {
			query = query.Order(ent.Desc(softwareinstalllog.FieldAction))
		}
	default:
		query = query.Order(ent.Desc(softwareinstalllog.FieldCreated))
	}

	return query.All(context.Background())
}

// CountInstallLogsForAgent returns the total number of install logs for an agent.
func (m *Model) CountInstallLogsForAgent(agentID string) (int, error) {
	return m.Client.SoftwareInstallLog.Query().
		Where(softwareinstalllog.HasAgentWith(agent.IDEQ(agentID))).
		Count(context.Background())
}

// GetPackagesFromCatalog returns packages available in the agent's effective catalog,
// keyed by package name. For each name, returns the package with the highest version.
func (m *Model) GetPackagesFromCatalog(effectiveCatalog string, tenantID string) (map[string]*ent.SoftwarePackage, error) {
	rings, err := m.GetCatalogRings(tenantID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*ent.SoftwarePackage)

	// Find the target catalog ring order
	targetOrder := -1
	for _, ring := range rings {
		if ring.Catalog.Name == effectiveCatalog {
			targetOrder = ring.Catalog.RingOrder
			break
		}
	}
	if targetOrder == -1 {
		// Fallback: use "broad" (highest order)
		targetOrder = 999
	}

	// Get all packages from catalogs at or below the target ring order
	id, err := strconv.Atoi(tenantID)
	if err != nil {
		return nil, err
	}

	pkgs, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(id)),
		).
		WithCatalogs().
		All(context.Background())
	if err != nil {
		return nil, err
	}

	for _, pkg := range pkgs {
		// Check if package is available in the effective catalog
		available := false
		for _, cat := range pkg.Edges.Catalogs {
			if cat.RingOrder <= targetOrder {
				available = true
				break
			}
		}
		if !available {
			continue
		}
		// Keep the entry (or replace if we already have one — last one wins)
		if existing, ok := result[pkg.Name]; !ok || compareVersions(pkg.Version, existing.Version) > 0 {
			result[pkg.Name] = pkg
		}
	}

	return result, nil
}

// GetLatestInstallStatusForAgent returns the latest install log per package for an agent.
func (m *Model) GetLatestInstallStatusForAgent(agentID string) ([]*ent.SoftwareInstallLog, error) {
	// Get all logs for this agent, ordered by created desc
	allLogs, err := m.Client.SoftwareInstallLog.Query().
		Where(softwareinstalllog.HasAgentWith(agent.IDEQ(agentID))).
		WithPackage().
		Order(ent.Desc(softwareinstalllog.FieldCreated)).
		All(context.Background())
	if err != nil {
		return nil, err
	}

	// Keep only the latest log per package
	seen := make(map[int]bool)
	var latest []*ent.SoftwareInstallLog
	for _, l := range allLogs {
		if l.Edges.Package == nil {
			continue
		}
		if !seen[l.Edges.Package.ID] {
			seen[l.Edges.Package.ID] = true
			latest = append(latest, l)
		}
	}
	return latest, nil
}

// compareVersions compares two version strings numerically by splitting on ".".
// Returns 1 if a > b, -1 if a < b, 0 if equal.
func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var na, nb int
		if i < len(partsA) {
			na, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			nb, _ = strconv.Atoi(partsB[i])
		}
		if na > nb {
			return 1
		}
		if na < nb {
			return -1
		}
	}
	return 0
}

// GetErrorLogsForAgent returns install logs with errors for an agent.
func (m *Model) GetErrorLogsForAgent(agentID string) ([]*ent.SoftwareInstallLog, error) {
	return m.Client.SoftwareInstallLog.Query().
		Where(
			softwareinstalllog.HasAgentWith(agent.IDEQ(agentID)),
			softwareinstalllog.StatusEQ(softwareinstalllog.StatusFailed),
		).
		WithPackage().
		Order(ent.Desc(softwareinstalllog.FieldCreated)).
		Limit(20).
		All(context.Background())
}

// ringOrderMap maps ring names to their order for comparison.
var ringOrderMap = map[string]int{
	"test":  0,
	"first": 1,
	"fast":  2,
	"broad": 3,
}

// GetEffectiveRing determines the rollout ring for an agent.
// Priority: Agent override > Tag (lowest ring wins) > Site > Default ("broad").
func (m *Model) GetEffectiveRing(agentID string) (ring string, source string) {
	ctx := context.Background()

	agentObj, err := m.Client.Agent.Get(ctx, agentID)
	if err != nil {
		return "broad", "default"
	}

	// 1. Direct agent override
	if agentObj.CatalogRing != nil && *agentObj.CatalogRing != "" {
		return *agentObj.CatalogRing, "agent"
	}

	// 2. Tags — lowest ring_order wins
	tags, err := agentObj.QueryTags().All(ctx)
	if err == nil {
		lowestOrder := 999
		ringFromTag := ""
		tagName := ""
		for _, t := range tags {
			if t.CatalogRing != nil && *t.CatalogRing != "" {
				order, ok := ringOrderMap[*t.CatalogRing]
				if ok && order < lowestOrder {
					lowestOrder = order
					ringFromTag = *t.CatalogRing
					tagName = t.Tag
				}
			}
		}
		if ringFromTag != "" {
			return ringFromTag, fmt.Sprintf("tag:%s", tagName)
		}
	}

	// 3. Site-level ring
	site, err := agentObj.QuerySite().Only(ctx)
	if err == nil && site.CatalogRing != nil && *site.CatalogRing != "" {
		return *site.CatalogRing, fmt.Sprintf("site:%s", site.Description)
	}

	// 4. Default
	return "broad", "default"
}

// SubscribeGlobalPackageFamily subscribes a tenant to ALL versions of a global package family.
// Each global version gets a subscription entry with a global_ref edge.
func (m *Model) SubscribeGlobalPackageFamily(tenantID int, name, platform string) error {
	ctx := context.Background()

	// Get all global packages matching this family
	globalPkgs, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.SourceEQ(softwarepackage.SourceGlobal),
			softwarepackage.NameEQ(name),
			softwarepackage.PlatformEQ(softwarepackage.Platform(platform)),
		).
		WithRepo().
		All(ctx)
	if err != nil {
		return fmt.Errorf("could not find global packages: %w", err)
	}
	if len(globalPkgs) == 0 {
		return fmt.Errorf("no global packages found for %s (%s)", name, platform)
	}

	// Get already subscribed global IDs for this tenant+family
	alreadySubscribed := make(map[int]bool)
	existing, err := m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
			softwarepackage.NameEQ(name),
			softwarepackage.PlatformEQ(softwarepackage.Platform(platform)),
		).
		WithGlobalRef().
		All(ctx)
	if err == nil {
		for _, sub := range existing {
			if sub.Edges.GlobalRef != nil {
				alreadySubscribed[sub.Edges.GlobalRef.ID] = true
			}
		}
	}

	// Create subscription for each version not yet subscribed
	for _, globalPkg := range globalPkgs {
		if alreadySubscribed[globalPkg.ID] {
			continue
		}

		creator := m.Client.SoftwarePackage.Create().
			SetName(globalPkg.Name).
			SetDisplayName(globalPkg.DisplayName).
			SetVersion(globalPkg.Version).
			SetPlatform(globalPkg.Platform).
			SetInstallerPath(globalPkg.InstallerPath).
			SetCategory(globalPkg.Category).
			SetDeveloper(globalPkg.Developer).
			SetDescription(globalPkg.Description).
			SetSizeBytes(globalPkg.SizeBytes).
			SetChecksumSha256(globalPkg.ChecksumSha256).
			SetUnattendedInstall(globalPkg.UnattendedInstall).
			SetUnattendedUninstall(globalPkg.UnattendedUninstall).
			SetPkginfoData(globalPkg.PkginfoData).
			SetSource(softwarepackage.SourceGlobalSubscription).
			SetGlobalRefID(globalPkg.ID).
			SetTenantID(tenantID)

		if globalPkg.Edges.Repo != nil {
			creator = creator.SetRepoID(globalPkg.Edges.Repo.ID)
		}

		if _, err := creator.Save(ctx); err != nil {
			return fmt.Errorf("could not subscribe to %s %s: %w", globalPkg.Name, globalPkg.Version, err)
		}
	}

	return nil
}

// UnsubscribeGlobalPackageFamily removes all subscriptions for a package family from a tenant.
func (m *Model) UnsubscribeGlobalPackageFamily(tenantID int, name, platform string) error {
	ctx := context.Background()

	_, err := m.Client.SoftwarePackage.Delete().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
			softwarepackage.NameEQ(name),
			softwarepackage.PlatformEQ(softwarepackage.Platform(platform)),
		).
		Exec(ctx)
	return err
}

// GetSubscribedPackages returns all global package subscriptions for a tenant with their global refs.
func (m *Model) GetSubscribedPackages(tenantID int) ([]*ent.SoftwarePackage, error) {
	return m.Client.SoftwarePackage.Query().
		Where(
			softwarepackage.HasTenantWith(tenant.ID(tenantID)),
			softwarepackage.SourceEQ(softwarepackage.SourceGlobalSubscription),
		).
		WithGlobalRef(func(q *ent.SoftwarePackageQuery) {
			q.WithCatalogs().WithRepo()
		}).
		Order(ent.Asc(softwarepackage.FieldName)).
		All(context.Background())
}

// IsPackageAvailableForRing checks if a package (or its global ref) has been promoted
// to a ring that is accessible for the given client ring.
// A client in ring "test" sees packages in test+first+fast+broad.
// A client in ring "broad" sees only packages promoted to broad.
func (m *Model) IsPackageAvailableForRing(pkg *ent.SoftwarePackage, clientRing string) bool {
	clientOrder, ok := ringOrderMap[clientRing]
	if !ok {
		clientOrder = 3 // default to broad
	}

	// For subscribed packages, check the global package's catalogs
	if pkg.Source == softwarepackage.SourceGlobalSubscription && pkg.Edges.GlobalRef != nil {
		return m.packageInRingOrLower(pkg.Edges.GlobalRef, clientOrder)
	}

	// For own packages, check this package's catalogs
	return m.packageInRingOrLower(pkg, clientOrder)
}

// packageInRingOrLower checks if a package is in any catalog with ring_order <= clientOrder.
func (m *Model) packageInRingOrLower(pkg *ent.SoftwarePackage, clientOrder int) bool {
	catalogs := pkg.Edges.Catalogs
	if catalogs == nil {
		// Load catalogs if not eager-loaded
		loaded, err := pkg.QueryCatalogs().All(context.Background())
		if err != nil {
			return false
		}
		catalogs = loaded
	}

	for _, cat := range catalogs {
		if cat.RingOrder <= clientOrder {
			return true
		}
	}
	return false
}

