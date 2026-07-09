package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/config"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
	"aegiskeys/internal/security"
)

// lockedView renders the master-password prompt shown until the vault unlocks.
func (m *model) lockedView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render(" AegisKeys — Vault Locked "))
	b.WriteString("\n\n")
	b.WriteString(s.Body.Render("Enter your master password to unlock the vault."))
	b.WriteString("\n\n")
	b.WriteString(m.passwordInput.View())
	b.WriteString("\n\n")
	if m.unlockError != "" {
		b.WriteString(s.Danger.Render("✗ " + m.unlockError))
		b.WriteString("\n\n")
	}
	b.WriteString(s.Muted.Render("Enter to unlock · Esc/Ctrl+C to quit"))
	return b.String()
}

// selected returns true if row i is the current selection for the screen.
func (m *model) rowSelected(i int) bool {
	return i == m.selected[m.active]
}

// selMarker returns the cursor glyph for a selected row.
func (m *model) selMarker(s *Styles, i int) string {
	if m.rowSelected(i) {
		return s.Value.Render("›")
	}
	return " "
}

// dashboardView shows vault status, quick actions, ready profiles, detected
// apps, and security status.
func (m *model) dashboardView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Dashboard"))
	b.WriteString("\n\n")

	// Vault status.
	b.WriteString(fmt.Sprintf("Vault:       %s\n", statusLine(s, m.unlocked, m.vaultExists)))
	b.WriteString(fmt.Sprintf("Providers:   %d\n", len(m.providers.Providers)))
	b.WriteString(fmt.Sprintf("Keys:        %d\n", len(m.keys)))
	b.WriteString(fmt.Sprintf("Profiles:    %d\n", len(m.profiles.Profiles)))

	// Quick actions.
	b.WriteString("\n")
	b.WriteString(s.SectionHeader.Render("Quick actions"))
	b.WriteString("\n")
	actions := []string{
		"Create launch profile",
		"Add API key",
		"Add provider/router",
		"Run doctor",
	}
	for i, a := range actions {
		marker := " "
		if i == m.selected[screenDashboard] && m.focus == focusContent {
			marker = "›"
			a = s.SelectedRow.Render(a)
		} else {
			a = s.Body.Render(a)
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", marker, a))
	}
	b.WriteString(s.Muted.Render("  press Enter to run selected"))

	// Ready profiles.
	if len(m.profiles.Profiles) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Ready profiles"))
		b.WriteString("\n")
		for _, p := range m.profiles.Profiles {
			status := s.Success.Render("ready")
			appID := p.TargetApp()
			if appID == "" {
				appID = "generic"
			}
			support := ""
			if a, ok := m.adapterRegistry.Get(appID); ok {
				c := a.Contract()
				support = string(c.SupportLevel)
			}
			name := truncate(p.Name, 18)
			b.WriteString(fmt.Sprintf("  %-20s %-12s %s\n", name, s.Muted.Render(appID), status))
			if support != "" {
				b.WriteString(fmt.Sprintf("    %s\n", s.Muted.Render(support)))
			}
		}
	}

	// Detected apps.
	b.WriteString("\n")
	b.WriteString(s.SectionHeader.Render("Detected apps"))
	b.WriteString("\n")
	detected := m.detectInstalledApps()
	if len(detected) == 0 {
		b.WriteString(s.Muted.Render("  (none — install a coding agent to auto-detect)"))
		b.WriteString("\n")
	} else {
		for _, d := range detected {
			icon := s.Muted.Render("○")
			if d.Installed {
				icon = s.Success.Render("●")
			}
			b.WriteString(fmt.Sprintf("  %s %-20s %s\n", icon, d.Name, s.Muted.Render(d.Command)))
		}
	}

	// Security summary.
	b.WriteString("\n")
	b.WriteString(s.SectionHeader.Render("Security"))
	b.WriteString("\n")
	securityIssues := 0
	if !m.unlocked {
		securityIssues++
	}
	b.WriteString(fmt.Sprintf("  %s vault %s\n", s.Success.Render("✓"), s.Muted.Render(map[bool]string{true: "unlocked", false: "locked"}[m.unlocked])))
	b.WriteString(fmt.Sprintf("  %d audit events\n", len(m.auditEvents)))
	b.WriteString(fmt.Sprintf("  %d security issues\n", securityIssues))

	// Recent activity.
	b.WriteString("\n")
	b.WriteString(s.SectionHeader.Render("Recent activity"))
	b.WriteString("\n")
	if len(m.auditEvents) == 0 {
		b.WriteString(s.Muted.Render("  no events recorded"))
		b.WriteString("\n")
	} else {
		for _, e := range m.auditEvents {
			b.WriteString(fmt.Sprintf("  %s  %s\n", e.Time.Format("2006-01-02 15:04"), e.Event))
		}
	}
	return b.String()
}

