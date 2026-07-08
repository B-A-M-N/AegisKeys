package tui

import (
	"fmt"
	"strings"
)

// modelCatalogView renders the full-screen overlay-style model catalog view.
// Called when m.modelCatalog.active is true.
func (m *model) modelCatalogView(s *Styles) string {
	var b strings.Builder

	prov := m.providers.Find(m.modelCatalog.providerSlug)
	provName := m.modelCatalog.providerSlug
	if prov != nil {
		provName = prov.Name
	}

	b.WriteString(s.Title.Render(fmt.Sprintf("Model Catalog — %s", provName)))
	b.WriteString("\n\n")

	// Status line.
	filtered := m.filteredCatalogModels()
	if m.modelCatalog.fetching {
		b.WriteString(s.Success.Render("⟳ fetching models..."))
		b.WriteString("\n")
	} else if m.modelCatalog.errMsg != "" {
		b.WriteString(s.Danger.Render("✗ " + m.modelCatalog.errMsg))
		b.WriteString("\n")
	} else {
		cacheAge := cacheAgeString(m)
		status := fmt.Sprintf("%d models (%d selected)", len(filtered), m.selectedCount())
		if len(filtered) < len(m.modelCatalog.models) {
			status = fmt.Sprintf("%d/%d models (%d selected)", len(filtered), len(m.modelCatalog.models), m.selectedCount())
		}
		if cacheAge != "" {
			status += "  [" + cacheAge + "]"
		}
		b.WriteString(s.Body.Render(status))
		b.WriteString("\n")
	}

	// Filter line.
	if m.modelCatalog.filtering {
		b.WriteString(s.Warning.Render(fmt.Sprintf("  filter: %s█", m.modelCatalog.filter)))
		b.WriteString("\n")
	} else if m.modelCatalog.filter != "" {
		b.WriteString(s.Muted.Render(fmt.Sprintf("  filter: %s", m.modelCatalog.filter)))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Scrollable list.
	if len(filtered) == 0 {
		b.WriteString(s.Muted.Render("  (no models — press 'r' to refresh)"))
		b.WriteString("\n")
	} else {
		// Show up to 20 rows, centered on cursor.
		maxRows := 20
		start := 0
		if m.modelCatalog.cursor >= maxRows {
			start = m.modelCatalog.cursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(filtered) {
			end = len(filtered)
		}
		if start > 0 {
			b.WriteString(s.Muted.Render(fmt.Sprintf("  ... %d more above ...", start)))
			b.WriteString("\n")
		}
		for i := start; i < end; i++ {
			mod := filtered[i]
			marker := "[ ]"
			if m.modelCatalog.selected[mod.ID] {
				marker = s.Success.Render("[x]")
			}
			staticBadge := ""
			// Static indicator if this was in the provider's original static list.
			if prov != nil {
				for _, pm := range prov.Models {
					if pm.ID == mod.ID {
						staticBadge = s.Muted.Render(" (static)")
						break
					}
				}
			}
			idStr := truncate(mod.ID, 36)
			nameStr := ""
			if strings.TrimSpace(mod.Name) != "" && mod.Name != mod.ID {
				nameStr = "  " + s.Muted.Render(truncate(mod.Name, 24))
			}
			line := fmt.Sprintf("  %s %s%s%s", marker, idStr, nameStr, staticBadge)
			if i == m.modelCatalog.cursor {
				b.WriteString("› " + s.SelectedRow.Render(line[2:]))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
		if end < len(filtered) {
			b.WriteString(s.Muted.Render(fmt.Sprintf("  ... %d more below ...", len(filtered)-end)))
			b.WriteString("\n")
		}
	}

	// Footer.
	b.WriteString("\n")
	if m.modelCatalog.filtering {
		b.WriteString(s.Muted.Render(
			"type to filter · Enter/Esc to close · backspace to erase"))
	} else {
		b.WriteString(s.Muted.Render(
			"r refresh  / filter  space toggle  a all  c clear  s save  q quit"))
	}

	return b.String()
}
