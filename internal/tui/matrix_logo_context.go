package tui

func (m *model) activeMatrixLogoID() string {
	if m == nil {
		return ""
	}
	if m.wizard.active && m.wizard.draft.AppID != "" {
		return m.wizard.draft.AppID
	}
	if m.modelCatalog.active && m.modelCatalog.providerSlug != "" {
		return m.modelCatalog.providerSlug
	}

	switch m.active {
	case screenProviders:
		if p := m.selectedProvider(); p != nil {
			return p.Slug
		}
	case screenProfiles, screenLaunch:
		if p := m.selectedProfile(); p != nil {
			if app := p.TargetApp(); app != "" && app != "generic" {
				return app
			}
			return p.ProviderSlug
		}
	case screenDashboard:
		if len(m.profiles.Profiles) > 0 {
			app := m.profiles.Profiles[0].TargetApp()
			if app != "" && app != "generic" {
				return app
			}
			return m.profiles.Profiles[0].ProviderSlug
		}
	}
	return ""
}
