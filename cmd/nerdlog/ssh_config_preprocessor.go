package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// preprocessSSHConfig reads the SSH config file at path, recursively processes Include directives,
// and returns the combined config content as a string.
func preprocessSSHConfig(path string, visited map[string]bool) (string, error) {
	if visited == nil {
		visited = make(map[string]bool)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path of %s: %w", path, err)
	}

	if visited[absPath] {
		// Prevent cyclic includes
		return "", nil
	}
	visited[absPath] = true

	file, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to open ssh config file %s: %w", absPath, err)
	}
	defer file.Close()

	var combinedLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "include ") {
			// Extract the path(s) after Include directive
			includePaths := strings.Fields(trimmed)[1:]
			for _, incPath := range includePaths {
				// Expand ~ to home directory if present
				if strings.HasPrefix(incPath, "~") {
					homeDir, err := os.UserHomeDir()
					if err == nil {
						incPath = filepath.Join(homeDir, incPath[1:])
					}
				}
				// Handle glob patterns
				matches, err := filepath.Glob(incPath)
				if err != nil {
					return "", fmt.Errorf("failed to glob include path %s: %w", incPath, err)
				}
				for _, match := range matches {
					includedContent, err := preprocessSSHConfig(match, visited)
					if err != nil {
						return "", err
					}
					combinedLines = append(combinedLines, includedContent)
				}
			}
		} else {
			combinedLines = append(combinedLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading ssh config file %s: %w", absPath, err)
	}

	return strings.Join(combinedLines, "\n"), nil
}
