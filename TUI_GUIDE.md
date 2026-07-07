# AegisKeys TUI Implementation Guide

> Comprehensive guide for building the AegisKeys interactive terminal UI using the Charmbracelet v2 stack.

---

## Table of Contents

1. [Stack Overview](#1-stack-overview)
2. [Project Structure](#2-project-structure)
3. [Core Architecture](#3-core-architecture)
4. [Screen-by-Screen Implementation](#4-screen-by-screen-implementation)
5. [Styles & Theming](#5-styles--theming)
6. [Component Patterns](#6-component-patterns)
7. [Security-Critical Patterns](#7-security-critical-patterns)
8. [Charm Apps to Study](#8-charm-apps-to-study)
9. [Pitfalls to Avoid](#9-pitfalls-to-avoid)
10. [Implementation Checklist](#10-implementation-checklist)

---

## 1. Stack Overview

AegisKeys uses the **Charmbracelet v2 stack** exclusively:

| Component | Purpose | Import Path |
|-----------|---------|-------------|
| **Bubble Tea v2** | App framework, event loop, program lifecycle | `charm.land/bubbletea/v2` |
| **Bubbles v2** | Reusable components (list, viewport, textinput, spinner, help, table) | `charm.land/bubbles/v2` |
| **Lip Gloss v2** | Styling, layout, borders, colors, responsive sizing | `charm.land/lipgloss/v2` |
| **Huh** | Form wizard flows (CLI init, add provider, add key, create profile) | `charm.land/huh/v2` |
| **Glamour** | Markdown rendering for help, threat model, provider docs | `charm.land/glamour/v2` |
| **Cobra** | CLI command tree | `github.com/spf13/cobra` |
| **Fang** | Optional Cobra polish layer | `charm.land/fang/v2` |
| **VHS** | Reproducible terminal demos (`.tape` files) | `charm.land/vhs/v2` |
| **Freeze** | Terminal screenshots for README | `charm.land/freeze/v2` |
| **Wish** | Future: SSH remote TUI (NOT for MVP) | `charm.land/wish/v2` |

> ⚠️ **Critical**: Use `charm.land/.../v2` imports. Do NOT mix with old `github.com/charmbracelet/...` v1 paths.

---

## 2. Project Structure

```
internal/tui/
├── model.go          # Root model, routing, global state
├── update.go         # Global message handling
├── view.go           # Global layout shell
├── styles.go         # Central theme/style definitions
├── keys.go           # Key bindings & help
├── dashboard.go      # Dashboard screen
├── providers.go      # Providers screen
├── keys.go           # Keys screen (rename to keys_screen.go to avoid conflict)
├── profiles.go       # Profiles screen
├── launch.go         # Launch screen
├── doctor.go         # Security Doctor screen
├── audit.go          # Audit log screen
├── help.go           # Help screen
└── settings.go       # Settings screen
```

### Go Module Imports

```go
module aegiskeys

go 1.23

require (
    charm.land/bubbletea/v2 v2.x.x
    charm.land/bubbles/v2 v2.x.x
    charm.land/lipgloss/v2 v2.x.x
    charm.land/huh/v2 v2.x.x
    charm.land/glamour/v2 v2.x.x
    charm.land/fang/v2 v2.x.x
    github.com/spf13/cobra v1.x.x
    github.com/alecthomas/chroma/v2 v2.x.x  // for syntax highlighting in help
)
```

---

## 3. Core Architecture

### 3.1 Root Model

```go
// internal/tui/model.go
package tui

import (
    "time"
    tea "charm.land/bubbletea/v2"
    "charm.land/lipgloss/v2"
    "aegiskeys/internal/provider"
    "aegiskeys/internal/profile"
    "aegiskeys/internal/secret"
    "aegiskeys/internal/security"
)

type Screen int

const (
    ScreenDashboard Screen = iota
    ScreenProviders
    ScreenKeys
    ScreenProfiles
    ScreenLaunch
    ScreenDoctor
    ScreenAudit
    ScreenSettings
    ScreenHelp
)

type Model struct {
    // Dimensions
    width  int
    height int

    // Routing
    active Screen
    prev   Screen

    // Global state
    vaultUnlocked bool
    lastError     error
    statusMsg     string
    statusUntil   time.Time

    // Data (populated from services)
    providers  []provider.Provider
    keys       []secret.MaskedKeyItem
    profiles   []profile.Profile
    doctorResults []security.CheckResult
    auditEvents []security.AuditEvent

    // Child models
    dashboard dashboardModel
    providers providersModel
    keys      keysModel
    profiles  profilesModel
    launch    launchModel
    doctor    doctorModel
    audit     auditModel
    settings  settingsModel
    help      helpModel

    // Styles
    styles *Styles
}

func NewModel(providers []provider.Provider, keys []secret.MaskedKeyItem, profiles []profile.Profile) Model {
    m := Model{
        providers: providers,
        keys:      keys,
        profiles:  profiles,
        styles:    NewStyles(true), // dark default
        active:    ScreenDashboard,
    }
    m.initChildren()
    return m
}

func (m *Model) initChildren() {
    m.dashboard = newDashboardModel(m.styles)
    m.providers = newProvidersModel(m.styles, m.providers)
    m.keys = newKeysModel(m.styles, m.keys)
    m.profiles = newProfilesModel(m.styles, m.profiles, m.providers, m.keys)
    m.launch = newLaunchModel(m.styles, m.profiles)
    m.doctor = newDoctorModel(m.styles)
    m.audit = newAuditModel(m.styles)
    m.settings = newSettingsModel(m.styles)
    m.help = newHelpModel(m.styles)
}
```

### 3.2 Global Update Loop

```go
// internal/tui/update.go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Global keys handled at root level
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m.resizeChildren()
        return m, nil

    case tea.KeyPressMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            if m.active != ScreenDashboard {
                m.active = ScreenDashboard
                return m, nil
            }
            return m, tea.Quit

        case "ctrl+l":
            return m.lockVault()

        case "tab":
            m.nextScreen()
            return m, nil

        case "shift+tab":
            m.prevScreen()
            return m, nil

        case "?":
            m.toggleHelp()
            return m, nil

        case "1", "2", "3", "4", "5", "6", "7", "8":
            m.jumpToScreen(msg.String())
            return m, nil
        }

    case vaultLockedMsg:
        m.vaultUnlocked = false
        m.keys = nil
        m.statusMsg = "Vault locked"
        m.statusUntil = time.Now().Add(2 * time.Second)
        return m, nil

    case statusMsg:
        m.statusMsg = string(msg)
        m.statusUntil = time.Now().Add(3 * time.Second)
        return m, nil
    }

    // Delegate to active screen
    return m.updateActiveScreen(msg)
}

func (m Model) updateActiveScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch m.active {
    case ScreenDashboard:
        var cmd tea.Cmd
        m.dashboard, cmd = m.dashboard.Update(msg)
        return m, cmd

    case ScreenProviders:
        var cmd tea.Cmd
        m.providers, cmd = m.providers.Update(msg)
        return m, cmd

    case ScreenKeys:
        var cmd tea.Cmd
        m.keys, cmd = m.keys.Update(msg)
        return m, cmd

    case ScreenProfiles:
        var cmd tea.Cmd
        m.profiles, cmd = m.profiles.Update(msg)
        return m, cmd

    case ScreenLaunch:
        var cmd tea.Cmd
        m.launch, cmd = m.launch.Update(msg)
        return m, cmd

    case ScreenDoctor:
        var cmd tea.Cmd
        m.doctor, cmd = m.doctor.Update(msg)
        return m, cmd

    case ScreenAudit:
        var cmd tea.Cmd
        m.audit, cmd = m.audit.Update(msg)
        return m, cmd

    case ScreenSettings:
        var cmd tea.Cmd
        m.settings, cmd = m.settings.Update(msg)
        return m, cmd

    case ScreenHelp:
        var cmd tea.Cmd
        m.help, cmd = m.help.Update(msg)
        return m, cmd
    }
    return m, nil
}
```

### 3.3 Global View Layout

```go
// internal/tui/view.go
func (m Model) View() string {
    if m.styles == nil {
        return "Initializing..."
    }

    // Build sidebar
    sidebar := m.renderSidebar()

    // Build content area
    content := m.renderContent()

    // Build footer
    footer := m.renderFooter()

    // Join horizontally: sidebar | content
    main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, content)

    // Join vertically: main + footer
    full := lipgloss.JoinVertical(lipgloss.Left, main, footer)

    return m.styles.AppFrame.Render(full)
}

func (m Model) renderSidebar() string {
    items := []struct {
        key   string
        label string
        screen Screen
    }{
        {"1", "Dashboard", ScreenDashboard},
        {"2", "Providers", ScreenProviders},
        {"3", "Keys", ScreenKeys},
        {"4", "Profiles", ScreenProfiles},
        {"5", "Launch", ScreenLaunch},
        {"6", "Doctor", ScreenDoctor},
        {"7", "Audit", ScreenAudit},
        {"8", "Settings", ScreenSettings},
    }

    var sb strings.Builder
    sb.WriteString(m.styles.Header.Render("AegisKeys"))
    sb.WriteString("\n")

    for _, item := range items {
        style := m.styles.SidebarItem
        if item.screen == m.active {
            style = m.styles.SidebarItemActive
        }
        sb.WriteString(style.Render(fmt.Sprintf("  %s  %s", item.key, item.label)))
        sb.WriteString("\n")
    }

    // Vault status
    vaultStatus := "🔒Locked"
    if m.vaultUnlocked {
        vaultStatus = "🔓Unlocked"
    }
    sb.WriteString("\n")
    sb.WriteString(m.styles.SidebarFooter.Render(fmt.Sprintf("Vault: %s", vaultStatus)))

    return m.styles.Sidebar.Render(sb.String())
}

func (m Model) renderContent() string {
    switch m.active {
    case ScreenDashboard:
        return m.dashboard.View(m.width - sidebarWidth - 4, m.height - headerHeight - footerHeight)
    case ScreenProviders:
        return m.providers.View(m.width - sidebarWidth - 4, m.height - headerHeight - footerHeight)
    // ... other screens
    }
    return ""
}
```

### 3.4 Screen Interface

Each screen model implements:

```go
type ScreenModel interface {
    Init() tea.Cmd
    Update(tea.Msg) (ScreenModel, tea.Cmd)
    View(width, height int) string
    SetSize(width, height int)
    SetData(data interface{}) // For passing providers, keys, etc.
}
```

---

## 4. Screen-by-Screen Implementation

### 4.1 Dashboard Screen

**Purpose**: Overview, quick actions, security status.

```go
// internal/tui/dashboard.go
type dashboardModel struct {
    styles *Styles
    width  int
    height int
}

func newDashboardModel(styles *Styles) dashboardModel {
    return dashboardModel{styles: styles}
}

func (m dashboardModel) Init() tea.Cmd {
    return nil
}

func (m *dashboardModel) SetSize(w, h int) {
    m.width = w
    m.height = h
}

func (m dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
    return m, nil // Dashboard is mostly static, updates via root model data
}

func (m dashboardModel) View(width, height int) string {
    // Build dashboard content using Lip Gloss
    // Show: Vault status, counts, warnings, quick actions
}
```

**Key UI Elements**:
- Vault status badge (locked/unlocked)
- Provider/Key/Profile counts
- Doctor status (OK/WARN/FAIL)
- Recent profile usage
- Quick action list (navigable with j/k, Enter to activate)

---

### 4.2 Providers Screen

**Purpose**: List, search, add, edit, inspect providers.

**Component**: `bubbles/list` with custom delegate.

```go
// internal/tui/providers.go
import (
    "charm.land/bubbles/v2/list"
    "charm.land/lipgloss/v2"
)

type providerItem struct {
    provider.Provider
    hasKey bool
}

func (i providerItem) FilterValue() string {
    return i.Name + " " + i.Slug + " " + strings.Join(i.Tags, " ")
}

type providerDelegate struct {
    styles *Styles
}

func (d providerDelegate) Height() int  { return 2 }
func (d providerDelegate) Spacing() int { return 1 }

func (d providerDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
    return nil
}

func (d providerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
    p, ok := item.(providerItem)
    if !ok { return }

    var sb strings.Builder
    // Name with badges
    name := d.styles.ProviderName.Render(p.Name)
    sb.WriteString(name)

    // Tags as badges
    for _, tag := range p.Tags {
        badge := d.styles.Badge.Render(tag)
        sb.WriteString(" " + badge)
    }
    sb.WriteString("\n")

    // Slug + key status
    keyStatus := "● configured"
    keyStyle := d.styles.Success
    if !p.hasKey {
        keyStatus = "○ no key"
        keyStyle = d.styles.Warning
    }
    sb.WriteString(d.styles.ProviderMeta.Render(fmt.Sprintf("  %s  %s", p.Slug, keyStyle.Render(keyStatus))))

    // Highlight selection
    if index == m.Index() {
        fmt.Fprint(w, d.styles.SelectedItem.Render(sb.String()))
    } else {
        fmt.Fprint(w, sb.String())
    }
}

type providersModel struct {
    list   list.Model
    styles *Styles
    width  int
    height int
}

func newProvidersModel(styles *Styles, providers []provider.Provider) providersModel {
    items := make([]list.Item, len(providers))
    for i, p := range providers {
        items[i] = providerItem{Provider: p, hasKey: checkKeyExists(p.Slug)}
    }

    delegate := providerDelegate{styles: styles}
    l := list.New(items, delegate, 0, 0)
    l.Title = "Providers"
    l.SetShowStatusBar(true)
    l.SetFilteringEnabled(true)
    l.Styles.Title = styles.ListTitle
    l.Styles.PaginationStyle = styles.Pagination
    l.Styles.HelpStyle = styles.Help

    return providersModel{list: l, styles: styles}
}

func (m *providersModel) SetSize(w, h int) {
    m.width = w
    m.height = h
    m.list.SetSize(w, h-4) // Account for title, borders
}
```

**Keys**:
- `/` - filter
- `a` - add provider (opens Huh form)
- `e` - edit selected
- `Enter` - view details
- `d` - delete (with confirmation)

---

### 4.3 Keys Screen

**Purpose**: List keys by provider, add, rotate, delete. **Never show raw secrets.**

```go
// internal/tui/keys.go
type maskedKeyItem struct {
    ID           string
    ProviderSlug string
    Label        string
    MaskedSecret string
    LastUsed     string
    Tags         []string
}

type keysModel struct {
    list   list.Model
    styles *Styles
    width  int
    height int
    byProvider map[string][]maskedKeyItem
}

func newKeysModel(styles *Styles, keys []secret.MaskedKeyItem) keysModel {
    // Group by provider
    byProvider := make(map[string][]maskedKeyItem)
    for _, k := range keys {
        byProvider[k.ProviderSlug] = append(byProvider[k.ProviderSlug], maskedKeyItem{
            ID: k.ID, ProviderSlug: k.ProviderSlug, Label: k.Label,
            MaskedSecret: k.MaskedSecret, LastUsed: k.LastUsed, Tags: k.Tags,
        })
    }

    // Flatten with provider headers
    items := make([]list.Item, 0)
    for providerSlug, keyList := range byProvider {
        // Add provider header
        items = append(items, keySectionHeader{provider: providerSlug})
        for _, k := range keyList {
            items = append(items, k)
        }
    }

    delegate := keyDelegate{styles: styles}
    l := list.New(items, delegate, 0, 0)
    l.Title = "API Keys"
    l.SetFilteringEnabled(true)

    return keysModel{list: l, styles: styles, byProvider: byProvider}
}

// Custom delegate handles section headers + key rows
type keyDelegate struct {
    styles *Styles
}

func (d keyDelegate) Height() int  { return 1 }
func (d keyDelegate) Spacing() int { return 0 }

func (d keyDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
    switch v := item.(type) {
    case keySectionHeader:
        fmt.Fprint(w, d.styles.SectionHeader.Render("  "+v.provider))
    case maskedKeyItem:
        selected := index == m.Index()
        line := fmt.Sprintf("    %-20s  %-20s  %s", v.Label, v.MaskedSecret, v.LastUsed)
        if selected {
            fmt.Fprint(w, d.styles.SelectedItem.Render("▸ "+line))
        } else {
            fmt.Fprint(w, d.styles.Item.Render("  "+line))
        }
    }
}
```

**Security**: Keys screen receives **already-masked** `MaskedKeyItem` from secret service. Raw secrets never enter TUI models.

---

### 4.4 Profiles Screen

**Purpose**: List, create, edit, validate profiles.

```go
// internal/tui/profiles.go
type profileItem struct {
    profile.Profile
    providerName string
    keyLabel     string
    valid        bool
}

type profilesModel struct {
    list   list.Model
    styles *Styles
    // ... data fields
}

func newProfilesModel(styles *Styles, profiles []profile.Profile, providers []provider.Provider, keys []secret.MaskedKeyItem) profilesModel {
    // Build items with resolved names
    items := make([]list.Item, len(profiles))
    for i, p := range profiles {
        prov := findProvider(providers, p.ProviderSlug)
        key := findKey(keys, p.KeyID)
        items[i] = profileItem{
            Profile: p,
            providerName: prov.Name,
            keyLabel: key.Label,
            valid: prov != nil && key != nil,
        }
    }
    // ... list setup
}
```

**Validation**: Show warning badges for profiles referencing missing keys/providers.

---

### 4.5 Launch Screen

**Purpose**: Select profile, enter command, preview env, launch.

**Critical**: Do NOT run interactive child processes inside Bubble Tea viewport.

```go
// internal/tui/launch.go
type launchModel struct {
    profileList list.Model
    cmdInput    textinput.Model
    previewVP   viewport.Model
    styles      *Styles
    width       int
    height      int
    focused     int // 0=profile list, 1=command input
    selectedProfile *profile.Profile
}

func (m launchModel) Update(msg tea.Msg) (launchModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "tab":
            m.focused = (m.focused + 1) % 2
            m.updateFocus()
            return m, nil
        case "enter":
            if m.focused == 1 && m.selectedProfile != nil {
                return m, m.launchCommand()
            }
        }
    }
    // Update focused component
    var cmd tea.Cmd
    if m.focused == 0 {
        m.profileList, cmd = m.profileList.Update(msg)
    } else {
        m.cmdInput, cmd = m.cmdInput.Update(msg)
    }
    return m, cmd
}

func (m launchModel) launchCommand() tea.Cmd {
    // Return a command that tells root model to exit TUI and run
    return func() tea.Msg {
        return launchRequestedMsg{
            Profile: m.selectedProfile,
            Command: m.cmdInput.Value(),
        }
    }
}
```

**Root model handles launch**:

```go
// In root update.go
case launchRequestedMsg:
    // 1. Exit alternate screen
    // 2. Run child process with env injection (via internal/runner)
    // 3. Return to TUI or quit
    return m, tea.Sequence(
        tea.ExitAltScreen,
        runChildProcessCmd(msg.Profile, msg.Command),
        tea.EnterAltScreen,
    )
```

---

### 4.6 Doctor Screen

**Purpose**: Display security checks with status icons.

```go
// internal/tui/doctor.go
type doctorModel struct {
    vp     viewport.Model
    styles *Styles
    results []security.CheckResult
}

func (m doctorModel) View(w, h int) string {
    m.vp.Width = w
    m.vp.Height = h

    var sb strings.Builder
    for _, r := range m.results {
        icon := "✓"
        style := m.styles.Success
        switch r.Severity {
        case security.SeverityWarn:
            icon = "!"
            style = m.styles.Warning
        case security.SeverityFail:
            icon = "✗"
            style = m.styles.Danger
        }
        sb.WriteString(fmt.Sprintf("%s %s\n", style.Render(icon), r.Message))
        if r.Fix != "" {
            sb.WriteString(fmt.Sprintf("    %s\n", m.styles.Muted.Render("Fix: "+r.Fix)))
        }
    }
    m.vp.SetContent(sb.String())
    return m.vp.View()
}
```

---

### 4.7 Audit Screen

**Purpose**: View audit log (metadata only, never secrets).

```go
// internal/tui/audit.go
type auditModel struct {
    vp     viewport.Model
    styles *Styles
    events []security.AuditEvent
}

func (m auditModel) View(w, h int) string {
    m.vp.Width = w
    m.vp.Height = h

    var sb strings.Builder
    for _, e := range m.events {
        sb.WriteString(fmt.Sprintf("[%s] %s profile=%s provider=%s cmd=%s\n",
            e.Time.Format("2006-01-02 15:04"),
            e.Event, e.Profile, e.Provider, e.Command))
    }
    m.vp.SetContent(sb.String())
    return m.vp.View()
}
```

---

### 4.8 Help Screen

**Purpose**: Render markdown docs with Glamour in viewport.

```go
// internal/tui/help.go
import "charm.land/glamour/v2"

type helpModel struct {
    vp     viewport.Model
    styles *Styles
    renderer *glamour.TermRenderer
}

func newHelpModel(styles *Styles) helpModel {
    r, _ := glamour.NewTermRenderer(
        glamour.WithAutoStyle(),
        glamour.WithWordWrap(80),
    )
    return helpModel{styles: styles, renderer: r}
}

func (m *helpModel) SetContent(markdown string) {
    rendered, _ := m.renderer.Render(markdown)
    m.vp.SetContent(rendered)
}

func (m helpModel) View(w, h int) string {
    m.vp.Width = w
    m.vp.Height = h
    return m.vp.View()
}
```

**Content files**:
- `docs/security.md`
- `docs/threat-model.md`
- `docs/providers.md`

---

### 4.9 Settings Screen

**Purpose**: Configure auto-lock, theme, export permissions, etc.

Use Huh form or Bubbles textinput for editing.

---

## 5. Styles & Theming

### 5.1 Central Styles File

```go
// internal/tui/styles.go
package tui

import (
    "charm.land/lipgloss/v2"
)

type Styles struct {
    // Layout
    AppFrame       lipgloss.Style
    Sidebar        lipgloss.Style
    SidebarItem    lipgloss.Style
    SidebarItemActive lipgloss.Style
    SidebarFooter  lipgloss.Style
    Header         lipgloss.Style
    Footer         lipgloss.Style
    Content        lipgloss.Style

    // Panels
    Panel          lipgloss.Style
    ActivePanel    lipgloss.Style
    PanelBorder    lipgloss.Border

    // Text
    Title          lipgloss.Style
    Subtitle       lipgloss.Style
    Muted          lipgloss.Style
    Body           lipgloss.Style

    // Status
    Success        lipgloss.Style
    Warning        lipgloss.Style
    Danger         lipgloss.Style
    Info           lipgloss.Style

    // Badges
    Badge          lipgloss.Style
    BadgeSecure    lipgloss.Style
    BadgeMissing   lipgloss.Style
    BadgeWarning   lipgloss.Style

    // Provider/Key specific
    ProviderName   lipgloss.Style
    ProviderMeta   lipgloss.Style
    KeyLabel       lipgloss.Style
    KeyMasked      lipgloss.Style
    SectionHeader  lipgloss.Style

    // List
    ListTitle      lipgloss.Style
    ListItem       lipgloss.Style
    SelectedItem   lipgloss.Style
    Pagination     lipgloss.Style
    Help           lipgloss.Style

    // Inputs
    Input          lipgloss.Style
    InputFocused   lipgloss.Style

    // Viewport
    Viewport       lipgloss.Style
}

var (
    // Color palette (ANSI 256, works in most terminals)
    vaultGold   = lipgloss.Color("214")
    ironGray    = lipgloss.Color("240")
    signalGreen = lipgloss.Color("46")
    warnAmber   = lipgloss.Color("214")
    dangerRed   = lipgloss.Color("196")
    mutedBlue   = lipgloss.Color("39")
    bgDark      = lipgloss.Color("235")
    fgLight     = lipgloss.Color("252")
)

func NewStyles(darkBG bool) *Styles {
    s := &Styles{}

    // Base styles
    s.AppFrame = lipgloss.NewStyle().
        Background(bgDark).
        Foreground(fgLight)

    s.Sidebar = lipgloss.NewStyle().
        Width(28).
        Padding(1, 2).
        Background(lipgloss.Color("236")).
        Border(lipgloss.NormalBorder(), false, true, false, false).
        BorderForeground(ironGray)

    s.SidebarItem = lipgloss.NewStyle().
        Padding(0, 1).
        Foreground(fgLight)

    s.SidebarItemActive = s.SidebarItem.
        Background(vaultGold).
        Foreground(lipgloss.Color("232")).
        Bold(true)

    s.Header = lipgloss.NewStyle().
        Bold(true).
        Foreground(vaultGold).
        MarginBottom(1)

    s.Footer = lipgloss.NewStyle().
        Height(1).
        Padding(0, 1).
        Background(lipgloss.Color("236")).
        Foreground(ironGray).
        Align(lipgloss.Center)

    // Panels
    s.Panel = lipgloss.NewStyle().
        Padding(1, 2).
        Border(lipgloss.RoundedBorder()).
        BorderForeground(ironGray)

    s.ActivePanel = s.Panel.
        BorderForeground(vaultGold)

    // Status colors
    s.Success = lipgloss.NewStyle().Foreground(signalGreen).Bold(true)
    s.Warning = lipgloss.NewStyle().Foreground(warnAmber).Bold(true)
    s.Danger  = lipgloss.NewStyle().Foreground(dangerRed).Bold(true)
    s.Info    = lipgloss.NewStyle().Foreground(mutedBlue).Bold(true)

    // Badges
    s.Badge = lipgloss.NewStyle().
        Padding(0, 1).
        Background(ironGray).
        Foreground(fgLight).
        Bold(true)

    s.BadgeSecure = s.Badge.Background(signalGreen).Foreground(lipgloss.Color("232"))
    s.BadgeMissing = s.Badge.Background(warnAmber).Foreground(lipgloss.Color("232"))
    s.BadgeWarning = s.Badge.Background(dangerRed).Foreground(lipgloss.Color("232"))

    // Provider/Key
    s.ProviderName = lipgloss.NewStyle().Bold(true).Foreground(vaultGold)
    s.ProviderMeta = lipgloss.NewStyle().Foreground(ironGray)
    s.KeyLabel = lipgloss.NewStyle().Foreground(fgLight)
    s.KeyMasked = lipgloss.NewStyle().Foreground(mutedBlue)
    s.SectionHeader = lipgloss.NewStyle().Bold(true).Foreground(vaultGold).Underline(true)

    // List
    s.ListTitle = lipgloss.NewStyle().Bold(true).Foreground(vaultGold).MarginLeft(2)
    s.ListItem = lipgloss.NewStyle().PaddingLeft(4)
    s.SelectedItem = lipgloss.NewStyle().PaddingLeft(2).Foreground(vaultGold).Bold(true)
    s.Pagination = lipgloss.NewStyle().PaddingLeft(4).Foreground(ironGray)
    s.Help = lipgloss.NewStyle().PaddingLeft(4).PaddingBottom(1).Foreground(ironGray)

    // Inputs
    s.Input = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ironGray)
    s.InputFocused = s.Input.BorderForeground(vaultGold)

    // Viewport
    s.Viewport = lipgloss.NewStyle().Padding(1, 2)

    return s
}
```

### 5.2 Responsive Sizing

Always compute content area dynamically:

```go
func (m *Model) resizeChildren() {
    sidebarW := 28
    headerH := 3
    footerH := 1
    padding := 4

    contentW := m.width - sidebarW - padding
    contentH := m.height - headerH - footerH - padding

    if contentW < 40 { contentW = 40 }
    if contentH < 10 { contentH = 10 }

    m.dashboard.SetSize(contentW, contentH)
    m.providers.list.SetSize(contentW, contentH)
    m.keys.list.SetSize(contentW, contentH)
    // ... etc
}
```

---

## 6. Component Patterns

### 6.1 Bubbles List with Custom Delegate

```go
type customDelegate struct {
    styles *Styles
    renderFunc func(item list.Item, selected bool) string
}

func (d customDelegate) Height() int  { return 1 }
func (d customDelegate) Spacing() int { return 0 }
func (d customDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d customDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
    selected := index == m.Index()
    fmt.Fprint(w, d.renderFunc(item, selected))
}

l := list.New(items, customDelegate{styles: s, renderFunc: myRender}, w, h)
```

### 6.2 Text Input with Focus Management

```go
type formModel struct {
    inputs  []textinput.Model
    focused int
}

func (m *formModel) focusNext() {
    m.inputs[m.focused].Blur()
    m.focused = (m.focused + 1) % len(m.inputs)
    m.inputs[m.focused].Focus()
}

func (m formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        if msg.String() == "tab" {
            m.focusNext()
            return m, nil
        }
    }
    var cmd tea.Cmd
    m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
    return m, cmd
}
```

### 6.3 Viewport for Scrollable Content

```go
vp := viewport.New(width, height)
vp.SetContent(longContent)
vp.Style = styles.Viewport
// In View():
vp.Width = contentWidth
vp.Height = contentHeight
return vp.View()
```

### 6.4 Spinner for Async Operations

```go
sp := spinner.New()
sp.Spinner = spinner.Dot
sp.Style = styles.Spinner

// In Update:
case spinner.TickMsg:
    var cmd tea.Cmd
    m.spinner, cmd = m.spinner.Update(msg)
    return m, cmd

// In View:
m.spinner.View() + " Loading..."
```

### 6.5 Glamour Markdown Rendering

```go
r, _ := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),
    glamour.WithWordWrap(contentWidth),
)
rendered, _ := r.Render(markdown)
vp.SetContent(rendered)
```

---

## 7. Security-Critical Patterns

### 7.1 Never Store Raw Secrets in TUI Models

```go
// ✅ GOOD: TUI receives masked view models
type MaskedKeyItem struct {
    ID           string
    ProviderSlug string
    Label        string
    MaskedSecret string  // "sk-or-v1-...91ef"
    LastUsed     string
}

// ❌ BAD: Raw secret in TUI model
type KeyScreenModel struct {
    Selected secret.SecretRecord  // Contains raw secret!
}
```

### 7.2 All Blocking I/O via Commands

```go
// ❌ BAD: Blocks UI
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    results := security.RunDoctor() // BLOCKS!
    m.results = results
    return m, nil
}

// ✅ GOOD: Async via command
func runDoctorCmd() tea.Cmd {
    return func() tea.Msg {
        return doctorFinishedMsg{Results: security.RunDoctor()}
    }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    case "r":
        return m, runDoctorCmd()
    case doctorFinishedMsg:
        m.results = msg.Results
        return m, nil
}
```

### 7.3 Redact All Output

```go
import "aegiskeys/internal/security/redact"

// Any string that might contain secrets
safeOutput := redact.Redact(outputString)
```

### 7.4 Vault Unlock Flow

```go
type unlockVaultMsg struct{ password string }
type vaultUnlockedMsg struct{ keys []MaskedKeyItem }
type vaultLockMsg struct{}

func unlockVaultCmd(password string) tea.Cmd {
    return func() tea.Msg {
        keys, err := secret.UnlockVault(password)
        if err != nil {
            return errMsg{err}
        }
        return vaultUnlockedMsg{Keys: keys}
    }
}
```

---

## 8. Charm Apps to Study

| Repo | Location | What to Learn |
|------|----------|---------------|
| **Bubble Tea Examples** | `/home/bamn/TUIs/bubbletea/examples/` | Fundamentals: tabs, list, exec, textarea, help, viewport, Glamour, fullscreen |
| **Bubbles** | `/home/bamn/TUIs/bubbles/` | List, viewport, spinner, help, table, textinput, textarea implementations |
| **Lip Gloss** | `/home/bamn/TUIs/lipgloss/` | Styling, borders, layout, color blending, responsive sizing |
| **Huh** | `/home/bamn/TUIs/huh/` | Form patterns, field types, validation, accessible mode |
| **Glamour** | `/home/bamn/TUIs/glamour/` | Markdown rendering, custom stylesheets |
| **Glow** | `/home/bamn/TUIs/glow/` | Markdown viewer layout, viewport scrolling, doc UX |
| **Soft Serve** | `/home/bamn/TUIs/soft-serve/pkg/ui/` | Multi-pane app, sidebar+content, nested navigation, SSH TUI |
| **Gum** | `/home/bamn/TUIs/gum/` | Minimal prompt UX, confirmations, selectable lists |
| **VHS** | `/home/bamn/TUIs/vhs/` | `.tape` demo recording |
| **Freeze** | `/home/bamn/TUIs/freeze/` | Terminal screenshot capture |

### Key Files to Read

```
/home/bamn/TUIs/bubbletea/examples/tabs/main.go          # Tab navigation pattern
/home/bamn/TUIs/bubbletea/examples/list-simple/main.go   # Basic list usage
/home/bamn/TUIs/bubbletea/examples/exec/main.go          # Subprocess launching
/home/bamn/TUIs/bubbles/list/list.go                     # List component internals
/home/bamn/TUIs/soft-serve/pkg/ui/styles/styles.go       # Large-scale theming
/home/bamn/TUIs/soft-serve/pkg/ui/common/common.go       # Shared UI context pattern
/home/bamn/TUIs/glamour/examples/helloworld/main.go      # Glamour basic usage
```

---

## 9. Pitfalls to Avoid

| # | Pitfall | Solution |
|---|---------|----------|
| 1 | Mixing v1/v2 imports | Use only `charm.land/.../v2` |
| 2 | Raw secrets in TUI models | Pass only masked view models |
| 3 | Blocking I/O in Update | Use `tea.Cmd` for all async work |
| 4 | Ignoring `tea.WindowSizeMsg` | Call `SetSize()` on all children |
| 5 | Hardcoding widths | Use `lipgloss.Width()`, `GetHorizontalFrameSize()` |
| 6 | Emoji for alignment | Use ASCII/nerd-font-optional symbols |
| 7 | Color-only status | Always include text: `✓ OK`, `! WARN`, `✗ FAIL` |
| 8 | Mouse required | Keyboard-first; mouse additive only |
| 9 | Focus bugs in forms | Track `focusedField`, only focused input receives text |
| 10 | Full TUI for simple ops | CLI commands for list/doctor; TUI for complex flows |
| 11 | Child process in viewport | Exit/suspend TUI, run child, resume |
| 12 | Business logic in TUI | TUI = presentation only; services in `internal/` |
| 13 | Re-rendering everything | Cache expensive renders; invalidate on resize/data change |
| 14 | No secret redaction | Central `redact` package; apply to all output |

---

## 10. Implementation Checklist

### Phase 1: Foundation
- [ ] Project structure with `internal/tui/`
- [ ] `styles.go` with complete theme
- [ ] Root `model.go` with screen routing
- [ ] `update.go` global message handling
- [ ] `view.go` global layout (sidebar + content + footer)
- [ ] Window resize handling

### Phase 2: Core Screens
- [ ] Dashboard: vault status, counts, quick actions
- [ ] Providers: `bubbles/list` with filtering, badges
- [ ] Keys: masked secrets, grouped by provider
- [ ] Profiles: validation status, resolved names
- [ ] Help: Glamour + viewport for markdown docs

### Phase 3: Interactive Screens
- [ ] Launch: profile picker + command input + preview
- [ ] Doctor: viewport with status icons + fixes
- [ ] Audit: viewport with redacted events
- [ ] Settings: form for preferences

### Phase 4: Integration
- [ ] Wire services (provider, secret, profile, runner, security, audit)
- [ ] Vault unlock/lock flow
- [ ] Data refresh commands
- [ ] Error/status message system

### Phase 5: Polish
- [ ] Key bindings help (`?` screen)
- [ ] Empty states for all screens
- [ ] Loading spinners for async ops
- [ ] VHS demo tapes
- [ ] Freeze screenshots for README

### Phase 6: Security Verification
- [ ] No raw secrets in any TUI struct
- [ ] All output passes through redact
- [ ] Doctor checks run async
- [ ] Child process launches outside TUI
- [ ] File permissions 0600/0700 enforced

---

## Appendix: Key Bindings Reference

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Next/previous screen |
| `1`-`8` | Jump to screen |
| `j`/`k` or `↑`/`↓` | Navigate list |
| `/` | Filter list |
| `Enter` | Select/activate |
| `Esc` | Back/cancel |
| `a` | Add (context-dependent) |
| `e` | Edit selected |
| `d` | Delete (with confirm) |
| `r` | Refresh/run doctor |
| `?` | Toggle help |
| `Ctrl+L` | Lock vault |
| `Ctrl+C` / `q` | Quit (from dashboard) / Back |

---

## Appendix: Screen Data Flow

```
Root Model (internal/tui/model.go)
    │
    ├── Services (internal/*)
    │     ├── provider.Registry → []Provider
    │     ├── secret.Vault → []MaskedKeyItem
    │     ├── profile.Store → []Profile
    │     ├── security.Doctor → []CheckResult
    │     └── audit.Log → []AuditEvent
    │
    ├── On data change: root updates child models via SetData()
    │
    └── Child screens render via View(width, height) using Styles
```

---

*Generated for AegisKeys TUI implementation using Charmbracelet v2 stack.*