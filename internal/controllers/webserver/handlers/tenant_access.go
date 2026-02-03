package handlers

import (
	"net/http"
	"strconv"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/EigerCode/openuem-console/internal/models"
	"github.com/EigerCode/openuem-console/internal/views/partials"
)

// TenantAccessMiddleware checks if the authenticated user has access to the requested tenant
func (h *Handler) TenantAccessMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Get user ID from session
		username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
		if username == "" {
			return h.Login(c)
		}

		// Get tenant ID from URL parameter
		tenantIDStr := c.Param("tenant")
		if tenantIDStr == "" || tenantIDStr == "-1" {
			// No specific tenant requested, continue
			return next(c)
		}

		tenantID, err := strconv.Atoi(tenantIDStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"))
		}

		// Check if user has access to this tenant
		hasAccess, err := h.Model.UserHasAccessToTenant(username, tenantID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		if !hasAccess {
			return echo.NewHTTPError(http.StatusForbidden, i18n.T(c.Request().Context(), "tenants.no_access"))
		}

		// Store tenant access info in context for later use
		c.Set("tenant_id", tenantID)
		c.Set("user_id", username)

		return next(c)
	}
}

// TenantAdminMiddleware checks if the user is an admin in the current tenant
func (h *Handler) TenantAdminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Get user ID from session
		username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
		if username == "" {
			return h.Login(c)
		}

		// Get tenant ID from URL parameter
		tenantIDStr := c.Param("tenant")
		if tenantIDStr == "" {
			return echo.NewHTTPError(http.StatusBadRequest, i18n.T(c.Request().Context(), "tenants.tenant_required"))
		}

		tenantID, err := strconv.Atoi(tenantIDStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"))
		}

		// Check if user is admin in this tenant
		isAdmin, err := h.Model.IsUserTenantAdmin(username, tenantID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		if !isAdmin {
			return echo.NewHTTPError(http.StatusForbidden, i18n.T(c.Request().Context(), "tenants.admin_required"))
		}

		return next(c)
	}
}

// SuperAdminMiddleware checks if the user is an admin in the hoster tenant (for global settings)
// This replaces the old SuperAdmin concept - now only admins of the hoster tenant can access global settings
func (h *Handler) SuperAdminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Get user ID from session
		username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
		if username == "" {
			return h.Login(c)
		}

		// Get hoster tenant
		hosterTenant, err := h.Model.GetHosterTenant()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		// Check if user is admin in the hoster tenant
		isHosterAdmin, err := h.Model.IsUserTenantAdmin(username, hosterTenant.ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		if !isHosterAdmin {
			return echo.NewHTTPError(http.StatusForbidden, i18n.T(c.Request().Context(), "tenants.hoster_admin_required"))
		}

		return next(c)
	}
}

// TenantOperatorMiddleware checks if the user is an admin OR operator in the tenant (for settings access)
func (h *Handler) TenantOperatorMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Get user ID from session
		username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
		if username == "" {
			return h.Login(c)
		}

		// Get tenant ID from URL parameter
		tenantIDStr := c.Param("tenant")
		if tenantIDStr == "" {
			return echo.NewHTTPError(http.StatusBadRequest, i18n.T(c.Request().Context(), "tenants.tenant_required"))
		}

		tenantID, err := strconv.Atoi(tenantIDStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, i18n.T(c.Request().Context(), "tenants.invalid_tenant_id"))
		}

		// Check if user is admin or operator in this tenant
		role, err := h.Model.GetUserRoleInTenant(username, tenantID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		if role != models.UserTenantRoleAdmin && role != models.UserTenantRoleOperator {
			return echo.NewHTTPError(http.StatusForbidden, i18n.T(c.Request().Context(), "tenants.operator_required"))
		}

		return next(c)
	}
}

// GetCurrentUserTenantRole returns the role of the current user in the current tenant
func (h *Handler) GetCurrentUserTenantRole(c echo.Context) (string, error) {
	username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
	if username == "" {
		return "", nil
	}

	// Get tenant ID
	tenantIDStr := c.Param("tenant")
	if tenantIDStr == "" || tenantIDStr == "-1" {
		return "", nil
	}

	tenantID, err := strconv.Atoi(tenantIDStr)
	if err != nil {
		return "", err
	}

	role, err := h.Model.GetUserRoleInTenant(username, tenantID)
	if err != nil {
		return "", err
	}

	return string(role), nil
}

// GetUserAccessibleTenants returns all tenants the current user can access
func (h *Handler) GetUserAccessibleTenants(c echo.Context) ([]*partials.TenantInfo, error) {
	username := h.SessionManager.Manager.GetString(c.Request().Context(), "uid")
	if username == "" {
		return nil, nil
	}

	tenants, err := h.Model.GetTenantsForUser(username)
	if err != nil {
		return nil, err
	}

	result := make([]*partials.TenantInfo, 0, len(tenants))
	for _, t := range tenants {
		role, _ := h.Model.GetUserRoleInTenant(username, t.ID)
		result = append(result, &partials.TenantInfo{
			ID:          t.ID,
			Description: t.Description,
			IsDefault:   t.IsDefault,
			IsHoster:    t.IsHosterTenant,
			UserRole:    string(role),
		})
	}

	return result, nil
}