// providersView lists provider metadata as a navigable list.
func (m *model) providersView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Providers"))
	b.WriteString("\n\n")

	if len(m.providers.Providers) == 0 {
		b.WriteString(s.Muted.Render("No providers. Press `z` to add one."))
		return b.String()
	}

	b.WriteString(s.Muted.Render(fmtRow("NAME", "SLUG", "ENV VAR")))
	b.WriteString("\n")

	start, end := visibleWindow(len(m.providers.Providers), m.selected[screenProviders], m.screenListRows(1))
	for i := start; i < end; i++ {
		p := m.providers.Providers[i]
		marker := m.selMarker(s, i)
		catalogBadge := ""
		switch providerModelSource(p) {
		case provider.ModelSourceDynamic:
			catalogBadge = s.Muted.Render(" [dynamic]")
		case provider.ModelSourceStatic:
			catalogBadge = s.Muted.Render(" [static]")
		case provider.ModelSourceLocal:
			catalogBadge = s.Muted.Render(" [local]")
		case provider.ModelSourceManual:
			catalogBadge = s.Muted.Render(" [manual]")
		}
		if m.rowSelected(i) {
			b.WriteString(marker + " " + s.SelectedRow.Render(fmtRow(p.Name, p.Slug, p.CanonicalEnvVar())) + catalogBadge)
		} else {
			b.WriteString(marker + " " + s.Body.Render(truncate(p.Name, 22)) + " " +
				s.KeyMasked.Render(padRight(p.Slug, 18)) + " " +
				s.Muted.Render(truncate(p.CanonicalEnvVar(), 24)) + catalogBadge)
		}
		b.WriteString("\n")
	}
	if status := scrollStatus(start, end, len(m.providers.Providers)); status != "" {
		b.WriteString(s.Muted.Render(status + " · ↑/↓ scroll · PgUp/PgDn jump"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("e edit  d inspect  m model catalog  / filter  x delete  z add"))
	return b.String()
}

// keysView shows masked secrets only.
func (m *model) keysView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Keys (masked)"))
	b.WriteString("\n\n")
	if !m.unlocked {
		b.WriteString(s.Muted.Render("Vault locked."))
		return b.String()
	}
	if len(m.keys) == 0 {
		b.WriteString(s.Muted.Render("No keys. Press `z` to add one."))
		return b.String()
	}
	start, end := visibleWindow(len(m.keys), m.selected[screenKeys], m.screenListRows(1))
	for i := start; i < end; i++ {
		k := m.keys[i]
		marker := m.selMarker(s, i)
		rotBadge := ""
		if m.keyNeedsRotation(k) {
			rotBadge = " " + s.Warning.Render("⚠ rotate")
		}
		if m.rowSelected(i) {
			b.WriteString(fmt.Sprintf("%s %s%s\n", marker, s.SelectedRow.Render(fmt.Sprintf("%-20s %s  %s",
				truncate(k.Label, 20), k.MaskedSecret, truncate(k.ProviderSlug, 16))), rotBadge))
		} else {
			b.WriteString(fmt.Sprintf("%s %-20s %s  %s%s\n", marker,
				s.KeyLabel.Render(truncate(k.Label, 20)),
				s.KeyMasked.Render(k.MaskedSecret),
				s.Muted.Render(truncate(k.ProviderSlug, 16)), rotBadge))
		}
	}
	if status := scrollStatus(start, end, len(m.keys)); status != "" {
		b.WriteString("\n")
		b.WriteString(s.Muted.Render(status + " · ↑/↓ scroll · PgUp/PgDn jump"))
	}
	return b.String()
}

