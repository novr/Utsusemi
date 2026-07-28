package main

import (
	"github.com/novr/utsusemi/internal/listing"
	"github.com/spf13/cobra"
)

func registerListCompletions(cmd *cobra.Command) {
	choices := listing.TargetChoices()
	args := make([]cobra.Completion, len(choices))
	for i, choice := range choices {
		args[i] = choice
	}
	cmd.ValidArgs = args
}
