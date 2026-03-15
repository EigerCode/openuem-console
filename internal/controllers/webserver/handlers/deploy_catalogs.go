package handlers

import (
	"strconv"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

func (h *Handler) DeployCatalogsInit(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int"), true))
	}

	if err := h.Model.InitializeDefaultCatalogs(tenantID); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_catalogs.initialized"))
}

func (h *Handler) DeployCatalogPromote(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	catalogID, err := strconv.Atoi(c.Param("catalogId"))
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_catalogs.promote_error"), true))
	}

	if err := h.Model.PromotePackageToCatalog(catalogID, commonInfo.TenantID); err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "deploy_catalogs.promote_error")+": "+err.Error(), true))
	}

	return h.DeployPackages(c, i18n.T(c.Request().Context(), "deploy_catalogs.promoted"))
}
