package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/store"
)

const usage = `rflib manages a local SQLite library of analog radio channels.

Usage:
  rflib <command> [options]

Commands:
  init --db PATH
  group add|list|show|update|delete
  channel add|list|show|update|delete
  export csv --db PATH --output PATH [filters]

Run "rflib <command> --help" for command-specific options.`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(args []string, out, errOut io.Writer) error {
	cmd := newRootCmd(out, errOut)
	cmd.SetArgs(args)
	return cmd.Execute()
}
func open(args []string, name string) (*flag.FlagSet, *string, *store.Store, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "SQLite library path (required)")
	return fs, dbPath, nil, nil
}
func initCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *db == "" {
		return fmt.Errorf("--db is required")
	}
	s, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.Migrate(context.Background()); err != nil {
		return err
	}
	fmt.Fprintln(out, "Library initialized:", *db)
	return nil
}
func withStore(dbPath string, fn func(*store.Store) error) error {
	if dbPath == "" {
		return fmt.Errorf("--db is required")
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.Ready(context.Background()); err != nil {
		return err
	}
	return fn(s)
}
func groupCmd(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("group requires add, list, show, update, or delete")
	}
	switch args[0] {
	case "add":
		return groupAdd(args[1:], out)
	case "list":
		return groupList(args[1:], out)
	case "show":
		return groupShow(args[1:], out)
	case "update":
		return groupUpdate(args[1:], out)
	case "delete":
		return groupDelete(args[1:], out)
	default:
		return fmt.Errorf("unknown group command %q", args[0])
	}
}
func groupFlags(fs *flag.FlagSet) (db, name, desc, lat, lon, radius, notes *string) {
	db = fs.String("db", "", "SQLite library path (required)")
	name = fs.String("name", "", "group name")
	desc = fs.String("description", "", "description")
	lat = fs.String("lat", "", "latitude")
	lon = fs.String("lon", "", "longitude")
	radius = fs.String("radius-km", "", "radius in kilometers")
	notes = fs.String("notes", "", "notes")
	return
}
func groupFromFlags(id string, name, desc, lat, lon, radius, notes string) (domain.Group, error) {
	g := domain.Group{ID: id, Name: name, Description: desc, Notes: notes}
	var err error
	if lat != "" {
		v, e := strconv.ParseFloat(lat, 64)
		if e != nil {
			return g, fmt.Errorf("parse latitude: %w", e)
		}
		g.Latitude = &v
	}
	if lon != "" {
		v, e := strconv.ParseFloat(lon, 64)
		if e != nil {
			return g, fmt.Errorf("parse longitude: %w", e)
		}
		g.Longitude = &v
	}
	if radius != "" {
		v, e := strconv.ParseFloat(radius, 64)
		if e != nil {
			return g, fmt.Errorf("parse radius: %w", e)
		}
		g.RadiusKM = &v
	}
	return g, err
}
func groupAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("group add", flag.ContinueOnError)
	fs.SetOutput(out)
	db, n, d, lat, lon, r, notes := groupFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	g, err := groupFromFlags("", *n, *d, *lat, *lon, *r, *notes)
	if err != nil {
		return err
	}
	return withStore(*db, func(s *store.Store) error {
		g, err = s.CreateGroup(context.Background(), g)
		if err == nil {
			fmt.Fprintln(out, g.ID)
		}
		return err
	})
}
func groupList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("group list", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withStore(*db, func(s *store.Store) error {
		gs, e := s.ListGroups(context.Background())
		if e != nil {
			return e
		}
		for _, g := range gs {
			fmt.Fprintf(out, "%s\t%s\t%s\n", g.ID, g.Name, formatCoordinates(g))
		}
		return nil
	})
}
func groupShow(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("group show", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	id := fs.String("id", "", "group ID or name (required)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*db, func(s *store.Store) error {
		g, e := s.GetGroup(context.Background(), *id)
		if e != nil {
			return e
		}
		printGroup(out, g)
		return nil
	})
}
func groupUpdate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("group update", flag.ContinueOnError)
	fs.SetOutput(out)
	db, n, d, lat, lon, r, notes := groupFlags(fs)
	id := fs.String("id", "", "group ID or name (required)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*db, func(s *store.Store) error {
		old, e := s.GetGroup(context.Background(), *id)
		if e != nil {
			return e
		}
		g, e := groupFromFlags(old.ID, choose(*n, old.Name), choose(*d, old.Description), choose(*lat, optionalFloat(old.Latitude)), choose(*lon, optionalFloat(old.Longitude)), choose(*r, optionalFloat(old.RadiusKM)), choose(*notes, old.Notes))
		if e != nil {
			return e
		}
		_, e = s.UpdateGroup(context.Background(), g)
		return e
	})
}
func groupDelete(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("group delete", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	id := fs.String("id", "", "group ID (required)")
	yes := fs.Bool("yes", false, "confirm deletion")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if !*yes {
		return fmt.Errorf("group deletion requires --yes")
	}
	return withStore(*db, func(s *store.Store) error { return s.DeleteGroup(context.Background(), *id) })
}

func channelCmd(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("channel requires add, list, show, update, or delete")
	}
	switch args[0] {
	case "add":
		return channelAdd(args[1:], out)
	case "list":
		return channelList(args[1:], out)
	case "show":
		return channelShow(args[1:], out)
	case "update":
		return channelUpdate(args[1:], out)
	case "delete":
		return channelDelete(args[1:], out)
	default:
		return fmt.Errorf("unknown channel command %q", args[0])
	}
}

