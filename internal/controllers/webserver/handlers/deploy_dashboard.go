package handlers

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/views/deploy_views"
)

func (h *Handler) DeployDashboardView(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	totalInstalled, totalPending, totalFailed, successRate, _ := h.Model.GetDeployDashboardStats(commonInfo.TenantID)

	recentLogs, err := h.Model.GetRecentInstallLogs(commonInfo.TenantID, 20)
	if err != nil {
		log.Printf("[ERROR]: could not get recent install logs: %v", err)
	}

	data := deploy_views.DeployDashboardData{
		TotalInstalled: totalInstalled,
		TotalPending:   totalPending,
		TotalFailed:    totalFailed,
		SuccessRate:    successRate,
		RecentLogs:     recentLogs,
	}

	return RenderView(c, deploy_views.DeployIndex("| Deploy Dashboard", deploy_views.DeployDashboard(c, data, commonInfo), commonInfo))
}
