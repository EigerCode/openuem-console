package models

import (
	"context"

	"github.com/open-uem/ent"
)

// GetBranding retrieves the global branding settings.
// There should only be one branding record (singleton pattern).
func (m *Model) GetBranding() (*ent.Branding, error) {
	return m.Client.Branding.Query().First(context.Background())
}

// GetOrCreateBranding retrieves branding settings or creates default if not exists.
func (m *Model) GetOrCreateBranding() (*ent.Branding, error) {
	b, err := m.Client.Branding.Query().First(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			// Create default branding
			return m.Client.Branding.Create().
				SetProductName("OpenUEM").
				SetPrimaryColor("#16a34a").
				SetSecondaryColor("#6d28d9").
				SetShowPoweredBy(true).
				Save(context.Background())
		}
		return nil, err
	}
	return b, nil
}

// UpdateBranding updates the global branding settings.
func (m *Model) UpdateBranding(b *ent.Branding) error {
	update := m.Client.Branding.UpdateOneID(b.ID)

	// Logo settings
	if b.LogoLight != "" {
		update = update.SetLogoLight(b.LogoLight)
	} else {
		update = update.ClearLogoLight()
	}
	if b.LogoDark != "" {
		update = update.SetLogoDark(b.LogoDark)
	} else {
		update = update.ClearLogoDark()
	}
	if b.LogoSmall != "" {
		update = update.SetLogoSmall(b.LogoSmall)
	} else {
		update = update.ClearLogoSmall()
	}

	// Colors
	if b.PrimaryColor != "" {
		update = update.SetPrimaryColor(b.PrimaryColor)
	}
	if b.SecondaryColor != "" {
		update = update.SetSecondaryColor(b.SecondaryColor)
	}
	if b.AccentColor != "" {
		update = update.SetAccentColor(b.AccentColor)
	} else {
		update = update.ClearAccentColor()
	}

	// Text settings
	if b.ProductName != "" {
		update = update.SetProductName(b.ProductName)
	}
	if b.SupportURL != "" {
		update = update.SetSupportURL(b.SupportURL)
	} else {
		update = update.ClearSupportURL()
	}
	if b.SupportEmail != "" {
		update = update.SetSupportEmail(b.SupportEmail)
	} else {
		update = update.ClearSupportEmail()
	}
	if b.TermsURL != "" {
		update = update.SetTermsURL(b.TermsURL)
	} else {
		update = update.ClearTermsURL()
	}
	if b.PrivacyURL != "" {
		update = update.SetPrivacyURL(b.PrivacyURL)
	} else {
		update = update.ClearPrivacyURL()
	}

	// Login page
	if b.LoginBackgroundImage != "" {
		update = update.SetLoginBackgroundImage(b.LoginBackgroundImage)
	} else {
		update = update.ClearLoginBackgroundImage()
	}
	if b.LoginWelcomeText != "" {
		update = update.SetLoginWelcomeText(b.LoginWelcomeText)
	} else {
		update = update.ClearLoginWelcomeText()
	}

	// Footer
	if b.FooterText != "" {
		update = update.SetFooterText(b.FooterText)
	} else {
		update = update.ClearFooterText()
	}
	update = update.SetShowPoweredBy(b.ShowPoweredBy)

	return update.Exec(context.Background())
}

// SaveLogoLight saves the light mode logo.
func (m *Model) SaveLogoLight(logoData string) error {
	b, err := m.GetOrCreateBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		SetLogoLight(logoData).
		Exec(context.Background())
}

// SaveLogoDark saves the dark mode logo.
func (m *Model) SaveLogoDark(logoData string) error {
	b, err := m.GetOrCreateBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		SetLogoDark(logoData).
		Exec(context.Background())
}

// SaveLogoSmall saves the small logo/favicon.
func (m *Model) SaveLogoSmall(logoData string) error {
	b, err := m.GetOrCreateBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		SetLogoSmall(logoData).
		Exec(context.Background())
}

// UpdateColors updates the color scheme.
func (m *Model) UpdateColors(primary, secondary, accent string) error {
	b, err := m.GetOrCreateBranding()
	if err != nil {
		return err
	}
	
	update := m.Client.Branding.UpdateOneID(b.ID)
	
	if primary != "" {
		update = update.SetPrimaryColor(primary)
	}
	if secondary != "" {
		update = update.SetSecondaryColor(secondary)
	}
	if accent != "" {
		update = update.SetAccentColor(accent)
	}
	
	return update.Exec(context.Background())
}

// BrandingExists checks if branding settings exist.
func (m *Model) BrandingExists() (bool, error) {
	return m.Client.Branding.Query().Exist(context.Background())
}

// DeleteLogoLight removes the light mode logo.
func (m *Model) DeleteLogoLight() error {
	b, err := m.GetBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		ClearLogoLight().
		Exec(context.Background())
}

// DeleteLogoDark removes the dark mode logo.
func (m *Model) DeleteLogoDark() error {
	b, err := m.GetBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		ClearLogoDark().
		Exec(context.Background())
}

// DeleteLogoSmall removes the small logo.
func (m *Model) DeleteLogoSmall() error {
	b, err := m.GetBranding()
	if err != nil {
		return err
	}
	return m.Client.Branding.UpdateOneID(b.ID).
		ClearLogoSmall().
		Exec(context.Background())
}