// keyNeedsRotation reports whether a key's age since its last rotation (or
// creation, if never rotated) exceeds the configured rotation reminder
// interval. Returns false when reminders are disabled (0 days).
func (m *model) keyNeedsRotation(k secret.MaskedKeyItem) bool {
	if m.cfg.RotationReminderDays <= 0 {
		return false
	}
	var t time.Time
	if k.LastRotated != "" {
		if parsed, err := time.Parse("2006-01-02", k.LastRotated); err == nil {
			t = parsed
		}
	}
	if t.IsZero() && k.CreatedAt != "" {
		if parsed, err := time.Parse("2006-01-02", k.CreatedAt); err == nil {
			t = parsed
		}
	}
	if t.IsZero() {
		return false
	}
	return time.Since(t) > time.Duration(m.cfg.RotationReminderDays)*24*time.Hour
}

// profilesView lists profiles with their provider and masked key.
func (m *model) profilesView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Profiles"))
	b.WriteString("\n\n")
	if len(m.profiles.Profiles) == 0 {
		b.WriteString(s.Muted.Render("No profiles. Press `z` to create one."))
		return b.String()
	}
	start, end := visibleWindow(len(m.profiles.Profiles), m.selected[screenProfiles], m.screenListRows(2))
	for i := start; i < end; i++ {
		p := m.profiles.Profiles[i]
		marker := m.selMarker(s, i)
		provName := p.ProviderSlug
		if pr := m.providers.Find(p.ProviderSlug); pr != nil {
			provName = pr.Name
		}
		if m.rowSelected(i) {
			b.WriteString(fmt.Sprintf("%s %s\n", marker, s.SelectedRow.Render(truncate(p.Name, 24))))
			b.WriteString(fmt.Sprintf("     %s  key: %s\n", s.Muted.Render(truncate(provName, 18)), s.KeyMasked.Render(p.KeyID)))
		} else {
			b.WriteString(fmt.Sprintf("%s %s\n", marker, s.KeyLabel.Render(truncate(p.Name, 24))))
			b.WriteString(fmt.Sprintf("     %s  key: %s\n", s.Muted.Render(truncate(provName, 18)), s.KeyMasked.Render(p.KeyID)))
		}
	}
	if status := scrollStatus(start, end, len(m.profiles.Profiles)); status != "" {
		b.WriteString("\n")
		b.WriteString(s.Muted.Render(status + " · ↑/↓ scroll · PgUp/PgDn jump"))
	}
	return b.String()
}

