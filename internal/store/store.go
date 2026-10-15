// Package store defines how rflib keeps its data, independent of the
// database: a Store holds libraries, and a Library holds one library's
// channels, radios, and saved searches. Package sqlite implements
// it; package storetest is the contract every implementation must pass.
//
// Stores check what a database can check: that records are well formed,
// names are unique within a library, and references stay inside their
// library. Rules that need more than one record, such as what a saved
// search may refer to, belong to package app.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

var (
	// ErrNotFound is returned (wrapped) when a lookup matches nothing in
	// the library.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned (wrapped) when a name is already taken in the
	// library.
	ErrConflict = errors.New("already exists")
)

type changeKey struct{}

type change struct{ reason, author string }

// WithChange attaches why a change is made, and by whom, to ctx; stores
// record it in shared channels' revisions.
func WithChange(ctx context.Context, reason, author string) context.Context {
	return context.WithValue(ctx, changeKey{}, change{reason, author})
}

// ChangeOf returns what WithChange attached to ctx.
func ChangeOf(ctx context.Context) (reason, author string) {
	c, _ := ctx.Value(changeKey{}).(change)
	return c.reason, c.author
}

// Store is a database of libraries.
type Store interface {
	// Migrate creates or upgrades the schema.
	Migrate(ctx context.Context) error
	// Ready reports an error unless the schema is current.
	Ready(ctx context.Context) error
	Close() error

	CreateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error)
	// GetLibrary looks a library up by ID or name; "" is the default
	// library.
	GetLibrary(ctx context.Context, idOrName string) (domain.Library, error)
	ListLibraries(ctx context.Context) ([]domain.Library, error)
	// UpdateLibrary saves a library's name and description. Setting
	// Default makes it the default library in place of the old one;
	// clearing it on the default library is refused, since there is always
	// one default.
	UpdateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error)
	// DeleteLibrary deletes a library and everything in it. The default
	// library can't be deleted.
	DeleteLibrary(ctx context.Context, id string) error

	// Library returns the library with the given ID. It doesn't check
	// that the library exists; look it up with GetLibrary first.
	Library(id string) Library
}

