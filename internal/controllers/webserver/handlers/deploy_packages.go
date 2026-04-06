package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	ent "github.com/open-uem/ent"
	"github.com/open-uem/ent/softwarepackage"
	"github.com/open-uem/openuem-console/internal/common/pkgextract"
	"github.com/open-uem/openuem-console/internal/common/s3storage"
	"github.com/open-uem/openuem-console/internal/models"
	"github.com/open-uem/openuem-console/internal/views/deploy_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

func (h *Handler) DeployPackages(c echo.Context, successMessage string) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	// Ensure default catalogs exist for this tenant
	tenantID, _ := strconv.Atoi(commonInfo.TenantID)
	if tenantID > 0 {
		_ = h.Model.EnsureDefaultCatalogs(tenantID)
	}

	// Catalog filter
	catalogFilter := c.FormValue("filterByCatalog0")

	packages, err := h.Model.GetPackageList(commonInfo.TenantID, catalogFilter)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Search filter
	search := c.FormValue("filterBySearch")
	if search != "" {
		searchLower := strings.ToLower(search)
		var filtered []models.PackageListEntry
		for _, pkg := range packages {
			displayName := pkg.DisplayName
			if displayName == "" {
				displayName = pkg.Name
			}
			if strings.Contains(strings.ToLower(displayName), searchLower) ||
				strings.Contains(strings.ToLower(pkg.Name), searchLower) ||
				strings.Contains(strings.ToLower(pkg.Category), searchLower) ||
				strings.Contains(strings.ToLower(pkg.Developer), searchLower) {
				filtered = append(filtered, pkg)
			}
		}
		packages = filtered
	}

	// Sort
	sortBy := c.FormValue("sortBy")
	sortOrder := c.FormValue("sortOrder")
	if sortBy == "" {
		sortBy = "name"
		sortOrder = "asc"
	}
	sort.Slice(packages, func(i, j int) bool {
		var vi, vj string
		switch sortBy {
		case "name":
			vi = packages[i].DisplayName
			if vi == "" {
				vi = packages[i].Name
			}
			vj = packages[j].DisplayName
			if vj == "" {
				vj = packages[j].Name
			}
		case "version":
			if len(packages[i].Versions) > 0 {
				vi = packages[i].Versions[0].Version
			}
			if len(packages[j].Versions) > 0 {
				vj = packages[j].Versions[0].Version
			}
		case "platform":
			vi, vj = packages[i].Platform, packages[j].Platform
		case "category":
			vi, vj = packages[i].Category, packages[j].Category
		default:
			vi = packages[i].DisplayName
			if vi == "" {
				vi = packages[i].Name
			}
			vj = packages[j].DisplayName
			if vj == "" {
				vj = packages[j].Name
			}
		}
		vi, vj = strings.ToLower(vi), strings.ToLower(vj)
		if sortOrder == "desc" {
			return vi > vj
		}
		return vi < vj
	})

	p := partials.PaginationAndSort{
		SortBy:    sortBy,
		SortOrder: sortOrder,
		PageSize:  1000,
	}

	// Check if tenant has a software repo configured
	tenantIDInt, _ := strconv.Atoi(commonInfo.TenantID)
	repos, _ := h.Model.GetSoftwareRepos(tenantIDInt)
	hasRepo := len(repos) > 0
	if !hasRepo && commonInfo.CurrentTenantIsMain {
		// Main tenant can use global repos
		globalRepos, _ := h.Model.GetSoftwareRepos(0)
		hasRepo = len(globalRepos) > 0
	}

	// Get catalog names for filter dropdown
	catalogs, _ := h.Model.GetCatalogs(commonInfo.TenantID)
	var catalogNames []string
	for _, cat := range catalogs {
		catalogNames = append(catalogNames, cat.Name)
	}

	return RenderView(c, deploy_views.DeployIndex("| Packages", deploy_views.Packages(c, packages, p, search, catalogFilter, catalogNames, commonInfo, successMessage, hasRepo), commonInfo))
}

