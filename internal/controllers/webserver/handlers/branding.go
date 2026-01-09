package handlers

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	"github.com/invopop/ctxi18n/i18n"
	"github.com/labstack/echo/v4"
	"github.com/open-uem/ent"
	"github.com/open-uem/openuem-console/internal/views/admin_views"
	"github.com/open-uem/openuem-console/internal/views/partials"
)

// GetBrandingSettings handles GET /admin/branding
func (h *Handler) GetBrandingSettings(c echo.Context) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	branding, err := h.Model.GetOrCreateBranding()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, admin_views.BrandingSettings(c, branding, commonInfo, ""))
}

// PostBrandingLogoLight handles POST /admin/branding/logo-light
func (h *Handler) PostBrandingLogoLight(c echo.Context) error {
	return h.handleLogoUpload(c, "light")
}

// PostBrandingLogoDark handles POST /admin/branding/logo-dark
func (h *Handler) PostBrandingLogoDark(c echo.Context) error {
	return h.handleLogoUpload(c, "dark")
}

// PostBrandingLogoSmall handles POST /admin/branding/logo-small
func (h *Handler) PostBrandingLogoSmall(c echo.Context) error {
	return h.handleLogoUpload(c, "small")
}

// DeleteBrandingLogoLight handles DELETE /admin/branding/logo-light
func (h *Handler) DeleteBrandingLogoLight(c echo.Context) error {
	if err := h.Model.DeleteLogoLight(); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.logo_deleted"))
}

// DeleteBrandingLogoDark handles DELETE /admin/branding/logo-dark
func (h *Handler) DeleteBrandingLogoDark(c echo.Context) error {
	if err := h.Model.DeleteLogoDark(); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.logo_deleted"))
}

// DeleteBrandingLogoSmall handles DELETE /admin/branding/logo-small
func (h *Handler) DeleteBrandingLogoSmall(c echo.Context) error {
	if err := h.Model.DeleteLogoSmall(); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.logo_deleted"))
}

// PostBrandingColors handles POST /admin/branding/colors
func (h *Handler) PostBrandingColors(c echo.Context) error {
	primary := c.FormValue("primary_color")
	secondary := c.FormValue("secondary_color")
	accent := c.FormValue("accent_color")

	// Use text input if color picker value is empty
	if c.FormValue("primary_color_text") != "" {
		primary = c.FormValue("primary_color_text")
	}
	if c.FormValue("secondary_color_text") != "" {
		secondary = c.FormValue("secondary_color_text")
	}
	if c.FormValue("accent_color_text") != "" {
		accent = c.FormValue("accent_color_text")
	}

	if err := h.Model.UpdateColors(primary, secondary, accent); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.saved"))
}

// PostBrandingText handles POST /admin/branding/text
func (h *Handler) PostBrandingText(c echo.Context) error {
	branding, err := h.Model.GetOrCreateBranding()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	branding.ProductName = c.FormValue("product_name")
	branding.SupportEmail = c.FormValue("support_email")
	branding.SupportURL = c.FormValue("support_url")
	branding.TermsURL = c.FormValue("terms_url")
	branding.PrivacyURL = c.FormValue("privacy_url")
	branding.FooterText = c.FormValue("footer_text")
	branding.ShowPoweredBy = c.FormValue("show_powered_by") == "on"

	if err := h.Model.UpdateBranding(branding); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.saved"))
}

// PostBrandingLogin handles POST /admin/branding/login
func (h *Handler) PostBrandingLogin(c echo.Context) error {
	branding, err := h.Model.GetOrCreateBranding()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	branding.LoginWelcomeText = c.FormValue("login_welcome_text")

	// Handle background image upload
	file, err := c.FormFile("login_background")
	if err == nil && file != nil {
		src, err := file.Open()
		if err != nil {
			return RenderError(c, partials.ErrorMessage(err.Error(), true))
		}
		defer src.Close()

		data, err := io.ReadAll(src)
		if err != nil {
			return RenderError(c, partials.ErrorMessage(err.Error(), true))
		}

		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "branding.invalid_image"), true))
		}

		base64Data := base64.StdEncoding.EncodeToString(data)
		branding.LoginBackgroundImage = "data:" + mimeType + ";base64," + base64Data
	}

	if err := h.Model.UpdateBranding(branding); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.saved"))
}

// DeleteBrandingLoginBackground handles DELETE /admin/branding/login-background
func (h *Handler) DeleteBrandingLoginBackground(c echo.Context) error {
	branding, err := h.Model.GetBranding()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	branding.LoginBackgroundImage = ""
	if err := h.Model.UpdateBranding(branding); err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.logo_deleted"))
}

// handleLogoUpload processes logo file uploads
func (h *Handler) handleLogoUpload(c echo.Context, logoType string) error {
	file, err := c.FormFile("logo")
	if err != nil {
		return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "branding.no_file_selected"), true))
	}

	src, err := file.Open()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	// Detect MIME type
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		// Check for SVG (DetectContentType returns text/xml for SVG)
		if strings.HasSuffix(strings.ToLower(file.Filename), ".svg") {
			mimeType = "image/svg+xml"
		} else {
			return RenderError(c, partials.ErrorMessage(i18n.T(c.Request().Context(), "branding.invalid_image"), true))
		}
	}

	// Convert to base64 data URL
	base64Data := base64.StdEncoding.EncodeToString(data)
	dataURL := "data:" + mimeType + ";base64," + base64Data

	// Save based on logo type
	var saveErr error
	switch logoType {
	case "light":
		saveErr = h.Model.SaveLogoLight(dataURL)
	case "dark":
		saveErr = h.Model.SaveLogoDark(dataURL)
	case "small":
		saveErr = h.Model.SaveLogoSmall(dataURL)
	}

	if saveErr != nil {
		return RenderError(c, partials.ErrorMessage(saveErr.Error(), true))
	}

	return h.renderBrandingWithSuccess(c, i18n.T(c.Request().Context(), "branding.logo_uploaded"))
}

// renderBrandingWithSuccess renders the branding page with a success message
func (h *Handler) renderBrandingWithSuccess(c echo.Context, message string) error {
	commonInfo, err := h.GetCommonInfo(c)
	if err != nil {
		return err
	}

	branding, err := h.Model.GetOrCreateBranding()
	if err != nil {
		return RenderError(c, partials.ErrorMessage(err.Error(), true))
	}

	return RenderView(c, admin_views.BrandingSettings(c, branding, commonInfo, message))
}

// GetBrandingForViews returns branding data for use in views
func (h *Handler) GetBrandingForViews() (*ent.Branding, error) {
	return h.Model.GetOrCreateBranding()
}
