package handlers

import (
	"strconv"
	"strings"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/views/deploy_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

func (h *Handler) DeployAssignments(c echo.Context, successMessage string) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	p := partials.NewPaginationAndSort(20)
	p.GetPaginationAndSortParams(c.QueryParam("page"), c.QueryParam("pageSize"), c.QueryParam("sortBy"), c.QueryParam("sortOrder"), "created", 20)
	p.NItems, err = h.Model.CountAssignments(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	assignments, err := h.Model.GetAssignmentsByPage(p, commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, deploy_views.DeployIndex("| Assignments", deploy_views.Assignments(c, p, assignments, 20, commonInfo, successMessage), commonInfo))
}

func (h *Handler) DeployAssignmentNew(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	// Get distinct package names for this tenant
	packageNames, err := h.Model.GetDistinctPackageNames(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Get sites and tags for target selection
	sites, err := h.Model.GetSites(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	tags, err := h.Model.GetTagsForTenant(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, deploy_views.DeployIndex("| New Assignment", deploy_views.AssignmentForm(c, packageNames, sites, tags, commonInfo, ""), commonInfo))
}

func (h *Handler) DeployAssignmentCreate(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	// Parse "name|platform" format from datalist
	packageValue := c.FormValue("assignment-package")
	var packageName, packagePlatform string
	if idx := strings.Index(packageValue, "|"); idx > 0 {
		packageName = packageValue[:idx]
		packagePlatform = packageValue[idx+1:]
	}

	assignmentType := c.FormValue("assignment-type")
	targetType := c.FormValue("assignment-target-type")

	var targetID string
	switch targetType {
	case "site":
		targetID = c.FormValue("assignment-target-site")
	case "tag":
		targetID = c.FormValue("assignment-target-tag")
	case "agent":
		targetID = c.FormValue("assignment-target-agent")
	}
	// Extract ID from "ID|Label" format used by datalist inputs
	if idx := strings.Index(targetID, "|"); idx > 0 {
		targetID = targetID[:idx]
	}

	if packageName == "" || targetID == "" {
		packageNames, _ := h.Model.GetDistinctPackageNames(tenantID)
		sites, _ := h.Model.GetSites(tenantID)
		tags, _ := h.Model.GetTagsForTenant(tenantID)
		return RenderView(c, deploy_views.DeployIndex("| New Assignment", deploy_views.AssignmentForm(c, packageNames, sites, tags, commonInfo, i18n.T(c.Request().Context(), "deploy_assignments.required_fields")), commonInfo))
	}

	_, err = h.Model.CreateAssignment(tenantID, packageName, packagePlatform, assignmentType, targetType, targetID)
	if err != nil {
		packageNames, _ := h.Model.GetDistinctPackageNames(tenantID)
		sites, _ := h.Model.GetSites(tenantID)
		tags, _ := h.Model.GetTagsForTenant(tenantID)
		return RenderView(c, deploy_views.DeployIndex("| New Assignment", deploy_views.AssignmentForm(c, packageNames, sites, tags, commonInfo, err.Error()), commonInfo))
	}

	return h.DeployAssignments(c, i18n.T(c.Request().Context(), "deploy_assignments.created"))
}

func (h *Handler) DeployAssignmentDelete(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	assignmentID, err := strconv.Atoi(c.Param("assignmentId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_assignments.invalid_id"), true))
	}

	// Verify the assignment belongs to the current tenant
	assignment, err := h.Model.GetAssignmentByID(assignmentID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	if assignment.Edges.Tenant != nil && strconv.Itoa(assignment.Edges.Tenant.ID) != commonInfo.TenantID {
		return RenderError(c, partials.ErrorMessage("access denied", true))
	}

	if err := h.Model.DeleteAssignment(assignmentID); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.DeployAssignments(c, i18n.T(c.Request().Context(), "deploy_assignments.deleted"))
}
