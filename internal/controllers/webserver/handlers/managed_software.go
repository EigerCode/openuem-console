package handlers

import (
	"log"
	"strconv"
	"time"

	ent "github.com/open-uem/ent"
	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/views/computers_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

func (h *Handler) ComputerManagedSoftware(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	agentId := c.Param("uuid")
	if agentId == "" {
		return RenderError(c, partials.ErrorMessage("an error occurred getting uuid param", false))
	}

	agent, err := h.Model.GetAgentById(agentId, commonInfo)
	if err != nil {
		return RenderView(c, computers_views.InventoryIndex(" | Inventory", partials.Error(c, err.Error(), "Computers", partials.GetNavigationUrl(commonInfo, "/computers"), commonInfo), commonInfo))
	}

	assignments, err := h.Model.GetAssignmentsForAgent(agentId, commonInfo.TenantID, agent.Os)
	if err != nil {
		log.Printf("[ERROR]: could not get assignments for agent: %v", err)
	}

	effectiveCatalog, _ := h.Model.GetEffectiveRing(agentId)

	// Load packages from the agent's effective catalog
	packageMap, err := h.Model.GetPackagesFromCatalog(effectiveCatalog, commonInfo.TenantID)
	if err != nil {
		log.Printf("[ERROR]: could not get packages from catalog: %v", err)
		packageMap = map[string]*ent.SoftwarePackage{}
	}

	statusLogs, err := h.Model.GetLatestInstallStatusForAgent(agentId)
	if err != nil {
		log.Printf("[ERROR]: could not get install status for agent: %v", err)
	}

	errorLogs, err := h.Model.GetErrorLogsForAgent(agentId)
	if err != nil {
		log.Printf("[ERROR]: could not get error logs for agent: %v", err)
	}

	confirmDelete := c.QueryParam("delete") != ""

	tenantID, err := strconv.Atoi(commonInfo.TenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "tenants.could_not_convert_to_int", err.Error()), true))
	}
	settings, err := h.Model.GetNetbirdSettings(tenantID)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "netbird.could_not_get_settings", err.Error()), true))
	}
	netbird := settings.AccessToken != ""

	offline := h.IsAgentOffline(c)

	return RenderView(c, computers_views.InventoryIndex(" | Managed Software", computers_views.ManagedSoftware(c, agent, assignments, packageMap, statusLogs, errorLogs, effectiveCatalog, confirmDelete, commonInfo, netbird, offline), commonInfo))
}

func (h *Handler) ComputerSoftwareCheck(c echo.Context) error {
	agentId := c.Param("uuid")
	if agentId == "" {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "agents.no_empty_id"), false))
	}

	if h.NATSConnection == nil || !h.NATSConnection.IsConnected() {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nats.not_connected"), false))
	}

	if _, err := h.NATSConnection.Request("agent.softwarecheck."+agentId, nil, time.Duration(h.NATSTimeout)*time.Second); err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "nats.request_error", err.Error()), true))
	}

	return RenderSuccess(c, partials.SuccessMessage(i18n.T(c.Request().Context(), "managed_software.check_success")))
}