// launchView shows and executes the safe credential-launch bridge using the
// adapter system.
func (m *model) launchView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Launch"))
	b.WriteString("\n\n")
	if !m.unlocked {
		b.WriteString(s.Muted.Render("Unlock the vault to preview a launch."))
		return b.String()
	}
	if len(m.profiles.Profiles) == 0 {
		b.WriteString(s.Muted.Render("No profiles. Create one with `aegiskeys profile create`."))
		return b.String()
	}

	// Profile selection.
	b.WriteString(s.SectionHeader.Render("Profile"))
	b.WriteString("\n")
	maxLaunchRows := m.screenListRows(1)
	if maxLaunchRows > 8 {
		maxLaunchRows = 8
	}
	start, end := visibleWindow(len(m.profiles.Profiles), m.selected[screenLaunch], maxLaunchRows)
	for i := start; i < end; i++ {
		p := m.profiles.Profiles[i]
		marker := " "
		style := s.KeyLabel
		if i == m.selected[screenLaunch] && m.launchMode == launchSelectProfile {
			marker = "›"
			pstyle := s.SelectedRow
			style = pstyle
		}
		modelInfo := ""
		if p.ModelID() != "" {
			modelInfo = " · " + truncate(p.ModelID(), 18)
		}
		appInfo := ""
		if p.TargetApp() != "generic" {
			appInfo = " [" + p.TargetApp() + "]"
		}
		b.WriteString(fmt.Sprintf("%s %s%s%s\n", marker, style.Render(truncate(p.Name, 18)), s.Muted.Render(modelInfo), s.Muted.Render(appInfo)))
	}
	if status := scrollStatus(start, end, len(m.profiles.Profiles)); status != "" {
		b.WriteString(s.Muted.Render(status + " · ↑/↓ scroll · PgUp/PgDn jump"))
		b.WriteString("\n")
	}

	// Clamp the selected index: a profile delete can leave selected pointing
	// past the end of the list, which would panic on the next render.
	idx := m.selected[screenLaunch]
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.profiles.Profiles) {
		idx = len(m.profiles.Profiles) - 1
		m.selected[screenLaunch] = idx
	}
	prof := m.profiles.Profiles[idx]
	prov := m.providers.Find(prof.ProviderSlug)

	// Resolve the exact same LaunchStrategy that CLI run would execute.
	var strategy *adapter.LaunchStrategy
	var resolveErr error
	if prov != nil {
		var key *secret.SecretRecord
		if m.vaultSession != nil && m.vaultSession.vault != nil {
			key = m.vaultSession.vault.Get(prof.KeyID)
		}
		// Show hazards/manual steps for blocked adapters instead of erroring,
		// but never expose raw secrets.
		strategy, resolveErr = adapter.ResolveLaunchStrategyCatalog(prof, *prov, key, m.adapterRegistry, m.providers, m.vaultSession.vault, adapter.ResolvePreview)
	}

	// Show resolve errors as blocking red panels.
	if resolveErr != nil {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Cannot launch"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Danger.Render("✗"), s.Warning.Render(resolveErr.Error())))
		b.WriteString("\n")
		b.WriteString(s.Muted.Render("Fix the profile before launching."))
		return b.String()
	}

	if strategy == nil {
		b.WriteString("\n")
		b.WriteString(s.Muted.Render("No strategy resolved."))
		return b.String()
	}

	// Command + args from the resolved strategy.
	b.WriteString("\n")
	b.WriteString(s.SectionHeader.Render("Command"))
	b.WriteString("\n")
	if strategy.Blocked {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Danger.Render("✗"), s.Danger.Render("Launch blocked: "+strategy.BlockReason)))
	} else if strategy.Plan.Command == "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Warning.Render("⚠"), s.Muted.Render("No command produced by adapter.")))
	} else {
		b.WriteString(s.Value.Render("  " + strategy.Plan.Command))
		if len(strategy.Plan.Args) > 0 {
			b.WriteString(s.Muted.Render(" " + truncate(strings.Join(strategy.Plan.Args, " "), 50)))
		}
		b.WriteString("\n")
	}

	// Env preview (masked). Keys sorted for stable display across re-renders.
	if len(strategy.Plan.Env) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Will inject (masked)"))
		b.WriteString("\n")
		envKeys := make([]string, 0, len(strategy.Plan.Env))
		for k := range strategy.Plan.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		for _, k := range envKeys {
			v := strategy.Plan.Env[k]
			if looksSecretEnvValue(k) {
				b.WriteString(fmt.Sprintf("  %s=%s\n", s.KeyMasked.Render(k), s.Muted.Render("<secret>")))
			} else {
				b.WriteString(fmt.Sprintf("  %s=%s\n", s.KeyMasked.Render(k), s.Body.Render(v)))
			}
		}
	}

	// Config files with merge/backup policy.
	if len(strategy.Plan.Files) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Config files"))
		b.WriteString("\n")
		for _, f := range strategy.Plan.Files {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("→"), s.Muted.Render(f.Path)))
			b.WriteString(fmt.Sprintf("    %s merge=%s backup=%s scope=%s\n", s.Muted.Render("policy:"), s.Muted.Render(string(f.MergePolicy)), s.Muted.Render(string(f.BackupPolicy)), s.Muted.Render(string(f.Scope))))
		}
	}

	// Manual steps.
	if len(strategy.ManualSteps) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Manual steps"))
		b.WriteString("\n")
		for i, step := range strategy.ManualSteps {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, s.Warning.Render(step.Title)))
			if step.Description != "" {
				b.WriteString(fmt.Sprintf("     %s\n", s.Muted.Render(step.Description)))
			}
		}
	}

	// Hazards.
	if len(strategy.Hazards) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Warnings"))
		b.WriteString("\n")
		for _, h := range strategy.Hazards {
			icon := s.Warning.Render("⚠")
			if h.Severity == "critical" {
				icon = s.Danger.Render("✗")
			}
			b.WriteString(fmt.Sprintf("  %s %s\n", icon, s.Warning.Render(h.Title)))
			if h.Fix != "" {
				b.WriteString(fmt.Sprintf("    %s %s\n", s.Muted.Render("fix:"), s.Muted.Render(h.Fix)))
			}
		}
	}

	// Warnings from the resolver.
	if len(strategy.Plan.Warnings) > 0 {
		b.WriteString("\n")
		b.WriteString(s.SectionHeader.Render("Resolver warnings"))
		b.WriteString("\n")
		for _, w := range strategy.Plan.Warnings {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Warning.Render("!"), s.Muted.Render(w)))
		}
	}

	// Footer: launch action with CLI fallback.
	b.WriteString("\n")
	if m.launchMode == launchTypeCommand {
		b.WriteString(s.Muted.Render("Type command and press Enter. Leave blank to use adapter default.\n"))
		b.WriteString("  " + m.commandInput.View() + "\n")
	} else {
		b.WriteString(s.Muted.Render("Enter launches default. Right/d sets command override. CLI fallback:\n"))
		b.WriteString(s.Muted.Render(fmt.Sprintf("  aegiskeys run --profile %s -- <command>\n", prof.Name)))
	}

	return b.String()
}

