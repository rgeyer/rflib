package importer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// DuplicatePolicy decides what happens when an imported channel matches an
// existing one, first by ID and then by every programmed field (see
// duplicateKey). Update therefore changes metadata such as power, notes,
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

type Options struct {
	OnDuplicate DuplicatePolicy
	// Group, when set, is an existing group ID or name that every imported
	// channel is assigned to, overriding the source's groups.
	Group string
	// Tags are added to every imported channel.
	Tags   []string
	DryRun bool
}

type Summary struct {
	Created, Updated, Skipped int
	GroupsCreated             []string
}

var errDryRun = errors.New("dry run")

// Apply writes a batch's records in a single transaction, so a failure
// leaves the library unchanged. With DryRun the same work runs and is then
// rolled back, so the summary is exact. Batch.Errors are the caller's
// concern; Apply only writes Records.
func Apply(ctx context.Context, s *store.Store, b Batch, opts Options) (Summary, error) {
	if opts.OnDuplicate == "" {
		opts.OnDuplicate = SkipDuplicates
	}
	var sum Summary
	err := s.InTx(ctx, func(tx *store.Store) error {
		a, err := newApplier(ctx, tx, opts)
		if err != nil {
			return err
		}
		for _, g := range b.Groups {
			if _, err := a.group(g); err != nil {
				return fmt.Errorf("group %q: %w", g.Name, err)
			}
		}
		for _, r := range b.Records {
			if err := a.channel(r.Channel); err != nil {
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
	ctx         context.Context
	tx          *store.Store
	opts        Options
	target      *domain.Group
	groupByID   map[string]domain.Group
	groupByName map[string]domain.Group
	chanByID    map[string]domain.Channel
	chanByKey   map[string]domain.Channel
	sum         Summary
}

func newApplier(ctx context.Context, tx *store.Store, opts Options) (*applier, error) {
	a := &applier{ctx: ctx, tx: tx, opts: opts, groupByID: map[string]domain.Group{}, groupByName: map[string]domain.Group{}, chanByID: map[string]domain.Channel{}, chanByKey: map[string]domain.Channel{}}
	groups, err := tx.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		a.addGroup(g)
	}
	if opts.Group != "" {
		g, err := tx.GetGroup(ctx, opts.Group)
		if err != nil {
			return nil, err
		}
		a.target = &g
	}
	channels, err := tx.ListChannels(ctx, store.ChannelFilter{})
	if err != nil {
		return nil, err
	}
	for _, c := range channels {
		a.addChannel(c)
	}
	return a, nil
}

func (a *applier) addGroup(g domain.Group) {
	a.groupByID[g.ID] = g
	a.groupByName[g.Name] = g
}

func (a *applier) addChannel(c domain.Channel) {
	a.chanByID[c.ID] = c
	a.chanByKey[duplicateKey(c)] = c
}

// duplicateKey covers everything programmed into a radio, so channels that
// only share a name and frequency (RadioReference lists often reuse short
// names across sites with different tones) are kept apart.
func duplicateKey(c domain.Channel) string {
	gid, tx := "", "rx-only"
	if c.GroupID != nil {
		gid = *c.GroupID
	}
	if c.TransmitHz != nil {
		tx = strconv.FormatInt(*c.TransmitHz, 10)
	}
	return strings.Join([]string{c.Name, strconv.FormatInt(c.ReceiveHz, 10), tx,
		domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), strconv.FormatBool(c.ToneSquelch), gid}, "\x00")
}

// group finds g by ID, then by name, creating it when neither exists.
func (a *applier) group(g domain.Group) (domain.Group, error) {
	if existing, ok := a.groupByID[g.ID]; ok && g.ID != "" {
		return existing, nil
	}
	if existing, ok := a.groupByName[g.Name]; ok && g.Name != "" {
		return existing, nil
	}
	if g.Name == "" {
		return g, fmt.Errorf("group %q does not exist and has no name to create it with", g.ID)
	}
	created, err := a.tx.CreateGroup(a.ctx, g)
	if err != nil {
		return g, err
	}
	a.addGroup(created)
	a.sum.GroupsCreated = append(a.sum.GroupsCreated, created.Name)
	return created, nil
}

func (a *applier) channel(c domain.Channel) error {
	switch {
	case a.target != nil:
		c.GroupID = &a.target.ID
	case c.Group != nil:
		g, err := a.group(*c.Group)
		if err != nil {
			return err
		}
		c.GroupID = &g.ID
	default:
		c.GroupID = nil
	}
	c.Group = nil
	c.Tags = append(c.Tags, a.opts.Tags...)

	match, found := a.chanByID[c.ID]
	if !found || c.ID == "" {
		match, found = a.chanByKey[duplicateKey(c)]
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
	return nil
}
