package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Command groups for the root help listing. A flat alphabetical list of every
// top-level command tells a newcomer nothing about where to start, so the tree
// is grouped by the job being done the way kubectl and docker group theirs.
const (
	groupStart     = "start"
	groupInventory = "inventory"
	groupHistory   = "history"
	groupAnalysis  = "analysis"
	groupDiagnose  = "diagnose"
)

func registerCommandGroups(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Getting started:"},
		&cobra.Group{ID: groupInventory, Title: "Inventory and search:"},
		&cobra.Group{ID: groupHistory, Title: "Assessment and history:"},
		&cobra.Group{ID: groupAnalysis, Title: "Analysis:"},
		&cobra.Group{ID: groupDiagnose, Title: "Diagnostics:"},
	)
}

// inGroup files a command under a root help group.
func inGroup(id string, cmd *cobra.Command) *cobra.Command {
	cmd.GroupID = id
	return cmd
}

// topologyKinds are the subjects the topology, dependencies and blast-radius
// queries accept. Listing them in help and in ValidArgs is the difference
// between "KIND NAME" being a shape and being an instruction.
var topologyKinds = []string{"vm", "template", "host", "cluster", "datastore", "network", "dvswitch", "resourcepool"}

// unknownSubcommandError reports a mistyped command the way a user can act on:
// the name that failed, the closest matches, and where to look for the rest.
//
// Cobra builds this message itself for the default argument validators, but
// not for cobra.NoArgs and not at all for a parent command, so the two places
// a typo is most likely are the two places nothing is offered. Note that
// SuggestionsFor reads SuggestionsMinimumDistance without defaulting it —
// only the unexported findSuggestions does that — so it is set here.
func unknownSubcommandError(cmd *cobra.Command, arg string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown command %q for %q", arg, cmd.CommandPath())
	if !cmd.DisableSuggestions {
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		if suggestions := cmd.SuggestionsFor(arg); len(suggestions) > 0 {
			b.WriteString("\n\nDid you mean this?\n")
			for _, s := range suggestions {
				fmt.Fprintf(&b, "\t%s\n", s)
			}
		}
	}
	fmt.Fprintf(&b, "\nRun '%s --help' for the available commands.", cmd.CommandPath())
	return errors.New(b.String())
}

// requireSubcommand makes a parent command reject an unknown subcommand rather
// than printing its own help and reporting success.
//
// Cobra's default for a command that has children but no RunE is to accept any
// argument, fall back to Help() and exit 0, so "vsfleet assessment lst" looks
// to a script exactly like a capture that worked. The check has to live in
// RunE rather than Args because execute() returns flag.ErrHelp for an
// unrunnable command before it ever validates arguments.
//
// A bare parent still prints its help and succeeds: asking what is under
// "vsfleet assessment" is a reasonable thing to do, and answering it is not a
// failure.
func requireSubcommand(cmd *cobra.Command) *cobra.Command {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return c.Help()
		}
		return unknownSubcommandError(c, args[0])
	}
	cmd.SetUsageTemplate(parentUsageTemplate)
	return cmd
}

// parentUsageTemplate is cobra's default with one change: the "[flags]" usage
// line is shown only for a command that has no subcommands.
//
// Giving a parent a RunE makes cobra consider it runnable, which would
// otherwise advertise "vsfleet assessment [flags]" — an invocation that does
// nothing but print this same help. Cobra resolves a template through the
// parent chain, so the descendants of a parent inherit this one; the extra
// condition is what keeps it correct for them, since a leaf has no subcommands
// and still prints its usage line. Root keeps the default template, where the
// line is true: "vsfleet --refresh 5m" really does open the interface.
const parentUsageTemplate = `Usage:{{if and .Runnable (not .HasAvailableSubCommands)}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// rejectUnknownArgs is cobra.NoArgs with suggestions attached, for the root
// command. Root cannot use the default validator — a bare "vsfleet" opens the
// terminal interface, so its arguments have to stay rejected — but that choice
// should not also cost the user "Did you mean status?".
func rejectUnknownArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return unknownSubcommandError(cmd, args[0])
}