// DeployPackageFamily shows all versions of a package grouped by name.
func (h *Handler) DeployPackageFamily(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	name := c.QueryParam("name")
	platform := c.QueryParam("platform")
	if name == "" || platform == "" {
		return RenderError(c, partials.ErrorMessage("name and platform required", true))
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	versions, err := h.Model.GetPackageVersions(tenantID, name, platform)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	assignments, err := h.Model.GetPackageAssignmentsByName(name, platform, tenantID)
	if err != nil {
		assignments = nil
	}

	// Check if tenant has a software repo configured
	repos, _ := h.Model.GetSoftwareRepos(tenantID)
	hasRepo := len(repos) > 0

	return RenderView(c, deploy_views.DeployIndex("| "+name, deploy_views.PackageFamily(c, name, platform, versions, assignments, commonInfo, hasRepo), commonInfo))
}

func (h *Handler) DeployPackageNew(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	return RenderView(c, deploy_views.DeployIndex("| Upload Package", deploy_views.PackageUploadForm(c, commonInfo, ""), commonInfo))
}

func (h *Handler) DeployPackageAnalyze(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	file, err := c.FormFile("pkg-file")
	if err != nil {
		return RenderView(c, deploy_views.DeployIndex("| Upload Package", deploy_views.PackageUploadForm(c, commonInfo, i18n.T(c.Request().Context(), "deploy_packages.required_fields")), commonInfo))
	}

	src, err := file.Open()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	defer src.Close()

	fileBytes, err := io.ReadAll(src)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "openuem-pkg-*"+filepath.Ext(file.Filename))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	if _, err := tmpFile.Write(fileBytes); err != nil {
		tmpFile.Close()
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	tmpFile.Close()

	// Extract metadata
	meta, _ := pkgextract.Extract(file.Filename, fileBytes)

	// Build metadata map for the template
	metaMap := map[string]string{
		"filename":       file.Filename,
		"temp_file":      tmpFile.Name(),
		"size":           formatBytes(int64(len(fileBytes))),
		"name":           "",
		"display_name":   "",
		"version":        "",
		"platform":       "",
		"developer":      "",
		"description":    "",
		"category":       "",
		"pkginfo_data":   "",
	}
	if meta != nil {
		metaMap["name"] = meta.Name
		metaMap["display_name"] = meta.DisplayName
		metaMap["version"] = meta.Version
		metaMap["platform"] = meta.Platform
		metaMap["developer"] = meta.Developer
		metaMap["description"] = meta.Description
		metaMap["category"] = meta.Category
		if meta.Platform == "windows" {
			metaMap["pkginfo_data"] = meta.ToYAML()
		} else {
			metaMap["pkginfo_data"] = meta.ToPlist()
		}
	}

	repos := h.getReposWithGlobal(tenantID)

	catalogs, err := h.Model.GetCatalogs(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, deploy_views.DeployIndex("| Upload Package", deploy_views.PackageAnalyzeResult(c, metaMap, repos, catalogs, commonInfo, ""), commonInfo))
}

// getReposWithGlobal returns tenant repos and, for main tenant admins, also global repos.
func (h *Handler) getReposWithGlobal(tenantID int) []*ent.SoftwareRepo {
	var repos []*ent.SoftwareRepo

	if tenantID <= 0 {
		// Admin context (no specific tenant): show global repos + main tenant repos
		globalRepos, _ := h.Model.GetSoftwareRepos(-1)
		repos = append(repos, globalRepos...)
		mainTenant, err := h.Model.GetMainTenant()
		if err == nil {
			tenantRepos, _ := h.Model.GetSoftwareRepos(mainTenant.ID)
			repos = append(repos, tenantRepos...)
		}
	} else {
		repos, _ = h.Model.GetSoftwareRepos(tenantID)
		isMain, _ := h.Model.IsMainTenant(tenantID)
		if isMain {
			globalRepos, err := h.Model.GetSoftwareRepos(-1)
			if err == nil {
				repos = append(globalRepos, repos...)
			}
		}
	}

	return repos
}

// validateTempFilePath ensures the path is inside the OS temp directory and matches the expected pattern.
func validateTempFilePath(path string) error {
	cleanPath := filepath.Clean(path)
	tmpDir := os.TempDir()
	if !strings.HasPrefix(cleanPath, tmpDir) {
		return fmt.Errorf("path is not inside temp directory")
	}
	base := filepath.Base(cleanPath)
	if !strings.HasPrefix(base, "openuem-pkg-") {
		return fmt.Errorf("path does not match expected temp file pattern")
	}
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (h *Handler) DeployPackageCreate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	name := c.FormValue("pkg-name")
	displayName := c.FormValue("pkg-display-name")
	version := c.FormValue("pkg-version")
	platform := c.FormValue("pkg-platform")
	category := c.FormValue("pkg-category")
	developer := c.FormValue("pkg-developer")
	description := c.FormValue("pkg-description")
	repoID, _ := strconv.Atoi(c.FormValue("pkg-repo-id"))
	unattendedInstall, _ := strconv.ParseBool(c.FormValue("pkg-unattended-install"))
	pkginfoData := c.FormValue("pkg-pkginfo-data")
	tempFilePath := c.FormValue("pkg-temp-file")
	originalFilename := c.FormValue("pkg-original-filename")

	// Parse catalog IDs
	catalogIDStrs := c.Request().Form["pkg-catalogs"]
	var catalogIDs []int
	for _, idStr := range catalogIDStrs {
		id, err := strconv.Atoi(idStr)
		if err == nil {
			catalogIDs = append(catalogIDs, id)
		}
	}

	if name == "" || version == "" {
		return RenderView(c, deploy_views.DeployIndex("| Upload Package", deploy_views.PackageUploadForm(c, commonInfo, i18n.T(c.Request().Context(), "deploy_packages.required_fields")), commonInfo))
	}

	// Validate temp file path to prevent path traversal
	if err := validateTempFilePath(tempFilePath); err != nil {
		return RenderError(c, partials.ErrorMessage("Invalid file path. Please upload again.", true))
	}

	// Read file from temp location
	fileBytes, err := os.ReadFile(tempFilePath)
	if err != nil {
		return RenderError(c, partials.ErrorMessage("Temporary file not found. Please upload again.", true))
	}

	checksumSHA256 := fmt.Sprintf("%x", sha256.Sum256(fileBytes))
	sizeBytes := int64(len(fileBytes))

	// Sanitize path components to prevent path traversal in S3
	safeName := filepath.Base(strings.ReplaceAll(name, "..", ""))
	safeVersion := filepath.Base(strings.ReplaceAll(version, "..", ""))
	safeFilename := filepath.Base(originalFilename)

	// Determine installer path within the S3 bucket
	installerPath := fmt.Sprintf("%s/%s/%s", safeName, safeVersion, safeFilename)
	iconNameResult := ""

	// Read icon file bytes if provided (before goroutine, since request body is only available now)
	var iconBytes []byte
	iconFile, iconErr := c.FormFile("pkg-icon")
	if iconErr == nil && iconFile != nil {
		iconSrc, err := iconFile.Open()
		if err == nil {
			iconBytes, _ = io.ReadAll(iconSrc)
			iconSrc.Close()
			iconNameResult = safeName + ".png"
		}
	}

	// Create package record in DB immediately with status "uploading"
	pkg, err := h.Model.CreatePackage(tenantID, name, displayName, version, platform, installerPath, category, developer, description, sizeBytes, checksumSHA256, unattendedInstall, pkginfoData, repoID, catalogIDs, iconNameResult)
	if err != nil {
		os.Remove(tempFilePath)
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Upload to S3 in background
	if repoID > 0 {
		go func(packageID int, tempFile string) {
			defer os.Remove(tempFile)

			repo, err := h.Model.GetSoftwareRepoByID(repoID)
			if err != nil {
				_ = h.Model.SetPackageStatus(packageID, softwarepackage.StatusError)
				return
			}

			s3Client, err := s3storage.New(s3storage.Config{
				Endpoint:  repo.Endpoint,
				Bucket:    repo.Bucket,
				Region:    repo.Region,
				AccessKey: repo.AccessKey,
				SecretKey: repo.SecretKey,
				BasePath:  repo.BasePath,
			})
			if err != nil {
				_ = h.Model.SetPackageStatus(packageID, softwarepackage.StatusError)
				return
			}

			contentType := mime.TypeByExtension(filepath.Ext(originalFilename))
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			if err := s3Client.Upload(context.Background(), installerPath, bytes.NewReader(fileBytes), contentType); err != nil {
				_ = h.Model.SetPackageStatus(packageID, softwarepackage.StatusError)
				return
			}

			// Upload icon if provided
			if len(iconBytes) > 0 {
				iconPath := "icons/" + safeName + ".png"
				_ = s3Client.Upload(context.Background(), iconPath, bytes.NewReader(iconBytes), "image/png")
			}

			_ = h.Model.SetPackageStatus(packageID, softwarepackage.StatusReady)
		}(pkg.ID, tempFilePath)
	} else {
		os.Remove(tempFilePath)
		_ = h.Model.SetPackageStatus(pkg.ID, softwarepackage.StatusReady)
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_packages.created"))
}

func (h *Handler) DeployPackageDetail(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	packageID, err := strconv.Atoi(c.Param("packageId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_packages.invalid_id"), true))
	}

	pkg, err := h.Model.GetPackageByID(packageID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	logs, err := h.Model.GetPackageInstallLogs(packageID, 20)
	if err != nil {
		logs = nil
	}

	tenantID, _ := strconv.Atoi(commonInfo.TenantID)
	assignments, err := h.Model.GetPackageAssignmentsByName(pkg.Name, string(pkg.Platform), tenantID)
	if err != nil {
		assignments = nil
	}

	return RenderView(c, deploy_views.DeployIndex("| Package Detail", deploy_views.PackageDetail(c, pkg, logs, assignments, commonInfo), commonInfo))
}

func (h *Handler) DeployPackageEdit(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	packageID, err := strconv.Atoi(c.Param("packageId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_packages.invalid_id"), true))
	}

	pkg, err := h.Model.GetPackageByID(packageID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	repos := h.getReposWithGlobal(tenantID)

	catalogs, err := h.Model.GetCatalogs(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, deploy_views.DeployIndex("| Edit Package", deploy_views.PackageEditForm(c, pkg, repos, catalogs, commonInfo, ""), commonInfo))
}

func (h *Handler) DeployPackageUpdate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	packageID, err := strconv.Atoi(c.Param("packageId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_packages.invalid_id"), true))
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	name := c.FormValue("pkg-name")
	displayName := c.FormValue("pkg-display-name")
	version := c.FormValue("pkg-version")
	platform := c.FormValue("pkg-platform")
	category := c.FormValue("pkg-category")
	developer := c.FormValue("pkg-developer")
	description := c.FormValue("pkg-description")
	unattendedInstall, _ := strconv.ParseBool(c.FormValue("pkg-unattended-install"))
	pkginfoData := c.FormValue("pkg-pkginfo-data")

	catalogIDStrs := c.Request().Form["pkg-catalogs"]
	var catalogIDs []int
	for _, idStr := range catalogIDStrs {
		id, err := strconv.Atoi(idStr)
		if err == nil {
			catalogIDs = append(catalogIDs, id)
		}
	}

	if name == "" || version == "" {
		pkg, _ := h.Model.GetPackageByID(packageID)
		repos := h.getReposWithGlobal(tenantID)
		catalogs, _ := h.Model.GetCatalogs(commonInfo.TenantID)
		return RenderView(c, deploy_views.DeployIndex("| Edit Package", deploy_views.PackageEditForm(c, pkg, repos, catalogs, commonInfo, i18n.T(c.Request().Context(), "deploy_packages.required_fields")), commonInfo))
	}

	// Handle icon upload
	iconName := ""
	iconFile, err := c.FormFile("pkg-icon")
	if err == nil && iconFile != nil {
		iconSrc, err := iconFile.Open()
		if err == nil {
			defer iconSrc.Close()
			iconBytes, err := io.ReadAll(iconSrc)
			if err == nil {
				repos := h.getReposWithGlobal(tenantID)
				if len(repos) > 0 {
					repo := repos[0]
					s3Client, err := s3storage.New(s3storage.Config{
						Endpoint:  repo.Endpoint,
						Bucket:    repo.Bucket,
						Region:    repo.Region,
						AccessKey: repo.AccessKey,
						SecretKey: repo.SecretKey,
						BasePath:  repo.BasePath,
					})
					if err == nil {
						iconName = name + ".png"
						iconPath := "icons/" + iconName
						_ = s3Client.Upload(c.Request().Context(), iconPath, bytes.NewReader(iconBytes), "image/png")
					}
				}
			}
		}
	}

	_, err = h.Model.UpdatePackage(packageID, name, displayName, version, platform, category, developer, description, unattendedInstall, pkginfoData, catalogIDs, iconName)
	if err != nil {
		pkg, _ := h.Model.GetPackageByID(packageID)
		repos := h.getReposWithGlobal(tenantID)
		catalogs, _ := h.Model.GetCatalogs(commonInfo.TenantID)
		return RenderView(c, deploy_views.DeployIndex("| Edit Package", deploy_views.PackageEditForm(c, pkg, repos, catalogs, commonInfo, err.Error()), commonInfo))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_packages.updated"))
}

