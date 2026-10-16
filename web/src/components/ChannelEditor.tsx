import { Alert, Button, Divider, Drawer, Group, MultiSelect, SegmentedControl, Select, Stack, Switch, TagsInput, Text, Textarea, TextInput } from "@mantine/core";
import { IconLink } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, call, type Channel, type ChannelInput } from "../api/client";
import { useLib, useTags } from "../lib/library";
import { useOrgs } from "../lib/records";
import { failed, ok, warn } from "../lib/notify";
import { parseGrid } from "../lib/grid";
import { formatMHz, MODES, parseMHz, POWERS, SERVICES } from "../lib/radio";
import { History, NetworkLinks, ReasonModal, SharedChannelPicker } from "./Records";
import { ToneInput } from "./ToneInput";

type Form = {
  name: string;
  rx: string;
  tx: string;
  mode: string;
  service: string;
  rx_only: boolean;
  tx_tone: string;
  rx_tone: string;
  tone_squelch: boolean;
  power: string;
  bandwidth: string;
  skip: boolean;
  tags: string[];
  short_names: string[];
  notes: string;
  lat: string;
  lon: string;
  radius: string;
  networks: string[];
  operators: string[];
  partners: string[];
};

const blank: Form = {
  name: "", rx: "", tx: "", mode: "FM", service: "", rx_only: false, tx_tone: "none", rx_tone: "none", tone_squelch: false,
  power: "", bandwidth: "", skip: false, tags: [], short_names: [], notes: "", lat: "", lon: "", radius: "", networks: [],
  operators: [], partners: [],
};

function fromChannel(c: Channel): Form {
  return {
    name: c.name, rx: formatMHz(c.rx_hz), tx: c.tx_hz === null ? "" : formatMHz(c.tx_hz), mode: c.mode, service: c.service, rx_only: c.rx_only,
    tx_tone: c.tx_tone, rx_tone: c.rx_tone, tone_squelch: c.tone_squelch, power: c.power, bandwidth: c.bandwidth, skip: c.skip,
    tags: c.tags, short_names: c.short_names, notes: c.notes,
    lat: c.location ? String(c.location.latitude) : "", lon: c.location ? String(c.location.longitude) : "",
    radius: c.location?.radius_km != null ? String(c.location.radius_km) : "",
    networks: c.networks,
    operators: c.organizations.filter((o) => o.role === "operator").map((o) => o.org_id),
    partners: c.organizations.filter((o) => o.role === "partner").map((o) => o.org_id),
  };
}

/** toInput turns the form into API fields, or an error to show. A shared
 * channel sends only the user's annotation: tags, short names, and notes. */
function toInput(f: Form, linked: boolean): ChannelInput | string {
  if (linked) return { tags: f.tags, short_names: f.short_names, notes: f.notes };
  const rx = parseMHz(f.rx);
  if (rx === null) return "The receive frequency must be MHz, such as 146.52";
  let tx: number | null = null;
  if (f.tx.trim() !== "") {
    tx = parseMHz(f.tx);
    if (tx === null) return "The transmit frequency must be MHz, or empty for receive-only";
  }
  let location: ChannelInput["location"] = null;
  if (f.lat.trim() !== "" || f.lon.trim() !== "") {
    const latitude = Number(f.lat), longitude = Number(f.lon);
    if (!Number.isFinite(latitude) || !Number.isFinite(longitude) || f.lat.trim() === "" || f.lon.trim() === "") return "Give both latitude and longitude";
    location = { latitude, longitude, radius_km: f.radius.trim() === "" ? null : Number(f.radius) };
  }
  return {
    tags: f.tags, short_names: f.short_names, notes: f.notes, power: f.power, skip: f.skip, name: f.name, rx_hz: rx, tx_hz: tx, mode: f.mode, service: f.service, rx_only: f.rx_only,
    tx_tone: f.tx_tone, rx_tone: f.rx_tone, tone_squelch: f.tone_squelch, bandwidth: f.bandwidth, location, networks: f.networks,
  };
}

