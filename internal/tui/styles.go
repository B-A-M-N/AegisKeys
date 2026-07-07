package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// sidebarWidth is the fixed TUI sidebar width in columns.
const sidebarWidth = 26

// themeOrder lists available themes in display/persistence order. The first
// entry is the default.
var themeOrder = []string{"vault", "light", "matrix", "ice", "ember", "mono"}

// ThemeNames returns a copy of the available theme names.
func ThemeNames() []string { return append([]string{}, themeOrder...) }

// normalizeTheme maps legacy ("dark") or unknown theme names to a valid theme.
func normalizeTheme(name string) string {
	switch name {
	case "dark", "":
		return "vault"
	case "light":
		return "light"
	}
	for _, t := range themeOrder {
		if t == name {
			return name
		}
	}
	return "vault"
}

// palette holds the raw colors for a single theme.
type palette struct {
	appBG, panelBG, sidebarBG string

	accent     string // primary accent: borders, selection, values
	accentSoft string // softer accent: unfocused borders, modal title
	muted      string // secondary text
	darkGray   string // footer / disabled text
	white      string
	red        string // danger
	success    string // ok / secure
	warning    string // warnings / info

	// Matrix-rain gradient (dim -> bright).
	mVoid, mDim, mDeep, mMid, mBright, mWhite, mRed string

	light bool
}

// themes maps theme name -> palette.
var themes = map[string]palette{
	"vault": {
		appBG: "#000000", panelBG: "#0d0d14", sidebarBG: "#080810",
		accent: "#9b6dff", accentSoft: "#6a3fb0", muted: "#7a6f8a", darkGray: "#4a4a5a",
		white: "#ffffff", red: "#ff3b5c", success: "#39ff14", warning: "#9b6dff",
		mVoid: "#0a0014", mDim: "#2a1a4a", mDeep: "#4a2d7a", mMid: "#7b4dcc",
		mBright: "#a866ff", mWhite: "#ffffff", mRed: "#ff2d55",
	},
	"light": {
		appBG: "#f0f0f0", panelBG: "#ffffff", sidebarBG: "#e0e0e0",
		accent: "#6a3fb0", accentSoft: "#cccccc", muted: "#666666", darkGray: "#888888",
		white: "#333333", red: "#cc0000", success: "#228b22", warning: "#ff8c00",
		mVoid: "#d0d0d0", mDim: "#b0b0b0", mDeep: "#909090", mMid: "#7b4dcc",
		mBright: "#a866ff", mWhite: "#444444", mRed: "#cc0000",
		light: true,
	},
	"matrix": {
		appBG: "#000000", panelBG: "#03130a", sidebarBG: "#020a06",
		accent: "#39ff14", accentSoft: "#1f9e3a", muted: "#3fa86a", darkGray: "#2a5a3a",
		white: "#d8ffe8", red: "#ff3b5c", success: "#39ff14", warning: "#ffd23b",
		mVoid: "#001a08", mDim: "#064d1a", mDeep: "#0c7a2c", mMid: "#1fb84a",
		mBright: "#39ff14", mWhite: "#d8ffe8", mRed: "#ff3b5c",
	},
	"ice": {
		appBG: "#001018", panelBG: "#041420", sidebarBG: "#020c14",
		accent: "#36c5f0", accentSoft: "#1f7fa8", muted: "#5a8aa0", darkGray: "#33586a",
		white: "#e8f8ff", red: "#ff5c7a", success: "#36f0a0", warning: "#ffd23b",
		mVoid: "#021018", mDim: "#063a4a", mDeep: "#0a5e78", mMid: "#1f9ec8",
		mBright: "#36c5f0", mWhite: "#e8f8ff", mRed: "#ff5c7a",
	},
	"ember": {
		appBG: "#0a0600", panelBG: "#140d04", sidebarBG: "#0c0804",
		accent: "#ff8c1a", accentSoft: "#b35e0f", muted: "#a07a4a", darkGray: "#5a4631",
		white: "#fff0e0", red: "#ff3b3b", success: "#9bd13b", warning: "#ffb020",
		mVoid: "#1a0d00", mDim: "#4d2a06", mDeep: "#7a450c", mMid: "#c2701a",
		mBright: "#ff8c1a", mWhite: "#fff0e0", mRed: "#ff3b3b",
	},
	"mono": {
		appBG: "#0a0a0a", panelBG: "#141414", sidebarBG: "#0c0c0c",
		accent: "#cccccc", accentSoft: "#888888", muted: "#888888", darkGray: "#555555",
		white: "#ffffff", red: "#ff5555", success: "#bbbbbb", warning: "#dddddd",
		mVoid: "#0a0a0a", mDim: "#2a2a2a", mDeep: "#4a4a4a", mMid: "#7a7a7a",
		mBright: "#cccccc", mWhite: "#ffffff", mRed: "#ff5555",
	},
}