func (h *Handler) DeployPackageImport(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	families, err := h.Model.GetGlobalPackageFamilies(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, deploy_views.DeployIndex("| Subscribe Package", deploy_views.PackageSubscribeForm(c, families, commonInfo, ""), commonInfo))
}

func (h *Handler) DeployPackageImportCreate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	// Parse "name|platform" format from form
	packageValue := c.FormValue("import-package")
	var packageName, packagePlatform string
	if idx := strings.Index(packageValue, "|"); idx > 0 {
		packageName = packageValue[:idx]
		packagePlatform = packageValue[idx+1:]
	}

	if packageName == "" || packagePlatform == "" {
		families, _ := h.Model.GetGlobalPackageFamilies(tenantID)
		return RenderView(c, deploy_views.DeployIndex("| Subscribe Package", deploy_views.PackageSubscribeForm(c, families, commonInfo, i18n.T(c.Request().Context(), "deploy_packages.required_fields")), commonInfo))
	}

	err = h.Model.SubscribeGlobalPackageFamily(tenantID, packageName, packagePlatform)
	if err != nil {
		families, _ := h.Model.GetGlobalPackageFamilies(tenantID)
		return RenderView(c, deploy_views.DeployIndex("| Subscribe Package", deploy_views.PackageSubscribeForm(c, families, commonInfo, err.Error()), commonInfo))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_packages.subscribed_success"))
}