type channelValues struct {
	db, name, group, rx, tx, mode, rxTone, txTone, power, bw, notes, tags *string
	toneSQL, skip                                                         *bool
}

func channelFlags(fs *flag.FlagSet) channelValues {
	return channelValues{fs.String("db", "", "SQLite library path (required)"), fs.String("name", "", "channel name"), fs.String("group", "", "group ID or name"), fs.String("rx", "", "receive frequency MHz"), fs.String("tx", "", "transmit frequency MHz; omit for receive-only"), fs.String("mode", "FM", "FM, NFM, or AM"), fs.String("rx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]"), fs.String("tx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]"), fs.String("power", "", "power label"), fs.String("bandwidth", "", "bandwidth label"), fs.String("notes", "", "notes"), fs.String("tags", "", "comma-separated tags"), fs.Bool("tone-squelch", false, "enable receive tone squelch"), fs.Bool("skip", false, "skip when scanning")}
}
func channelFromValues(id string, v channelValues, old *domain.Channel, s *store.Store) (domain.Channel, error) {
	c := domain.Channel{ID: id}
	if old != nil {
		c = *old
	}
	c.Name = choose(*v.name, c.Name)
	c.Mode = choose(*v.mode, c.Mode)
	c.Power = choose(*v.power, c.Power)
	c.Bandwidth = choose(*v.bw, c.Bandwidth)
	c.Notes = choose(*v.notes, c.Notes)
	if *v.rx != "" {
		x, e := domain.ParseFrequencyMHz(*v.rx)
		if e != nil {
			return c, e
		}
		c.ReceiveHz = x
	}
	if *v.tx != "" {
		x, e := domain.ParseFrequencyMHz(*v.tx)
		if e != nil {
			return c, e
		}
		c.TransmitHz = &x
	}
	rt, e := domain.ParseTone(*v.rxTone)
	if e != nil {
		return c, e
	}
	tt, e := domain.ParseTone(*v.txTone)
	if e != nil {
		return c, e
	}
	c.ReceiveTone = rt
	c.TransmitTone = tt
	c.ToneSquelch = *v.toneSQL
	c.Skip = *v.skip
	if *v.tags != "" {
		c.Tags = strings.Split(*v.tags, ",")
	}
	if *v.group != "" {
		g, e := s.GetGroup(context.Background(), *v.group)
		if e != nil {
			return c, fmt.Errorf("find group: %w", e)
		}
		c.GroupID = &g.ID
	}
	return c, nil
}
func channelAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel add", flag.ContinueOnError)
	fs.SetOutput(out)
	v := channelFlags(fs)
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*v.db, func(s *store.Store) error {
		c, e := channelFromValues("", v, nil, s)
		if e != nil {
			return e
		}
		c, e = s.CreateChannel(context.Background(), c)
		if e == nil {
			fmt.Fprintln(out, c.ID)
		}
		return e
	})
}
func channelList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel list", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	f := filterFlags(fs)
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*db, func(s *store.Store) error {
		filter, e := parseFilter(f)
		if e != nil {
			return e
		}
		cs, e := s.ListChannels(context.Background(), filter)
		if e != nil {
			return e
		}
		for _, c := range cs {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), groupName(c))
		}
		return nil
	})
}
func channelShow(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel show", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	id := fs.String("id", "", "channel ID (required)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*db, func(s *store.Store) error {
		c, e := s.GetChannel(context.Background(), *id)
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "%s\nname: %s\nrx: %s MHz\ntx: %s MHz\nmode: %s\nreceive tone: %s\ntransmit tone: %s\ntags: %s\n", c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), optionalFreq(c.TransmitHz), c.Mode, domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), strings.Join(c.Tags, ","))
		return nil
	})
}
func channelUpdate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel update", flag.ContinueOnError)
	fs.SetOutput(out)
	v := channelFlags(fs)
	id := fs.String("id", "", "channel ID (required)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	return withStore(*v.db, func(s *store.Store) error {
		old, e := s.GetChannel(context.Background(), *id)
		if e != nil {
			return e
		}
		c, e := channelFromValues(old.ID, v, &old, s)
		if e != nil {
			return e
		}
		_, e = s.UpdateChannel(context.Background(), c)
		return e
	})
}
func channelDelete(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel delete", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	id := fs.String("id", "", "channel ID (required)")
	yes := fs.Bool("yes", false, "confirm deletion")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if !*yes {
		return fmt.Errorf("channel deletion requires --yes")
	}
	return withStore(*db, func(s *store.Store) error { return s.DeleteChannel(context.Background(), *id) })
}