// Styles holds every Lip Gloss style for the TUI so nothing is ad-hoc.
type Styles struct {
	ThemeName string
	Light     bool

	// App
	AppBackground lipgloss.Style

	// Sidebar
	Sidebar           lipgloss.Style
	SidebarItem       lipgloss.Style
	SidebarItemActive lipgloss.Style
	SidebarHeader     lipgloss.Style
	SidebarFooter     lipgloss.Style
	SidebarFocused    lipgloss.Style // stronger border when sidebar owns focus

	// Content / panels
	Panel        lipgloss.Style
	PanelFocused lipgloss.Style
	PanelHeader  lipgloss.Style
	Content      lipgloss.Style

	// Text
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Muted    lipgloss.Style
	Body     lipgloss.Style
	Value    lipgloss.Style

	// Status
	Success lipgloss.Style
	Warning lipgloss.Style
	Danger  lipgloss.Style
	Info    lipgloss.Style

	// Lists / rows
	Row         lipgloss.Style
	SelectedRow lipgloss.Style

	// Badges
	Badge        lipgloss.Style
	BadgeSecure  lipgloss.Style
	BadgeMissing lipgloss.Style
	BadgeWarning lipgloss.Style

	// Header / footer
	Header        lipgloss.Style
	Footer        lipgloss.Style
	SectionHeader lipgloss.Style
	KeyLabel      lipgloss.Style
	KeyMasked     lipgloss.Style

	// Help
	HelpStyle lipgloss.Style

	// Viewport
	Viewport lipgloss.Style

	// Matrix rain glyphs (dim → bright)
	MatrixVoid   lipgloss.Style
	MatrixDim    lipgloss.Style
	MatrixDeep   lipgloss.Style
	MatrixPurple lipgloss.Style
	MatrixBright lipgloss.Style
	MatrixWhite  lipgloss.Style
	MatrixRed    lipgloss.Style

	// Cell-safe styles for grid compositor (foreground/background only)
	CellDefault     lipgloss.Style
	CellBody        lipgloss.Style
	CellMuted       lipgloss.Style
	CellSelected    lipgloss.Style
	CellSidebar     lipgloss.Style
	CellFooter      lipgloss.Style
	CellTitle       lipgloss.Style
	CellAccent      lipgloss.Style
	CellHeader      lipgloss.Style
	CellSuccess     lipgloss.Style
	CellDanger      lipgloss.Style
	CellModalBg     lipgloss.Style
	CellModalBorder lipgloss.Style
	CellModalTitle  lipgloss.Style
	CellModalText   lipgloss.Style

	// Modal / form overlay (NOT for per-cell use)
	Modal lipgloss.Style
}

// NewStyles builds the theme for the named theme. Unknown or legacy names
// fall back to the default ("vault").
func NewStyles(theme string) *Styles {
	name := normalizeTheme(theme)
	s := &Styles{ThemeName: name, Light: themes[name].light}
	return buildStyles(s, themes[name])
}