// looksSecretEnvValue reports whether an env var name looks like it carries
// a secret, so the TUI can mask its value in previews.
func looksSecretEnvValue(k string) bool {
	upper := strings.ToUpper(k)
	for _, pat := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "AUTH"} {
		if strings.Contains(upper, pat) {
			return true
		}
	}
	return false
}

// modelEnvVar returns the conventional model env var name for a provider.
func modelEnvVar(p *provider.Provider) string {
	if p == nil {
		return "OPENAI_MODEL"
	}
	switch p.Compatibility {
	case provider.CompatAnthropic:
		return "ANTHROPIC_MODEL"
	case provider.CompatGoogle:
		return "GOOGLE_MODEL"
	default:
		return "OPENAI_MODEL"
	}
}

// doctorView shows security diagnostic results.
func (m *model) doctorView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Security Doctor"))
	b.WriteString("\n\n")
	if !m.doctorRan {
		b.WriteString(s.Muted.Render("Press r to run diagnostics."))
		return b.String()
	}
	for _, r := range m.doctorResults {
		b.WriteString(formatDoctorResult(s, r))
		b.WriteString("\n")
	}
	return b.String()
}

func formatDoctorResult(s *Styles, r security.CheckResult) string {
	var mark, msg string
	switch r.Severity {
	case security.SeverityOK:
		mark = s.Success.Render("OK")
		msg = s.Body.Render(r.Message)
	case security.SeverityWarn:
		mark = s.Warning.Render("WARN")
		msg = s.Warning.Render(r.Message)
	case security.SeverityFail:
		mark = s.Danger.Render("FAIL")
		msg = s.Danger.Render(r.Message)
	}
	out := fmt.Sprintf("[%s] %s", mark, msg)
	if r.Fix != "" {
		out += "\n    " + s.Muted.Render("fix: "+r.Fix)
	}
	return out
}

