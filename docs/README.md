# rflib documentation

New to rflib? Start with the [README](../README.md): what rflib is for, installing it, and a quick start from nothing to a programmed radio. These pages cover each part in detail. Commands are shown as `./rflib` from a Unix shell; on Windows, use `.\rflib.exe`.

**Before you start:** [Known limitations](limitations.md).

## Channels and the shared list

- [Your library](library.md): where your library is kept, how to back it up, and how it relates to the shared list.
- [Channels](channels.md): adding and changing channels, tones, locations, services, receive-only channels, band plan checks, and changing many at once.
- [Shared channels](shared-channels.md): the shared list your radios draw from, your notes on its channels, and following its changes. Channels are retired, never deleted.
- [Search](search.md): the query language every command and the web interface use, and saved searches.
- [Built-in channel lists](built-in-lists.md): the lists the shared list starts with, and keeping them up to date.
- [Organizations, systems, and plans](organizations-systems-plans.md): who runs a repeater, linked systems, and communications plans laid out as ICS 205.

## Radios

- [Radios](radios.md): building a radio, drafts, sections and memory numbers, per-radio settings, where radios may transmit, and keeping a radio up to date from searches and plans.
- [Radio models](radio-models.md): the radios rflib knows, using `chirp-generic` for others, and making or installing a model file.
- [Revisions, copies, and comparing](radio-history.md): what was last programmed, comparing a radio with its file or with another radio, and copying a radio to another model.
- [Power](power.md): power as low, medium, high, or max, and how each radio maps it.
- [Names on radio displays](names.md): how rflib fits names to a radio's display, and your own short names.

## Files

- [Import and export](import-export.md): backups, CSV, and CHIRP files, in and out.
- [Export profiles](export-profiles.md): manufacturer software formats, such as Yaesu ADMS, and writing your own.

## Running rflib

- [Web interface](web-interface.md): what each page does.
- [Accounts and hosting](accounts.md): hosting rflib for a club, with an account for each person.
- [HTTP API](api.md): scripting rflib over HTTP.
- [Development](development.md): building rflib, the checks, making a release, and maintaining the built-in data.
