package main

import (
	"fmt"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
	"github.com/spf13/cobra"
)

func newNamesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "names", Short: "Preview how channel names fit a radio's display", Args: cobra.NoArgs,
		Long: `Preview how channel names fit a radio's display, and how each was produced:

  fits       the full name fits
  variant    one of the channel's short names fits (see channel --short-names)
  shortened  abbreviated, spaces or vowels removed, or words trimmed
  cut        characters cut after shortening wasn't enough

A "+" marks names given a suffix by --dedupe. Fix any name you don't like by
adding a short name to the channel. --profile takes the width, case, and
characters from an export profile instead of the flags.`}
	cmd.Flags().Int("max-len", 0, "Display width in characters (0 is unlimited)")
	cmd.Flags().Bool("upper", false, "Upper-case names")
	cmd.Flags().String("charset", "ascii", `Allowed characters: ascii, alnum, or a set such as "A-Z0-9 -/"`)
	cmd.Flags().String("profile", "", "Take display rules from this export profile")
	cmd.Flags().String("sort", "name", "Order: name or freq")
	dedupeFlag(cmd)
	filterFields(cmd)
	nameListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := nameListing.check(cmd); err != nil {
			return err
		}
		rules, err := nameRules(cmd)
		if err != nil {
			return err
		}
		dedupe, err := naming.ParseDedupe(getString(cmd, "dedupe"))
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := filteredChannels(cmd, a)
			if err != nil {
				return err
			}
			if err := domain.SortChannels(cs, getString(cmd, "sort")); err != nil {
				return err
			}
			_, results := naming.Apply(cs, rules, dedupe)
			rows := make([]nameRow, len(cs))
			for i, c := range cs {
				rows[i] = nameRow{c, results[i]}
			}
			if err := nameListing.write(cmd, rows); err != nil {
				return err
			}
			warnDuplicateNames(cmd, results)
			return nil
		})
	}
	return cmd
}

type nameRow struct {
	domain.Channel
	result naming.Result
}

var nameListing = listing[nameRow]{
	fields: []field[nameRow]{
		{name: "radio_name", text: func(r nameRow) string { return r.result.Name }},
		{name: "fit", text: func(r nameRow) string { return string(r.result.Method) + mark(r.result.Deduped, "+") }, json: func(r nameRow) any { return r.result.Method }},
		{name: "deduped", text: func(r nameRow) string { return yesNo(r.result.Deduped) }, json: func(r nameRow) any { return r.result.Deduped }},
		{name: "name", text: func(r nameRow) string { return r.Name }},
		{name: "id", id: true, text: func(r nameRow) string { return r.ID }},
	},
	defaults: []string{"radio_name", "fit", "name"},
}

func nameRules(cmd *cobra.Command) (naming.Rules, error) {
	if ref := getString(cmd, "profile"); ref != "" {
		e, err := profile.Find(ref)
		if err != nil {
			return naming.Rules{}, err
		}
		return e.Profile.NameRules(), nil
	}
	maxLen, _ := cmd.Flags().GetInt("max-len")
	if maxLen < 0 {
		return naming.Rules{}, fmt.Errorf("--max-len must not be negative")
	}
	upper, _ := cmd.Flags().GetBool("upper")
	cs, err := naming.ParseCharset(getString(cmd, "charset"))
	if err != nil {
		return naming.Rules{}, fmt.Errorf("--charset: %w", err)
	}
	return naming.Rules{MaxLen: maxLen, Upper: upper, Charset: cs}, nil
}