type filterValues struct{ group, tag, lat, lon, radius *string }

func filterFlags(fs *flag.FlagSet) filterValues {
	return filterValues{fs.String("group", "", "group ID"), fs.String("tag", "", "tag"), fs.String("lat", "", "latitude"), fs.String("lon", "", "longitude"), fs.String("radius-km", "", "radius in kilometers")}
}
func parseFilter(v filterValues) (store.ChannelFilter, error) {
	f := store.ChannelFilter{GroupID: *v.group, Tag: *v.tag}
	for _, x := range []struct {
		s string
		p **float64
	}{{*v.lat, &f.Latitude}, {*v.lon, &f.Longitude}, {*v.radius, &f.RadiusKM}} {
		if x.s != "" {
			n, e := strconv.ParseFloat(x.s, 64)
			if e != nil {
				return f, e
			}
			*x.p = &n
		}
	}
	return f, nil
}
func exportCmd(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "csv" {
		return fmt.Errorf("export requires csv")
	}
	fs := flag.NewFlagSet("export csv", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "SQLite library path (required)")
	output := fs.String("output", "", "output CSV path (required)")
	f := filterFlags(fs)
	if e := fs.Parse(args[1:]); e != nil {
		return e
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	return withStore(*db, func(s *store.Store) error {
		filter, e := parseFilter(f)
		if e != nil {
			return e
		}
		cs, e := s.ListChannels(context.Background(), filter)
		if e != nil {
			return e
		}
		file, e := os.Create(*output)
		if e != nil {
			return e
		}
		defer file.Close()
		if e = (export.GenericCSV{}).Export(file, cs); e != nil {
			return e
		}
		fmt.Fprintln(out, "Exported", len(cs), "channels to", *output)
		return nil
	})
}
func choose(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
func optionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 6, 64)
}
func optionalFreq(v *int64) string {
	if v == nil {
		return ""
	}
	return domain.FormatFrequencyMHz(*v)
}
func formatCoordinates(g domain.Group) string {
	if g.Latitude == nil {
		return ""
	}
	return optionalFloat(g.Latitude) + "," + optionalFloat(g.Longitude)
}
func groupName(c domain.Channel) string {
	if c.Group == nil {
		return ""
	}
	return c.Group.Name
}
func printGroup(out io.Writer, g domain.Group) {
	fmt.Fprintf(out, "%s\nname: %s\ncoordinates: %s\ndescription: %s\nnotes: %s\n", g.ID, g.Name, formatCoordinates(g), g.Description, g.Notes)
}

var _ = sql.ErrNoRows
var _ = errors.Is
