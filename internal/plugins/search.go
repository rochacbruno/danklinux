package plugins

import (
	"sort"
	"strings"
)

func FuzzySearch(query string, plugins []Plugin) []Plugin {
	if query == "" {
		return plugins
	}

	queryLower := strings.ToLower(query)
	var results []Plugin

	for _, plugin := range plugins {
		if fuzzyMatch(queryLower, strings.ToLower(plugin.Name)) ||
			fuzzyMatch(queryLower, strings.ToLower(plugin.Category)) ||
			fuzzyMatch(queryLower, strings.ToLower(plugin.Description)) ||
			fuzzyMatch(queryLower, strings.ToLower(plugin.Author)) {
			results = append(results, plugin)
		}
	}

	return results
}

func fuzzyMatch(query, text string) bool {
	queryIdx := 0
	for _, char := range text {
		if queryIdx < len(query) && char == rune(query[queryIdx]) {
			queryIdx++
		}
	}
	return queryIdx == len(query)
}

func FilterByCategory(category string, plugins []Plugin) []Plugin {
	if category == "" {
		return plugins
	}

	var results []Plugin
	categoryLower := strings.ToLower(category)

	for _, plugin := range plugins {
		if strings.ToLower(plugin.Category) == categoryLower {
			results = append(results, plugin)
		}
	}

	return results
}

func FilterByCompositor(compositor string, plugins []Plugin) []Plugin {
	if compositor == "" {
		return plugins
	}

	var results []Plugin
	compositorLower := strings.ToLower(compositor)

	for _, plugin := range plugins {
		for _, comp := range plugin.Compositors {
			if strings.ToLower(comp) == compositorLower {
				results = append(results, plugin)
				break
			}
		}
	}

	return results
}

func FilterByCapability(capability string, plugins []Plugin) []Plugin {
	if capability == "" {
		return plugins
	}

	var results []Plugin
	capabilityLower := strings.ToLower(capability)

	for _, plugin := range plugins {
		for _, cap := range plugin.Capabilities {
			if strings.ToLower(cap) == capabilityLower {
				results = append(results, plugin)
				break
			}
		}
	}

	return results
}

// SortByFirstParty sorts plugins with first-party plugins at the top
func SortByFirstParty(plugins []Plugin) []Plugin {
	sort.SliceStable(plugins, func(i, j int) bool {
		// First-party plugins come first
		if plugins[i].FirstParty != plugins[j].FirstParty {
			return plugins[i].FirstParty
		}
		// Otherwise maintain original order (stable sort)
		return false
	})
	return plugins
}