// auditView shows recent audit events (metadata only).
func (m *model) auditView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Audit Log"))
	b.WriteString("\n\n")
	if len(m.auditEvents) == 0 {
		b.WriteString(s.Muted.Render("No audit events recorded yet."))
		return b.String()
	}
	for _, e := range m.auditEvents {
		line := e.Time.Format("2006-01-02 15:04:05") + "  " + e.Event
		if e.Profile != "" {
			line += "  profile=" + e.Profile
		}
		if e.Provider != "" {
			line += "  provider=" + e.Provider
		}
		if e.Command != "" {
			line += "  command=" + e.Command
		}
		b.WriteString(s.Body.Render(line))
		b.WriteString("\n")
	}
	return b.String()
}

// settingsView shows the current on-disk settings.
func (m *model) settingsView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Settings"))
	b.WriteString("\n\n")

	cfg, err := config.LoadConfig(config.ConfigPath(m.configDir))
	if err != nil {
		cfg = config.DefaultConfig()
	}
	theme := m.themeName
	if theme == "" {
		theme = "vault"
	}
	autoLock := "disabled"
	if cfg.AutoLock > 0 {
		autoLock = fmt.Sprintf("%d min", cfg.AutoLock)
	}

	defaultProfile := cfg.DefaultProfile
	if defaultProfile == "" {
		defaultProfile = "(none)"
	}
	rotation := "disabled"
	if cfg.RotationReminderDays > 0 {
		rotation = fmt.Sprintf("%dd", cfg.RotationReminderDays)
	}
	values := []string{
		autoLock + "  (←/→)",
		theme + "  (←/→)",
		defaultProfile + "  (←/→)",
		fmt.Sprintf("%ds  (←/→)", cfg.ClipboardTTLSeconds),
		fmt.Sprintf("%ds  (←/→)", cfg.AdapterVerifyTimeoutSeconds),
		fmt.Sprintf("%t  (←/→)", cfg.EnableAnimations),
		fmt.Sprintf("%t  (←/→)", cfg.EnableRiskyExport),
		rotation + "  (←/→)",
		cfg.RuntimePolicy + "  (←/→)",
		strings.TrimPrefix(config.ConfigPath(m.configDir), m.configDir+"/"),
	}
	for i, key := range settingKeys {
		marker := m.selMarker(s, i)
		val := ""
		if i < len(values) {
			val = values[i]
		}
		if m.rowSelected(i) {
			b.WriteString(fmt.Sprintf("%s %s  %s\n", marker, s.SelectedRow.Render(key), s.Muted.Render(val)))
		} else {
			b.WriteString(fmt.Sprintf("%s %s  %s\n", marker, s.KeyLabel.Render(key), s.Muted.Render(val)))
		}
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("Use ←/→ or Enter to adjust. CLI: aegiskeys settings set <key> <value>."))
	b.WriteString("\n")
	return b.String()
}

// helpView lists only the working keybindings.
func (m *model) helpView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Help"))
	b.WriteString("\n\n")
	b.WriteString(s.SectionHeader.Render("Keys"))
	b.WriteString("\n")
	for _, row := range [][2]string{
		{"w/a/s/d", "navigate / move / open"},
		{"↑/↓ or j/k", "move selection (alt)"},
		{"←/→ or h/l", "move pane (alt)"},
		{"enter", "select / open"},
		{"z", "new / add"},
		{"e", "edit selected"},
		{"x", "delete (confirm)"},
		{"/", "filter"},
		{"r", "refresh (doctor)"},
		{fmt.Sprintf("1-%d", screenCount()), "jump to screen"},
		{"tab", "focus sidebar/content"},
		{"[ / ]", "previous / next screen"},
		{"?", "toggle help"},
		{"esc", "back / cancel"},
		{"q / ctrl+c", "quit"},
	} {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", s.KeyMasked.Render(row[0]), s.Body.Render(row[1])))
	}
	return b.String()
}

// fmtRow builds a fixed-format provider row that cannot overflow the panel.
func fmtRow(name, slug, envVar string) string {
	return fmt.Sprintf("%-22s %-18s %s", truncate(name, 22), truncate(slug, 18), truncate(envVar, 24))
}