// Library is one library's data. Every lookup is confined to the library:
// a record in another library is not found.
type Library interface {
	ID() string
	// InTx runs fn in one transaction, committing when it returns nil. Only
	// the Library passed to fn may be used inside it.
	InTx(ctx context.Context, fn func(Library) error) error

	// CreateChannel stores a channel. A supplied ID is kept, so restored
	// backups keep their identity, unless another record already has it;
	// then a new one is assigned.
	CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error)
	GetChannel(ctx context.Context, id string) (domain.Channel, error)
	// ListChannels returns every channel in domain.CompareChannels order.
	// Links come with their shared channel's fields filled in (see
	// domain.Linked).
	ListChannels(ctx context.Context) ([]domain.Channel, error)
	// UpdateChannel saves a channel. A link saves only what's its own;
	// setting Link turns a channel into a link, and clearing it turns a
	// link into a private channel with the fields given.
	UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error)
	// DeleteChannel deletes a channel and takes it off every radio. A
	// shared channel that is linked can't be deleted (ErrConflict).
	DeleteChannel(ctx context.Context, id string) error
	// ChannelRevisions returns a shared channel's revisions, oldest
	// first; for a link, its shared channel's. Private channels have none.
	ChannelRevisions(ctx context.Context, id string) ([]domain.Revision, error)
	// SharedViews returns the library's views of shared channels, by ID:
	// each shared channel, under its own ID, with the library's annotation
	// (its tags, notes, and short names) laid over it. GetChannel returns
	// a view too, for a shared channel's ID, and UpdateChannel and
	// DeleteChannel save or delete the annotation. Radios hold shared
	// channels by their IDs.
	SharedViews(ctx context.Context, ids []string) (map[string]domain.Channel, error)
	// SaveAnnotation saves the library's annotation of c.Link.SharedID from
	// c's tags, notes, and short names; clearing them all removes it. It
	// returns the view.
	SaveAnnotation(ctx context.Context, c domain.Channel) (domain.Channel, error)
	// Follow makes the library follow a shared channel, seen at its current
	// revision, unless it already does; MarkSeen records a revision seen.
	// Annotating a shared channel or putting it on a radio follows it.
	Follow(ctx context.Context, sharedID string) error
	MarkSeen(ctx context.Context, sharedID string, revision int) error
	// ReplaceOnRadios puts newID in oldID's place on the library's radios,
	// keeping each entry's overrides; a radio holding both keeps newID's.
	ReplaceOnRadios(ctx context.Context, oldID, newID string) error
	// SeedChannels returns the library's channels that came from built-in
	// lists (see domain.SeedChannel); SetSeedChannel records or replaces
	// one. A record goes when its channel is deleted.
	SeedChannels(ctx context.Context) ([]domain.SeedChannel, error)
	SetSeedChannel(ctx context.Context, sc domain.SeedChannel) error
	// ChangedSince returns the library's channels with a revision recorded
	// after since, and the time of the latest one (since, when there are
	// none), to ask from next time. Only shared channels have revisions.
	ChangedSince(ctx context.Context, since time.Time) ([]domain.Channel, time.Time, error)

	CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error)
	// GetRadio looks a radio up by ID or name.
	GetRadio(ctx context.Context, idOrName string) (domain.Radio, error)
	ListRadios(ctx context.Context) ([]domain.Radio, error)
	UpdateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error)
	// DeleteRadio deletes a radio; its channels stay in the library.
	DeleteRadio(ctx context.Context, id string) error

	// RadioEntries returns a radio's channels in memory order, with the
	// radio's overrides for each.
	RadioEntries(ctx context.Context, radioID string) ([]domain.RadioEntry, error)
	// AddRadioChannels appends channels to the end of a radio's list in the
	// order given, skipping any already on it, and returns how many were
	// added.
	AddRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error)
	// AddSourceChannels adds channels for a radio source, skipping any
	// already on the radio. They go after the last channel the source
	// added; for a source with none yet, after the channels of earlier
	// sources and before those of later ones, else at the end. The source's
	// section start, when set, is kept on its first channel.
	AddSourceChannels(ctx context.Context, src domain.RadioSource, channelIDs []string) (int, error)
	// RemoveRadioChannels takes channels off a radio, closes up the list,
	// and returns how many were removed.
	RemoveRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error)
	// MoveRadioChannel moves a channel to index to (from 0) in the radio's
	// list; an index past the end moves it last. The channel no longer
	// belongs to the source that added it, as if added by hand.
	MoveRadioChannel(ctx context.Context, radioID, channelID string, to int) error
	// ReorderRadioChannels puts a radio's channels in the order given,
	// which must list each of them once. Section starts stay at their
	// places in the list; overrides and sources go with their channels.
	ReorderRadioChannels(ctx context.Context, radioID string, channelIDs []string) error
	// SetRadioEntry stores a radio's overrides for one of its channels: the
	// entry's SectionStart, Name, Power, and Skip.
	SetRadioEntry(ctx context.Context, radioID string, e domain.RadioEntry) error
	// RadioChannelIDs returns the channels on a radio, or on any radio when
	// radioID is empty.
	RadioChannelIDs(ctx context.Context, radioID string) (map[string]bool, error)

	// RadioSources returns a radio's sources in order.
	RadioSources(ctx context.Context, radioID string) ([]domain.RadioSource, error)
	// AllRadioSources returns every radio's sources.
	AllRadioSources(ctx context.Context) ([]domain.RadioSource, error)
	// AddRadioSource appends a source to its radio's sources.
	AddRadioSource(ctx context.Context, src domain.RadioSource) (domain.RadioSource, error)
	// UpdateRadioSource changes a source's query, sort, section start, and
	// KeepSorted.
	// A changed section start moves to the source's first channel now; a
	// removed one is taken off it, if it is still the source's.
	UpdateRadioSource(ctx context.Context, src domain.RadioSource) error
	// MoveRadioSource moves a source to index to (from 0). Channels already
	// on the radio stay where they are.
	MoveRadioSource(ctx context.Context, src domain.RadioSource, to int) error
	// RemoveRadioSource deletes a source. Its channels stay on the radio as
	// if added by hand, unless withChannels; it returns how many channels
	// were taken off.
	RemoveRadioSource(ctx context.Context, src domain.RadioSource, withChannels bool) (int, error)

	// Organizations, systems, and plans. Each change to one records a
	// revision (see RecordRevisions) with the reason in ctx. They refer only
	// to the library's own channels and organizations, and not to links;
	// package app keeps them to the shared library.

	CreateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error)
	// GetOrg looks an organization up by ID or name, ignoring case.
	GetOrg(ctx context.Context, idOrName string) (domain.Organization, error)
	ListOrgs(ctx context.Context) ([]domain.Organization, error)
	UpdateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error)
	// DeleteOrg deletes an organization attached to nothing
	// (ErrConflict otherwise).
	DeleteOrg(ctx context.Context, id string) error

	// CreateSystem stores a system with its networks, organizations, and
	// members.
	CreateSystem(ctx context.Context, s domain.System) (domain.System, error)
	// GetSystem looks a system up by ID or name, ignoring case. Members
	// come in their channels' name order.
	GetSystem(ctx context.Context, idOrName string) (domain.System, error)
	ListSystems(ctx context.Context) ([]domain.System, error)
	// UpdateSystem saves a system, replacing its lists.
	UpdateSystem(ctx context.Context, s domain.System) (domain.System, error)
	DeleteSystem(ctx context.Context, id string) error
	// SystemChannelIDs returns the channels in a system.
	SystemChannelIDs(ctx context.Context, systemID string) (map[string]bool, error)

	// CreatePlan stores a plan with its entries.
	CreatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error)
	// GetPlan looks a plan up by ID or name, ignoring case.
	GetPlan(ctx context.Context, idOrName string) (domain.Plan, error)
	ListPlans(ctx context.Context) ([]domain.Plan, error)
	// UpdatePlan saves a plan, replacing its entries. Plans are archived,
	// never deleted.
	UpdatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error)
	// PlanByID returns a plan by ID from any library: radio sources use
	// the shared library's plans.
	PlanByID(ctx context.Context, id string) (domain.Plan, error)

	// RecordRevisions returns an organization's, system's, or plan's
	// revisions, oldest first; kind is "organization", "system", or
	// "plan".
	RecordRevisions(ctx context.Context, kind, id string) ([]domain.RecordRevision, error)

	CreateSearch(ctx context.Context, s domain.SavedSearch) (domain.SavedSearch, error)
	// GetSearch looks a saved search up by ID or name; a leading @ is
	// ignored.
	GetSearch(ctx context.Context, idOrName string) (domain.SavedSearch, error)
	ListSearches(ctx context.Context) ([]domain.SavedSearch, error)
	UpdateSearch(ctx context.Context, s domain.SavedSearch) (domain.SavedSearch, error)
	DeleteSearch(ctx context.Context, id string) error
}
