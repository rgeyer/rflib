package importer

import (
	"context"
	"errors"
	"fmt"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// DuplicatePolicy decides what happens when an imported channel matches an
// existing one, first by ID and then by the Match rule (see duplicateKey). Update therefore changes metadata such as power, notes,
// skip, and tags; a changed tone or frequency makes a new channel.
type DuplicatePolicy string

const (
	SkipDuplicates   DuplicatePolicy = "skip"
	UpdateDuplicates DuplicatePolicy = "update"
	CreateDuplicates DuplicatePolicy = "create"
)

func ParseDuplicatePolicy(v string) (DuplicatePolicy, error) {
	switch p := DuplicatePolicy(v); p {
	case SkipDuplicates, UpdateDuplicates, CreateDuplicates:
		return p, nil
	}
	return "", fmt.Errorf("unknown duplicate policy %q; use skip, update, or create", v)
}

// MatchRule chooses which fields make two channels duplicates.
type MatchRule string

const (
	// MatchExact compares the name, frequencies, tones, and tone squelch.
	MatchExact MatchRule = "exact"
	// MatchProgrammed compares only what reaches the air: frequencies,
	// tones, and tone squelch. Use it for files from a radio, where names
	// are shortened.
	MatchProgrammed MatchRule = "programmed"
)

func ParseMatchRule(v string) (MatchRule, error) {
	switch m := MatchRule(v); m {
	case MatchExact, MatchProgrammed:
		return m, nil
	}
	return "", fmt.Errorf("unknown match rule %q; use exact or programmed", v)
}

type Options struct {
	OnDuplicate DuplicatePolicy
	// Match defaults to MatchExact.
	Match MatchRule
	// Tags are added to every imported channel.
	Tags []string
	// Service, when set, replaces every imported channel's service.
	Service domain.Service
	// RXOnly, when set, replaces every imported channel's receive-only flag.
	// When nil and Service is set, the service's default applies (see
	// domain.DefaultRXOnly).
	RXOnly *bool
	DryRun bool
}

type Summary struct {
	Created, Updated, Skipped int
	// Warnings are band-plan problems in written channels (see
	// domain.CheckBandPlan). They don't stop the import.
	Warnings []RowError
}

var errDryRun = errors.New("dry run")

// Apply writes a batch's records in a single transaction, so a failure
// leaves the library unchanged. With DryRun the same work runs and is then
// rolled back, so the summary is exact. Batch.Errors are the caller's
// concern; Apply only writes Records.
func Apply(ctx context.Context, s store.Library, b Batch, opts Options) (Summary, error) {
	if opts.OnDuplicate == "" {
		opts.OnDuplicate = SkipDuplicates
	}
	if opts.Match == "" {
		opts.Match = MatchExact
	}
	var sum Summary
	err := s.InTx(ctx, func(tx store.Library) error {
		a, err := newApplier(ctx, tx, opts)
		if err != nil {
			return err
		}
		for _, r := range b.Records {
			if err := a.channel(r); err != nil {
				return RowError{Row: r.Row, Err: err}
			}
		}
		sum = a.sum
		if opts.DryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return sum, err
}

type applier struct {
	ctx       context.Context
	tx        store.Library
	opts      Options
	chanByID  map[string]domain.Channel
	chanByKey map[string]domain.Channel
	sum       Summary
}

func newApplier(ctx context.Context, tx store.Library, opts Options) (*applier, error) {
	a := &applier{ctx: ctx, tx: tx, opts: opts, chanByID: map[string]domain.Channel{}, chanByKey: map[string]domain.Channel{}}
	channels, err := tx.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range channels {
		a.addChannel(c)
	}
	return a, nil
}

// addChannel indexes c. When another channel already has c's key, the
// first one keeps it, so matches don't depend on what was imported last.
func (a *applier) addChannel(c domain.Channel) {
	a.chanByID[c.ID] = c
	k := duplicateKey(c, a.opts.Match)
	if existing, ok := a.chanByKey[k]; !ok || existing.ID == c.ID {
		a.chanByKey[k] = c
	}
}

// duplicateKey covers everything programmed into a radio, so channels that
// only share a name and frequency (RadioReference lists often reuse short
// names across sites with different tones) are kept apart. MatchProgrammed
// leaves out the name.
func duplicateKey(c domain.Channel, m MatchRule) string {
	name := c.Name
	if m == MatchProgrammed {
		name = ""
	}
	return name + "\x00" + domain.ProgrammedKey(c)
}

func (a *applier) channel(r Record) error {
	c := r.Channel
	if a.opts.Service != domain.ServiceUnspecified {
		c.Service = a.opts.Service
		c.RXOnly = c.RXOnly || domain.DefaultRXOnly(c.Service)
	}
	if a.opts.RXOnly != nil {
		c.RXOnly = *a.opts.RXOnly
	}
	c.Tags = append(c.Tags, a.opts.Tags...)

	match, found := a.chanByID[c.ID]
	if !found || c.ID == "" {
		match, found = a.chanByKey[duplicateKey(c, a.opts.Match)]
	}
	if found {
		switch a.opts.OnDuplicate {
		case SkipDuplicates:
			a.sum.Skipped++
			return nil
		case UpdateDuplicates:
			c.ID, c.CreatedAt = match.ID, match.CreatedAt
			updated, err := a.tx.UpdateChannel(a.ctx, c)
			if err != nil {
				return err
			}
			a.addChannel(updated)
			a.sum.Updated++
			a.warn(r.Row, updated)
			return nil
		}
	}
	if _, taken := a.chanByID[c.ID]; taken {
		c.ID = ""
	}
	created, err := a.tx.CreateChannel(a.ctx, c)
	if err != nil {
		return err
	}
	a.addChannel(created)
	a.sum.Created++
	a.warn(r.Row, created)
	return nil
}

func (a *applier) warn(row int, c domain.Channel) {
	for _, w := range domain.CheckBandPlan(c) {
		a.sum.Warnings = append(a.sum.Warnings, RowError{Row: row, Err: fmt.Errorf("%s: %s", c.Name, w)})
	}
}
