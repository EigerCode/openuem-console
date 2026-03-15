package handlers

import (
	"crypto/x509"

	"github.com/labstack/echo/v4"
	"github.com/open-uem/openuem-console/internal/models"
)

// Handler holds dependencies for repo server request handlers.
type Handler struct {
	Model  *models.Model
	CACert *x509.Certificate
}

// NewHandler creates a new Handler.
func NewHandler(m *models.Model, caCert *x509.Certificate) *Handler {
	return &Handler{
		Model:  m,
		CACert: caCert,
	}
}

// Register registers all repo server routes.
func (h *Handler) Register(e *echo.Echo) {
	// Munki/CIMIAN repo endpoints
	repo := e.Group("/repo")

	// Manifests — dynamic per serial number
	repo.GET("/manifests/:serial", h.GetManifest)

	// Catalogs — dynamic per catalog name
	repo.GET("/catalogs/:catalog", h.GetCatalog)

	// Package downloads — redirects to pre-signed S3 URL
	repo.GET("/pkgs/*", h.GetPkg)

	// Package info — individual package metadata (plist/yaml)
	repo.GET("/pkgsinfo/*", h.GetPkgsInfo)

	// Icons — app icons for Managed Software Center
	repo.GET("/icons/*", h.GetIcon)

	// Health check
	repo.GET("/health", h.HealthCheck)
}
