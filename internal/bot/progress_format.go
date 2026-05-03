package bot

import (
	"fmt"
	"sort"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

func formatProgressGroupedHTML(prog *healthpb.GetProgressResponse, entries []*healthpb.UserCriterionEntry, groups []*healthpb.CriterionGroup) string {
	var b strings.Builder
	b.WriteString("<b>📊 Мой прогресс</b>\n\n")
	b.WriteString(fmt.Sprintf("Уровень: <b>%s</b>\n", prog.GetLevelLabel()))
	pct := prog.GetPercent()
	filled := int(pct / 10)
	empty := 10 - filled
	if filled < 0 {
		filled = 0
	}
	if empty < 0 {
		empty = 0
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", empty)
	b.WriteString(fmt.Sprintf("Прогресс: %s %.0f%%\n", bar, pct))
	b.WriteString(fmt.Sprintf("Заполнено: %d / %d критериев\n\n", prog.GetFilled(), prog.GetTotal()))

	if len(entries) == 0 {
		b.WriteString("Данные пока не добавлены. Нажмите «➕ Добавить данные»!")
		return b.String()
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].GetSortOrder() != groups[j].GetSortOrder() {
			return groups[i].GetSortOrder() < groups[j].GetSortOrder()
		}
		return groups[i].GetName() < groups[j].GetName()
	})
	groupSet := make(map[string]*healthpb.CriterionGroup, len(groups))
	for _, g := range groups {
		groupSet[g.GetId()] = g
	}

	writeEntry := func(e *healthpb.UserCriterionEntry) {
		icon := statusEmoji(e.GetStatus())
		b.WriteString(fmt.Sprintf("%s %s", icon, e.GetCriterionName()))
		if an := strings.TrimSpace(e.GetAnalysisName()); an != "" {
			b.WriteString(fmt.Sprintf(" (%s)", escapeHTML(an)))
		}
		if e.GetValue() != "" {
			b.WriteString(fmt.Sprintf(" — <b>%s</b>", escapeHTML(e.GetValue())))
		}
		b.WriteString("\n")
	}

	for _, g := range groups {
		var block []*healthpb.UserCriterionEntry
		for _, e := range entries {
			if e.GetGroupId() == g.GetId() {
				block = append(block, e)
			}
		}
		if len(block) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("<b>%s</b>\n", escapeHTML(g.GetName())))
		for _, e := range block {
			writeEntry(e)
		}
		b.WriteString("\n")
	}

	var ungrouped []*healthpb.UserCriterionEntry
	for _, e := range entries {
		gid := e.GetGroupId()
		if gid == "" || groupSet[gid] == nil {
			ungrouped = append(ungrouped, e)
		}
	}
	if len(ungrouped) > 0 {
		b.WriteString("<b>Прочее</b>\n")
		for _, e := range ungrouped {
			writeEntry(e)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