// buildStyles applies every style from the given palette.
func buildStyles(s *Styles, p palette) *Styles {
	s.AppBackground = lipgloss.NewStyle().Background(lipgloss.Color(p.appBG))

	// Sidebar
	s.Sidebar = lipgloss.NewStyle().
		Background(lipgloss.Color(p.sidebarBG)).
		Padding(0, 1)
	s.SidebarFocused = s.Sidebar.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(p.accentSoft))
	s.SidebarItem = lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Padding(0, 1)
	s.SidebarItemActive = lipgloss.NewStyle().
		Foreground(lipgloss.Color(p.appBG)).
		Background(lipgloss.Color(p.accent)).
		Bold(true).
		Padding(0, 1)
	s.SidebarHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.white))
	s.SidebarFooter = lipgloss.NewStyle().Foreground(lipgloss.Color(p.darkGray))

	// Content panels
	s.Panel = lipgloss.NewStyle().
		Background(lipgloss.Color(p.panelBG)).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.accentSoft))
	s.PanelFocused = lipgloss.NewStyle().
		Background(lipgloss.Color(p.panelBG)).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.accent))
	s.PanelHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.accent))
	s.Content = lipgloss.NewStyle().Background(lipgloss.Color(p.panelBG)).Padding(1, 2)

	// Text
	s.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.white))
	s.Subtitle = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent))
	s.Muted = lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted))
	s.Body = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white))
	s.Value = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true)

	// Status
	s.Success = lipgloss.NewStyle().Foreground(lipgloss.Color(p.success)).Bold(true)
	s.Warning = lipgloss.NewStyle().Foreground(lipgloss.Color(p.warning)).Bold(true)
	s.Danger = lipgloss.NewStyle().Foreground(lipgloss.Color(p.red)).Bold(true)
	s.Info = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true)

	// Rows
	s.Row = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white))
	s.SelectedRow = lipgloss.NewStyle().
		Foreground(lipgloss.Color(p.appBG)).
		Background(lipgloss.Color(p.accent)).
		Bold(true)

	// Badges
	s.Badge = lipgloss.NewStyle().Padding(0, 1).Background(lipgloss.Color(p.darkGray)).Foreground(lipgloss.Color(p.white)).Bold(true)
	s.BadgeSecure = s.Badge.Background(lipgloss.Color(p.success)).Foreground(lipgloss.Color(p.appBG))
	s.BadgeMissing = s.Badge.Background(lipgloss.Color(p.accent)).Foreground(lipgloss.Color(p.appBG))
	s.BadgeWarning = s.Badge.Background(lipgloss.Color(p.red)).Foreground(lipgloss.Color(p.appBG))

	// Header / footer
	s.Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.accent)).MarginBottom(1)
	s.Footer = lipgloss.NewStyle().Foreground(lipgloss.Color(p.darkGray))
	s.SectionHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.white)).Underline(true)
	s.KeyLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white))
	s.KeyMasked = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent))

	s.HelpStyle = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color(p.muted))

	s.Viewport = lipgloss.NewStyle().Background(lipgloss.Color(p.panelBG))

	// Matrix rain glyphs
	s.MatrixVoid = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mVoid))
	s.MatrixDim = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mDim))
	s.MatrixDeep = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mDeep))
	s.MatrixPurple = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mMid))
	s.MatrixBright = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mBright)).Bold(true)
	s.MatrixWhite = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mWhite)).Bold(true)
	s.MatrixRed = lipgloss.NewStyle().Foreground(lipgloss.Color(p.mRed)).Bold(true)

	// Cell-safe styles for grid compositor (foreground/background only)
	s.CellDefault = lipgloss.NewStyle()
	s.CellBody = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white))
	s.CellMuted = lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted))
	s.CellSelected = lipgloss.NewStyle().Foreground(lipgloss.Color(p.appBG)).Background(lipgloss.Color(p.accent)).Bold(true)
	s.CellSidebar = lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Background(lipgloss.Color(p.sidebarBG))
	s.CellFooter = lipgloss.NewStyle().Foreground(lipgloss.Color(p.darkGray)).Background(lipgloss.Color(p.appBG))
	s.CellTitle = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white)).Bold(true)
	s.CellAccent = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true)
	s.CellHeader = lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Underline(true)
	s.CellSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color(p.success)).Bold(true)
	s.CellDanger = lipgloss.NewStyle().Foreground(lipgloss.Color(p.red)).Bold(true)
	s.CellModalBg = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white)).Background(lipgloss.Color(p.panelBG))
	s.CellModalBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(p.red)).Background(lipgloss.Color(p.panelBG)).Bold(true)
	s.CellModalTitle = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Background(lipgloss.Color(p.panelBG)).Bold(true)
	s.CellModalText = lipgloss.NewStyle().Foreground(lipgloss.Color(p.white)).Background(lipgloss.Color(p.panelBG))

	// Modal / form overlay
	s.Modal = lipgloss.NewStyle().
		Background(lipgloss.Color(p.panelBG)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.red)).
		Padding(1, 2)

	return s
}

// truncate clips s to at most maxWidth display columns, adding an ellipsis.
func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return lipgloss.NewStyle().Width(maxWidth).Render(s)
	}
	out := ""
	w := 0
	for _, r := range s {
		cw := lipgloss.Width(string(r))
		if w+cw > maxWidth-3 {
			break
		}
		out += string(r)
		w += cw
	}
	return out + "..."
}

// padRight pads/truncates s to exactly maxWidth columns.
func padRight(s string, maxWidth int) string {
	w := lipgloss.Width(s)
	if w > maxWidth {
		return truncate(s, maxWidth)
	}
	if w < maxWidth {
		return s + strings.Repeat(" ", maxWidth-w)
	}
	return s
}
