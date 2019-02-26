package cmd

import (
	"os"

	"github.com/lyraproj/lyra/cmd/goplugin-identity/identity"
	"github.com/lyraproj/lyra/pkg/logger"
	"github.com/lyraproj/puppet-workflow/puppet"
	"github.com/spf13/cobra"
)

// EmbeddedPluginCmd runs embedded plugins
func EmbeddedPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "plugin",
		Hidden: true,
		Run:    startPlugin,
		Args:   cobra.ExactArgs(1),
	}

	cmd.SetHelpTemplate(cmd.HelpTemplate())

	return cmd
}

func startPlugin(cmd *cobra.Command, args []string) {
	name := args[0]
	switch name {
	case "identity":
		identity.Start("identity.db")
	case "puppet":
		puppet.Start(`Puppet`)
	default:
		logger.Get().Error("Unknown embedded plugin", "name", name)
		os.Exit(1)
	}
}