/** correction turns the form into a maintainer's correction of a shared
 * channel: its shared fields, curated tags, notes, organizations, and
 * nodes. Power, skip, and short names belong to each link. */
function correction(f: Form): ChannelInput | string {
  const body = toInput(f, false);
  if (typeof body === "string") return body;
  const { power: _power, skip: _skip, short_names: _short, ...rest } = body;
  return {
    ...rest,
    organizations: [...f.operators.map((org_id) => ({ org_id, role: "operator" as const })), ...f.partners.map((org_id) => ({ org_id, role: "partner" as const }))],
  };
}

/**
 * ChannelEditor creates a channel (channel null) or edits one. readOnly
 * shows a shared channel; with maintainer as well, it corrects one in the
 * shared list, asking for a reason.
 */
export function ChannelEditor(props: { channel: Channel | null; opened: boolean; onClose: () => void; readOnly?: boolean; maintainer?: boolean }) {
  const lib = useLib();
  const client = useQueryClient();
  const [form, setForm] = useState<Form>(blank);
  const c = props.channel;
  const linked = c?.link != null;
  const maintainer = props.maintainer === true && c !== null && !linked;
  const [asking, setAsking] = useState(false);
  const orgs = useOrgs();
  const tags = useTags(lib);
  const curated = useQuery({ queryKey: ["tags"], queryFn: () => call<{ tags: { name: string }[] }>(api.GET("/tags")), staleTime: Infinity });
  useEffect(() => setForm(c ? fromChannel(c) : blank), [c, props.opened]);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const [grid, setGrid] = useState("");
  // A grid square fills in its center, and a radius reaching its corners.
  const setGridSquare = (v: string) => {
    setGrid(v);
    const g = parseGrid(v);
    if (g) setForm((f) => ({ ...f, lat: String(g.latitude), lon: String(g.longitude), radius: String(g.radius_km) }));
  };
  const done = (warnings: string[]) => {
    warn(warnings);
    void client.invalidateQueries();
    props.onClose();
  };
  const correct = useMutation({
    mutationFn: async (reason: string) => {
      const body = correction(form);
      if (typeof body === "string") throw new Error(body);
      return call(api.PATCH("/libraries/{lib}/channels/{id}", { params: { path: { lib, id: c!.id } }, body: { ...body, reason } as ChannelInput }));
    },
    onSuccess: (r) => {
      ok("Corrected; links follow");
      setAsking(false);
      done(r.warnings);
    },
    onError: failed,
  });
  const save = useMutation({
    mutationFn: async () => {
      const body = toInput(form, linked);
      if (typeof body === "string") throw new Error(body);
      if (c) return call(api.PATCH("/libraries/{lib}/channels/{id}", { params: { path: { lib, id: c.id } }, body }));
      return call(api.POST("/libraries/{lib}/channels", { params: { path: { lib } }, body }));
    },
    onSuccess: (r) => {
      ok(c ? "Saved" : "Channel added");
      done(r.warnings);
    },
    onError: failed,
  });
  const copy = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/channels/copy", { params: { path: { lib } }, body: { select: { ids: [c!.id] } } })),
    onSuccess: () => {
      ok("It's a private copy now, and can be edited");
      done([]);
    },
    onError: failed,
  });
  // Shared fields are fixed on a link, and on a shared channel unless a
  // maintainer corrects it; own fields are fixed on any shared channel.
  const shared = linked || (props.readOnly && !maintainer);
  const ownFixed = props.readOnly;
  const orgData = (orgs.data?.organizations ?? []).map((o) => ({ value: o.id, label: o.name }));
  return (
    <Drawer opened={props.opened} onClose={props.onClose} position="right" size="lg" title={c ? c.name : "New channel"}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (maintainer) {
            const body = correction(form);
            if (typeof body === "string") failed(new Error(body));
            else setAsking(true);
          } else save.mutate();
        }}
      >
        <Stack gap="sm">
          {linked && (
            <Alert icon={<IconLink size={18} />} title="A shared channel">
              <Text size="sm">
                It follows the maintainers' corrections. Your tags, notes, and short names are yours; power and scan are set on each radio. To
                change the rest, make a private copy.
              </Text>
              <Button mt="xs" size="xs" variant="light" onClick={() => copy.mutate()} loading={copy.isPending}>
                Make a private copy
              </Button>
            </Alert>
          )}
          {c && c.status !== "active" && (
            <Alert color="yellow" title={c.status === "off-air" ? "Off the air" : "Retired"}>
              {[c.status_note, c.back_on && `expected back ${c.back_on}`].filter(Boolean).join("; ")}
            </Alert>
          )}
          <TextInput label="Name" required disabled={shared} value={form.name} onChange={(e) => set("name", e.currentTarget.value)} />
          <Group grow>
            <TextInput label="Receive MHz" required disabled={shared} value={form.rx} onChange={(e) => set("rx", e.currentTarget.value)} />
            <TextInput
              label="Transmit MHz"
              description="Empty for receive-only"
              disabled={shared}
              value={form.tx}
              onChange={(e) => set("tx", e.currentTarget.value)}
            />
          </Group>
          <Group grow>
            <Select label="Mode" disabled={shared} data={MODES} value={form.mode} allowDeselect={false} onChange={(v) => v && set("mode", v)} />
            <Select
              label="Service"
              disabled={shared}
              data={SERVICES.map((s) => ({ value: s, label: s || "unspecified" }))}
              value={form.service}
              allowDeselect={false}
              onChange={(v) => v !== null && set("service", v)}
            />
          </Group>
          <ToneInput label="Transmit tone" disabled={shared} value={form.tx_tone} onChange={(v) => set("tx_tone", v)} />
          <ToneInput label="Receive tone" disabled={shared} value={form.rx_tone} onChange={(v) => set("rx_tone", v)} />
          <Group>
            <Switch label="Tone squelch" disabled={shared} checked={form.tone_squelch} onChange={(e) => set("tone_squelch", e.currentTarget.checked)} />
            <Switch label="Receive only" disabled={shared} checked={form.rx_only} onChange={(e) => set("rx_only", e.currentTarget.checked)} />
            {!maintainer && !linked && (
              <Switch label="Skip in scans" disabled={ownFixed} checked={form.skip} onChange={(e) => set("skip", e.currentTarget.checked)} />
            )}
          </Group>
          <Group grow>
            {!maintainer && !linked && (
              <Select
                label="Power"
                disabled={ownFixed}
                data={POWERS.map((p) => ({ value: p, label: p || "radio's default" }))}
                value={form.power}
                allowDeselect={false}
                onChange={(v) => v !== null && set("power", v)}
              />
            )}
            <TextInput label="Bandwidth" disabled={shared} value={form.bandwidth} onChange={(e) => set("bandwidth", e.currentTarget.value)} />
          </Group>
          {maintainer ? (
            <MultiSelect
              label="Shared tags"
              description="From the curated list"
              data={curated.data?.tags.map((t) => t.name) ?? []}
              value={form.tags}
              onChange={(v) => set("tags", v)}
            />
          ) : (
            <TagsInput
              label="Tags"
              description="Pick a tag in use, or type a new one"
              disabled={ownFixed}
              data={tags}
              value={form.tags}
              onChange={(v) => set("tags", v)}
            />
          )}
          {linked && c?.link && c.link.tags.length > 0 && (
            <Text size="xs" c="dimmed">
              Shared tags: {c.link.tags.join(", ")}
            </Text>
          )}
          {!maintainer && (
            <TagsInput
              label="Short names"
              description="Your abbreviations for narrow radio displays"
              disabled={ownFixed}
              value={form.short_names}
              onChange={(v) => set("short_names", v)}
            />
          )}
          <Textarea label="Notes" autosize minRows={2} disabled={ownFixed && !maintainer} value={form.notes} onChange={(e) => set("notes", e.currentTarget.value)} />
          {linked && c?.link?.notes && (
            <Text size="xs" c="dimmed">
              Shared notes: {c.link.notes}
            </Text>
          )}
          <TagsInput
            label="Linking-network nodes"
            description="Such as allstar:43761 or echolink:12345"
            disabled={shared}
            value={form.networks}
            onChange={(v) => set("networks", v)}
          />
          {c && <NetworkLinks networks={c.networks} />}
          {maintainer ? (
            <Group grow>
              <MultiSelect label="Operators" data={orgData} searchable value={form.operators} onChange={(v) => set("operators", v)} />
              <MultiSelect label="Partners" data={orgData} searchable value={form.partners} onChange={(v) => set("partners", v)} />
            </Group>
          ) : (
            c &&
            c.organizations.length > 0 && <Text size="sm">Organizations: {c.organizations.map((o) => `${o.name} (${o.role})`).join(", ")}</Text>
          )}
          <Group grow align="flex-start">
            <TextInput
              label="Grid square"
              placeholder="CM94sq"
              disabled={shared}
              value={grid}
              onChange={(e) => setGridSquare(e.currentTarget.value)}
              error={gridError(grid)}
              description="Fills in the location"
            />
            <TextInput label="Latitude" disabled={shared} value={form.lat} onChange={(e) => set("lat", e.currentTarget.value)} />
            <TextInput label="Longitude" disabled={shared} value={form.lon} onChange={(e) => set("lon", e.currentTarget.value)} />
            <TextInput label="Radius km" disabled={shared} value={form.radius} onChange={(e) => set("radius", e.currentTarget.value)} />
          </Group>
          {maintainer && (
            <Group justify="flex-end" mt="md">
              <Button type="submit">Save correction</Button>
            </Group>
          )}
          {maintainer && <StatusEditor lib="shared" channel={c} onSaved={() => done([])} />}
          {!props.readOnly && (
            <Group justify="flex-end" mt="md">
              <Button type="submit" loading={save.isPending}>
                {c ? "Save" : "Add channel"}
              </Button>
            </Group>
          )}
        </Stack>
      </form>
      {c && !linked && !props.readOnly && <StatusEditor lib={lib} channel={c} onSaved={() => done([])} />}
      {c && props.opened && <ChannelHistory lib={lib} channel={c} />}
      <ReasonModal opened={asking} title={c ? `Correct ${c.name}` : ""} loading={correct.isPending} onClose={() => setAsking(false)} onConfirm={(r) => correct.mutate(r)} />
    </Drawer>
  );
}

