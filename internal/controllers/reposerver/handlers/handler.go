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

	// Catalogs — dynamic per ring (test, first, fast, broad)
	repo.GET("/catalogs/:ring", h.GetCatalog)

	// Health check
	repo.GET("/health", h.HealthCheck)
}