func (h *Handler) DeployPackageFamilyDelete(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	name := c.QueryParam("name")
	platform := c.QueryParam("platform")
	if name == "" || platform == "" {
		return RenderError(c, partials.ErrorMessage("missing name or platform", true))
	}

	if err := h.Model.UnsubscribeGlobalPackageFamily(tenantID, name, platform); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_packages.unsubscribed_success"))
}

func (h *Handler) DeployPackageDelete(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	packageID, err := strconv.Atoi(c.Param("packageId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_packages.invalid_id"), true))
	}

	// Fetch package details before deleting so we can clean up S3
	pkg, err := h.Model.GetPackageByID(packageID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Verify the package belongs to the current tenant
	if pkg.Edges.Tenant != nil && strconv.Itoa(pkg.Edges.Tenant.ID) != commonInfo.TenantID {
		return RenderError(c, partials.ErrorMessage("access denied", true))
	}

	// Delete files from S3 storage
	if pkg.Edges.Repo != nil {
		repo := pkg.Edges.Repo
		s3Client, err := s3storage.New(s3storage.Config{
			Endpoint:  repo.Endpoint,
			Bucket:    repo.Bucket,
			Region:    repo.Region,
			AccessKey: repo.AccessKey,
			SecretKey: repo.SecretKey,
			BasePath:  repo.BasePath,
		})
		if err == nil {
			// Delete installer file
			if pkg.InstallerPath != "" {
				_ = s3Client.Delete(c.Request().Context(), pkg.InstallerPath)
			}
			// Delete icon only if no other versions of this app exist
			if pkg.IconName != "" {
				otherVersions, _ := h.Model.CountPackagesByName(pkg.Name, packageID)
				if otherVersions == 0 {
					_ = s3Client.Delete(c.Request().Context(), "icons/"+pkg.IconName)
				}
			}
		}
	}

	if err := h.Model.DeletePackage(packageID); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_packages.deleted"))
}

// DeployPackageIcon serves a package icon from S3 to the browser.
func (h *Handler) DeployPackageIcon(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return c.NoContent(404)
	}

	iconName := filepath.Base(c.Param("iconName"))
	if iconName == "" || iconName == "." {
		return c.NoContent(404)
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return c.NoContent(404)
	}

	repos := h.getReposWithGlobal(tenantID)
	for _, repo := range repos {
		s3Client, err := s3storage.New(s3storage.Config{
			Endpoint:  repo.Endpoint,
			Bucket:    repo.Bucket,
			Region:    repo.Region,
			AccessKey: repo.AccessKey,
			SecretKey: repo.SecretKey,
			BasePath:  repo.BasePath,
		})
		if err != nil {
			continue
		}

		iconPath := "icons/" + iconName
		exists, _ := s3Client.Exists(c.Request().Context(), iconPath)
		if !exists {
			continue
		}

		body, err := s3Client.Download(c.Request().Context(), iconPath)
		if err != nil {
			continue
		}

		iconBytes, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			continue
		}

		c.Response().Header().Set("Cache-Control", "public, max-age=3600")
		return c.Blob(200, "image/png", iconBytes)
	}

	return c.NoContent(404)
}
