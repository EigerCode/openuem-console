package models

import (
	"context"
	"strconv"

	"github.com/EigerCode/ent/agent"
	"github.com/EigerCode/ent/printer"
	"github.com/EigerCode/ent/site"
	"github.com/EigerCode/ent/tenant"
	"github.com/EigerCode/openuem-console/internal/views/partials"
)

func (m *Model) CountDifferentPrinters(c *partials.CommonInfo) (int, error) {
	siteID, err := strconv.Atoi(c.SiteID)
	if err != nil {
		return 0, err
	}
	tenantID, err := strconv.Atoi(c.TenantID)
	if err != nil {
		return 0, err
	}

	if siteID == -1 {
		return m.Client.Printer.Query().Where(printer.HasOwnerWith(agent.HasSiteWith(site.HasTenantWith(tenant.ID(tenantID))))).Select(printer.FieldName).Unique(true).Count(context.Background())
	} else {
		return m.Client.Printer.Query().Where(printer.HasOwnerWith(agent.HasSiteWith(site.ID(siteID), site.HasTenantWith(tenant.ID(tenantID))))).Select(printer.FieldName).Unique(true).Count(context.Background())
	}
}
