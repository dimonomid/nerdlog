package core

import (
	"strings"
)

// EvaluateMatchDirective evaluates SSH config Match directives against the current environment.
// This is a placeholder implementation that can be extended to fully support Match conditions.
func EvaluateMatchDirective(matchConditions map[string]string, currentHost string, currentUser string) bool {
	for key, value := range matchConditions {
		switch strings.ToLower(key) {
		case "host":
			if !matchPattern(value, currentHost) {
				return false
			}
		case "user":
			if !matchPattern(value, currentUser) {
				return false
			}
		// Add more condition types as needed.
		default:
			// Unknown condition, assume no match.
			return false
		}
	}
	return true
}

// matchPattern checks if the pattern matches the target string.
// Supports simple glob patterns with '*' and '?'.
func matchPattern(pattern, target string) bool {
	// Simple implementation: convert glob to regex or use existing glob library.
	// For now, only support '*' wildcard at start or end.
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		substr := pattern[1 : len(pattern)-1]
		return strings.Contains(target, substr)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(target, suffix)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(target, prefix)
	}
	return pattern == target
}
