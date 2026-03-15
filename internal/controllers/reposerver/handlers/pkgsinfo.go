package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// GetPkg handles package download requests by redirecting to a pre-signed S3 URL.
// Munki requests: /repo/pkgs/<installer_item_location>
func (h *Handler) GetPkg(c echo.Context) error {
	path := c.Param("*")
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "package path required")
	}

	agentID, err := h.extractAgentID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}

	var tenantID int
	tenantIDStr, err := h.extractTenantID(c)
	if err == nil {
		tenantID, err = strconv.Atoi(tenantIDStr)
	}
	if err != nil {
		tenantID, err = h.Model.GetAgentTenantID(agentID)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "could not determine tenant")
		}
	}

	log.Printf("[REPO]: pkg download request from agent %s for %s (tenant %d)", agentID, path, tenantID)

	// Look up which repo this package belongs to and generate presigned URL
	ctx := context.Background()
	repoType, err := h.Model.GetPackageRepoType(tenantID, path)
	if err != nil {
		log.Printf("[REPO]: could not determine repo type for %s: %v", path, err)
		repoType = "tenant" // default fallback
	}

	presignedURL, err := h.Model.GetPresignedURL(ctx, tenantID, path, repoType)
	if err != nil {
		log.Printf("[REPO]: could not generate presigned URL for %s: %v", path, err)
		return echo.NewHTTPError(http.StatusNotFound, "package not available")
	}

	return c.Redirect(http.StatusTemporaryRedirect, presignedURL)
}

// GetPkgsInfo returns package info for a specific package.
// Munki gets most info from the catalog, so this is a secondary endpoint.
func (h *Handler) GetPkgsInfo(c echo.Context) error {
	path := c.Param("*")
	log.Printf("[REPO]: pkgsinfo request for %s (not yet implemented)", path)
	return echo.NewHTTPError(http.StatusNotFound, "pkgsinfo not available")
}

// GetIcon returns an app icon for Managed Software Center.
// Icons are stored in S3 under icons/<icon_name> and served via pre-signed URL redirect.
func (h *Handler) GetIcon(c echo.Context) error {
	path := c.Param("*")
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "icon path required")
	}

	agentID, err := h.extractAgentID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}

	var tenantID int
	tenantIDStr, err := h.extractTenantID(c)
	if err == nil {
		tenantID, err = strconv.Atoi(tenantIDStr)
	}
	if err != nil {
		tenantID, err = h.Model.GetAgentTenantID(agentID)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "could not determine tenant")
		}
	}

	log.Printf("[REPO]: icon request from agent %s for %s (tenant %d)", agentID, path, tenantID)

	iconPath := "icons/" + path
	ctx := context.Background()

	// Try tenant repo first, then global
	presignedURL, err := h.Model.GetPresignedURL(ctx, tenantID, iconPath, "tenant")
	if err != nil {
		presignedURL, err = h.Model.GetPresignedURL(ctx, tenantID, iconPath, "global")
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "icon not available")
		}
	}

	return c.Redirect(http.StatusTemporaryRedirect, presignedURL)
}
