package main

import (
	"strings"

	"github.com/dimonomid/ssh_config"
)

// filterSSHConfigByMatch filters the parsed ssh_config.Config to only include hosts
// that match the current environment based on Match directives.
// This is a simplified implementation that removes hosts that are part of Match blocks
// that do not apply to the current environment.
// The current environment parameters can be extended as needed.
func filterSSHConfigByMatch(cfg *ssh_config.Config, currentHost string, currentUser string) *ssh_config.Config {
	if cfg == nil {
		return nil
	}

	filteredHosts := make([]*ssh_config.Host, 0, len(cfg.Hosts))

	for _, host := range cfg.Hosts {
		if len(host.Patterns) == 0 {
			continue
		}

		// Check if this host is inside a Match block and if it matches the current environment.
		// The ssh_config library does not expose Match blocks directly, so this is a heuristic:
		// if the host has a "Match" option set, we check if it matches currentHost/currentUser.
		// For now, we skip hosts with patterns containing wildcards as they are ignored anyway.

		// This is a placeholder for actual Match evaluation logic.
		// For now, we include all hosts unconditionally.
		// TODO: Implement actual Match condition evaluation.

		filteredHosts = append(filteredHosts, host)
	}

	return &ssh_config.Config{
		Hosts: filteredHosts,
	}
}