// statusLine returns a colored vault-status string.
func statusLine(s *Styles, unlocked, vaultExists bool) string {
	if !vaultExists {
		return s.Warning.Render("! no vault")
	}
	if unlocked {
		return s.Success.Render("✓ unlocked")
	}
	return s.Danger.Render("✗ locked")
}

// renderDetailModal shows the selected item's details.
func (m *model) renderDetailModal() string {
	var b strings.Builder
	s := m.styles

	switch m.active {
	case screenProviders:
		if m.selected[screenProviders] < len(m.providers.Providers) {
			p := m.providers.Providers[m.selected[screenProviders]]
			b.WriteString(s.KeyLabel.Render("Name:    ") + s.Body.Render(p.Name) + "\n")
			b.WriteString(s.KeyLabel.Render("Slug:    ") + s.Body.Render(p.Slug) + "\n")
			b.WriteString(s.KeyLabel.Render("Env Var: ") + s.Body.Render(p.CanonicalEnvVar()) + "\n")
			if p.CanonicalBaseURL() != "" {
				b.WriteString(s.KeyLabel.Render("Base URL: ") + s.Body.Render(p.CanonicalBaseURL()) + "\n")
			}
		}
	case screenKeys:
		if m.selected[screenKeys] < len(m.keys) {
			k := m.keys[m.selected[screenKeys]]
			b.WriteString(s.KeyLabel.Render("Label:   ") + s.Body.Render(k.Label) + "\n")
			b.WriteString(s.KeyLabel.Render("ID:      ") + s.Muted.Render(k.ID) + "\n")
			b.WriteString(s.KeyLabel.Render("Secret:  ") + s.KeyMasked.Render(k.MaskedSecret) + "\n")
			b.WriteString(s.KeyLabel.Render("Provider: ") + s.Body.Render(k.ProviderSlug) + "\n")
		}
	case screenProfiles:
		if m.selected[screenProfiles] < len(m.profiles.Profiles) {
			p := m.profiles.Profiles[m.selected[screenProfiles]]
			b.WriteString(s.KeyLabel.Render("Name:    ") + s.Body.Render(p.Name) + "\n")
			b.WriteString(s.KeyLabel.Render("Provider: ") + s.Body.Render(p.ProviderSlug) + "\n")
			b.WriteString(s.KeyLabel.Render("Key ID:  ") + s.Muted.Render(p.KeyID) + "\n")
		}
	case screenAudit:
		if m.selected[screenAudit] < len(m.auditEvents) {
			e := m.auditEvents[m.selected[screenAudit]]
			b.WriteString(s.KeyLabel.Render("Time:    ") + s.Body.Render(e.Time.Format("2006-01-02 15:04:05")) + "\n")
			b.WriteString(s.KeyLabel.Render("Event:   ") + s.Body.Render(e.Event) + "\n")
			if e.Profile != "" {
				b.WriteString(s.KeyLabel.Render("Profile: ") + s.Body.Render(e.Profile) + "\n")
			}
			if e.Provider != "" {
				b.WriteString(s.KeyLabel.Render("Provider: ") + s.Body.Render(e.Provider) + "\n")
			}
			if e.Command != "" {
				b.WriteString(s.KeyLabel.Render("Command: ") + s.Body.Render(e.Command) + "\n")
			}
		}
	}
	return b.String()
}

