package handlers

import (
	"strconv"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	ent "github.com/open-uem/ent"
	"github.com/open-uem/openuem-console/internal/views/admin_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

func (h *Handler) SoftwareRepos(c echo.Context, successMessage string) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID := -1
	if commonInfo.TenantID != "-1" {
		tenantID, err = strconv.Atoi(commonInfo.TenantID)
		if err != nil {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
		}
	}

	repos, err := h.Model.GetSoftwareRepos(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()

	return RenderView(c, admin_views.SoftwareReposIndex("| Software Repos", admin_views.SoftwareRepos(c, repos, agentsExists, serversExists, commonInfo, h.GetAdminTenantName(commonInfo), successMessage), commonInfo))
}

func (h *Handler) SoftwareRepoNew(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()
	tenantName := h.GetAdminTenantName(commonInfo)

	return RenderView(c, admin_views.SoftwareReposIndex("| New Software Repo", admin_views.SoftwareRepoForm(c, &ent.SoftwareRepo{}, true, agentsExists, serversExists, commonInfo, tenantName, ""), commonInfo))
}

func (h *Handler) SoftwareRepoEdit(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	repoID, err := strconv.Atoi(c.Param("repoId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "software_repos.invalid_id"), true))
	}

	repo, err := h.Model.GetSoftwareRepoByID(repoID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	agentsExists, _ := h.Model.AgentsExists(commonInfo)
	serversExists, _ := h.Model.ServersExists()
	tenantName := h.GetAdminTenantName(commonInfo)

	return RenderView(c, admin_views.SoftwareReposIndex("| Edit Software Repo", admin_views.SoftwareRepoForm(c, repo, false, agentsExists, serversExists, commonInfo, tenantName, ""), commonInfo))
}

func (h *Handler) SoftwareRepoCreate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID := -1
	if commonInfo.TenantID != "-1" {
		tenantID, err = strconv.Atoi(commonInfo.TenantID)
		if err != nil {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
		}
	}

	name := c.FormValue("repo-name")
	endpoint := c.FormValue("repo-endpoint")
	bucket := c.FormValue("repo-bucket")
	region := c.FormValue("repo-region")
	accessKey := c.FormValue("repo-access-key")
	secretKey := c.FormValue("repo-secret-key")
	basePath := c.FormValue("repo-base-path")
	isDefault, _ := strconv.ParseBool(c.FormValue("repo-is-default"))

	// Type is determined by context: global admin = "global", tenant admin = "tenant"
	repoType := "tenant"
	if tenantID <= 0 {
		repoType = "global"
	}

	if name == "" || endpoint == "" || bucket == "" {
		agentsExists, _ := h.Model.AgentsExists(commonInfo)
		serversExists, _ := h.Model.ServersExists()
		tenantName := h.GetAdminTenantName(commonInfo)
		return RenderView(c, admin_views.SoftwareReposIndex("| New Software Repo", admin_views.SoftwareRepoForm(c, &ent.SoftwareRepo{}, true, agentsExists, serversExists, commonInfo, tenantName, i18n.T(c.Request().Context(), "software_repos.required_fields")), commonInfo))
	}

	// Pre-signed URLs always enabled with 4h TTL
	_, err = h.Model.CreateSoftwareRepo(tenantID, name, repoType, endpoint, bucket, region, accessKey, secretKey, basePath, true, 14400, isDefault)
	if err != nil {
		agentsExists, _ := h.Model.AgentsExists(commonInfo)
		serversExists, _ := h.Model.ServersExists()
		tenantName := h.GetAdminTenantName(commonInfo)
		return RenderView(c, admin_views.SoftwareReposIndex("| New Software Repo", admin_views.SoftwareRepoForm(c, &ent.SoftwareRepo{}, true, agentsExists, serversExists, commonInfo, tenantName, err.Error()), commonInfo))
	}

	return h.SoftwareRepos(c, i18n.T(c.Request().Context(), "software_repos.created"))
}

func (h *Handler) SoftwareRepoUpdate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	repoID, err := strconv.Atoi(c.Param("repoId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "software_repos.invalid_id"), true))
	}

	name := c.FormValue("repo-name")
	endpoint := c.FormValue("repo-endpoint")
	bucket := c.FormValue("repo-bucket")
	region := c.FormValue("repo-region")
	accessKey := c.FormValue("repo-access-key")
	secretKey := c.FormValue("repo-secret-key")
	basePath := c.FormValue("repo-base-path")
	isDefault, _ := strconv.ParseBool(c.FormValue("repo-is-default"))

	if name == "" || endpoint == "" || bucket == "" {
		repo, _ := h.Model.GetSoftwareRepoByID(repoID)
		agentsExists, _ := h.Model.AgentsExists(commonInfo)
		serversExists, _ := h.Model.ServersExists()
		tenantName := h.GetAdminTenantName(commonInfo)
		return RenderView(c, admin_views.SoftwareReposIndex("| Edit Software Repo", admin_views.SoftwareRepoForm(c, repo, false, agentsExists, serversExists, commonInfo, tenantName, i18n.T(c.Request().Context(), "software_repos.required_fields")), commonInfo))
	}

	// Pre-signed URLs always enabled with 4h TTL
	_, err = h.Model.UpdateSoftwareRepo(repoID, name, endpoint, bucket, region, accessKey, secretKey, basePath, true, 14400, isDefault)
	if err != nil {
		repo, _ := h.Model.GetSoftwareRepoByID(repoID)
		agentsExists, _ := h.Model.AgentsExists(commonInfo)
		serversExists, _ := h.Model.ServersExists()
		tenantName := h.GetAdminTenantName(commonInfo)
		return RenderView(c, admin_views.SoftwareReposIndex("| Edit Software Repo", admin_views.SoftwareRepoForm(c, repo, false, agentsExists, serversExists, commonInfo, tenantName, err.Error()), commonInfo))
	}

	return h.SoftwareRepos(c, i18n.T(c.Request().Context(), "software_repos.updated"))
}

func (h *Handler) SoftwareRepoDelete(c echo.Context) error {
	repoID, err := strconv.Atoi(c.Param("repoId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "software_repos.invalid_id"), true))
	}

	if err := h.Model.DeleteSoftwareRepo(repoID); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.SoftwareRepos(c, i18n.T(c.Request().Context(), "software_repos.deleted"))
}

func (h *Handler) SoftwareRepoTestConnection(c echo.Context) error {
	repoID, err := strconv.Atoi(c.Param("repoId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "software_repos.invalid_id"), true))
	}

	repo, err := h.Model.GetSoftwareRepoByID(repoID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	if err := h.Model.TestSoftwareRepoConnection(repo.Endpoint, repo.Bucket, repo.Region, repo.AccessKey, repo.SecretKey, repo.BasePath); err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "software_repos.connection_failed")+": "+err.Error(), true))
	}

	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "software_repos.connection_success")))
}