/** StatusEditor takes a channel off the air, retires it, or brings it
 * back. Channels are retired, never deleted. In the shared list it's the
 * maintainer's: links follow, and the change is recorded with a reason. A
 * retired channel of your own can name a replacement, yours or shared,
 * and move over to it on your radios. */
function StatusEditor(props: { lib: string; channel: Channel; onSaved: () => void }) {
  const c = props.channel;
  const inShared = props.lib === "shared";
  const client = useQueryClient();
  const [status, setStatus] = useState(c.status);
  const [note, setNote] = useState(c.status_note);
  const [backOn, setBackOn] = useState(c.back_on ?? "");
  // undefined keeps the current replacement; null clears it.
  const [replacement, setReplacement] = useState<Channel | null | undefined>(undefined);
  const current = useQuery({
    queryKey: ["channel", props.lib, c.replaced_by],
    queryFn: () => call<Channel>(api.GET("/libraries/{lib}/channels/{id}", { params: { path: { lib: props.lib, id: c.replaced_by! } } })),
    enabled: c.replaced_by != null && c.replaced_by !== "",
  });
  const [asking, setAsking] = useState(false);
  useEffect(() => {
    setStatus(c.status);
    setNote(c.status_note);
    setBackOn(c.back_on ?? "");
    setReplacement(undefined);
  }, [c]);
  const shown = replacement === undefined ? (current.data ?? null) : replacement;
  const save = useMutation({
    mutationFn: (reason: string) =>
      call(
        api.POST("/libraries/{lib}/channels/{id}/status", {
          params: { path: { lib: props.lib, id: c.id } },
          body: {
            status,
            note,
            back_on: status === "off-air" && backOn ? backOn : null,
            replaced_by: status === "retired" ? (shown?.id ?? "") : "",
            reason,
          },
        }),
      ),
    onSuccess: (r) => {
      ok(inShared ? `${r.name} is ${r.status}; links follow` : `${r.name} is ${r.status}`);
      setAsking(false);
      props.onSaved();
    },
    onError: failed,
  });
  const relink = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/channels/relink", { params: { path: { lib: props.lib } }, body: { select: { ids: [c.id] } } })),
    onSuccess: (r) => {
      ok(r.relinked.length ? `Moved ${c.name} over to ${r.relinked[0].to.name} on your radios; program them again` : `${c.name} isn't on a radio`);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  return (
    <Stack gap="sm" mt="md">
      <Divider label={inShared ? "Maintainer: on the air?" : "On the air?"} />
      <SegmentedControl
        data={[
          { value: "active", label: "Active" },
          { value: "off-air", label: "Off the air" },
          { value: "retired", label: "Retired" },
        ]}
        value={status}
        onChange={(v) => setStatus(v as Channel["status"])}
      />
      {status !== "active" && (
        <TextInput label="Status note" description="What's happening, such as antenna work" value={note} onChange={(e) => setNote(e.currentTarget.value)} />
      )}
      {status === "off-air" && (
        <TextInput label="Expected back" type="date" value={backOn} onChange={(e) => setBackOn(e.currentTarget.value)} />
      )}
      {status === "retired" && (
        <>
          <SharedChannelPicker label="Replaced by" lib={props.lib} onPick={setReplacement} />
          {shown && (
            <Group gap="xs">
              <Text size="sm">
                Replaced by {shown.name} · {formatMHz(shown.rx_hz)}
              </Text>
              <Button size="compact-xs" variant="subtle" onClick={() => setReplacement(null)}>
                Clear
              </Button>
            </Group>
          )}
        </>
      )}
      {!inShared && status === "retired" && (
        <Text size="xs" c="dimmed">
          Retired channels aren't deleted: they stay in your library and on your radios, crossed out and not programmed, and are hidden from
          the channel list unless you show retired ones.
        </Text>
      )}
      <Group justify="flex-end">
        {!inShared && c.status === "retired" && c.replaced_by && (
          <Button variant="default" onClick={() => relink.mutate()} loading={relink.isPending}>
            Relink on radios
          </Button>
        )}
        <Button variant="light" onClick={() => (inShared ? setAsking(true) : save.mutate(""))} loading={!inShared && save.isPending}>
          Change status
        </Button>
      </Group>
      <ReasonModal opened={asking} title={`Status of ${c.name}`} loading={save.isPending} onClose={() => setAsking(false)} onConfirm={(r) => save.mutate(r)} />
    </Stack>
  );
}

/** gridError flags a complete-length grid square that isn't one; shorter
 * text may still be being typed. */
function gridError(v: string): string | undefined {
  const t = v.trim();
  return [4, 6, 8].includes(t.length) && !parseGrid(t) ? "Not a grid square" : undefined;
}

/** ChannelHistory shows a channel's revisions, newest first: your own
 * channel's changes, or a shared channel's corrections. */
function ChannelHistory(props: { lib: string; channel: Channel }) {
  const history = useQuery({
    queryKey: ["channel-history", props.lib, props.channel.id, props.channel.revision],
    queryFn: () => call(api.GET("/libraries/{lib}/channels/{id}/history", { params: { path: { lib: props.lib, id: props.channel.id } } })),
  });
  const revisions = (history.data?.revisions ?? []).map((r) => ({ number: r.revision, at: r.at, reason: r.reason, author: r.author, changes: r.changes }));
  if (revisions.length === 0) return null;
  return (
    <Stack gap="xs" mt="md">
      <Divider label="History" />
      <History revisions={revisions} />
    </Stack>
  );
}