// scratchView renders the encrypted scratchpad screen with a navigable list
// and an editor panel. Uses Charm's textarea for multi-line editing.
func (m *model) scratchView(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.Title.Render("Scratchpads (encrypted)"))
	b.WriteString("\n\n")

	scratchpads := m.visibleScratchPads()
	if len(scratchpads) == 0 {
		b.WriteString(s.Muted.Render("No scratchpads. Press `n` to create one."))
		return b.String()
	}

	// Left: list panel.
	b.WriteString(s.SectionHeader.Render("Pages"))
	b.WriteString("\n")
	for i, sp := range scratchpads {
		marker := m.selMarker(s, i)
		title := sp.Title
		if title == "" {
			title = "(untitled)"
		}
		if sp.Archived {
			title += " " + s.Warning.Render("[archived]")
		}
		if m.scratchListSelected == i && !m.scratchEditing {
			b.WriteString(fmt.Sprintf("  %s %s\n", marker, s.SelectedRow.Render(truncate(title, 28))))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", marker, s.Body.Render(truncate(title, 28))))
		}
	}

	// Right: preview/editor panel.
	sp := m.selectedScratchPad()
	b.WriteString("\n")
	if sp != nil {
		b.WriteString(s.SectionHeader.Render(sp.Title))
		if sp.ProviderSlug != "" {
			b.WriteString(s.Muted.Render("  " + sp.ProviderSlug))
		}
		b.WriteString("\n")

		if m.scratchEditing {
			b.WriteString(m.scratchTitleInput.View())
			b.WriteString("\n")
			b.WriteString(m.scratchBodyInput.View())
			if m.scratchDirty {
				b.WriteString("\n" + s.Warning.Render("[modified]"))
			}
		} else {
			body := sp.Body
			if body == "" {
				body = s.Muted.Render("(empty)")
			} else {
				body = m.renderScratchBodyPreview(s, body)
			}
			b.WriteString(body)
		}
	}

	// Footer: contextual controls.
	b.WriteString("\n\n")
	if m.scratchEditing {
		b.WriteString(s.Muted.Render("ctrl+s save  ctrl+e external editor  esc cancel"))
	} else if m.scratchSelecting {
		b.WriteString(s.Muted.Render("v clear selection  j/k extend  y copy selected  c copy selected  esc cancel"))
	} else {
		b.WriteString(s.Muted.Render("n new  e/enter edit  v select lines  y/c copy  / filter  x delete  q/Esc back · vault-encrypted"))
	}
	return b.String()
}

func (m *model) renderScratchBodyPreview(s *Styles, body string) string {
	lines := scratchBodyLines(body)
	if len(lines) == 0 {
		return s.Muted.Render("(empty)")
	}
	m.scratchBodyCursor = clampInt(m.scratchBodyCursor, 0, len(lines)-1)
	start, end, hasSelection := m.scratchSelectionRange(len(lines))

	var b strings.Builder
	for i, line := range lines {
		marker := "  "
		if i == m.scratchBodyCursor {
			marker = "› "
		}
		text := marker + line
		if hasSelection && i >= start && i <= end {
			b.WriteString(s.SelectedRow.Render(text))
		} else if i == m.scratchBodyCursor {
			b.WriteString(s.KeyLabel.Render(text))
		} else {
			b.WriteString(s.Body.Render(text))
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// visibleScratchPads returns the scratchpad list, applying the archive filter.
func (m *model) visibleScratchPads() []secret.ScratchPadRecord {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		return nil
	}
	all := m.vaultSession.vault.ScratchPads
	out := make([]secret.ScratchPadRecord, 0, len(all))
	for _, sp := range all {
		if sp.Archived {
			continue
		}
		out = append(out, sp)
	}
	return out
}

// selectedScratchPad returns the scratchpad under the list cursor, or nil.
func (m *model) selectedScratchPad() *secret.ScratchPadRecord {
	sp := m.visibleScratchPads()
	idx := m.scratchListSelected
	if idx < 0 || idx >= len(sp) {
		return nil
	}
	return &sp[idx]
}

// renderDeleteModal shows the delete confirmation. When a provider has
// referencing keys, it asks for confirmation to cascade-delete (yes/n).
// When a deletion is blocked for another reason, it stays open and shows why.
func (m *model) renderDeleteModal() string {
	if m.deleteConfirmWait {
		return m.deleteBlockReason + "\n\n[y] yes  [n] no"
	}
	if m.deleteBlockReason != "" {
		return "Cannot delete:\n\n" + m.deleteBlockReason + "\n\nEsc to close"
	}
	return "Are you sure? This cannot be undone."
}
