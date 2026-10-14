import { Alert, Button, Drawer, Group, Select, Stack, Switch, TagsInput, Text, Textarea, TextInput } from "@mantine/core";
import { IconLink } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, call, type Channel, type ChannelInput } from "../api/client";
import { useLib } from "../lib/library";
import { failed, ok, warn } from "../lib/notify";
import { formatMHz, MODES, parseMHz, POWERS, SERVICES } from "../lib/radio";
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
};

const blank: Form = {
  name: "", rx: "", tx: "", mode: "FM", service: "", rx_only: false, tx_tone: "none", rx_tone: "none", tone_squelch: false,
  power: "", bandwidth: "", skip: false, tags: [], short_names: [], notes: "", lat: "", lon: "", radius: "",
};

function fromChannel(c: Channel): Form {
  return {
    name: c.name, rx: formatMHz(c.rx_hz), tx: c.tx_hz === null ? "" : formatMHz(c.tx_hz), mode: c.mode, service: c.service, rx_only: c.rx_only,
    tx_tone: c.tx_tone, rx_tone: c.rx_tone, tone_squelch: c.tone_squelch, power: c.power, bandwidth: c.bandwidth, skip: c.skip,
    tags: c.tags, short_names: c.short_names, notes: c.notes,
    lat: c.location ? String(c.location.latitude) : "", lon: c.location ? String(c.location.longitude) : "",
    radius: c.location?.radius_km != null ? String(c.location.radius_km) : "",
  };
}

/** toInput turns the form into API fields, or an error to show. A link
 * sends only what's its own. */
function toInput(f: Form, linked: boolean): ChannelInput | string {
  const own: ChannelInput = { tags: f.tags, short_names: f.short_names, notes: f.notes, power: f.power, skip: f.skip };
  if (linked) return own;
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
    ...own, name: f.name, rx_hz: rx, tx_hz: tx, mode: f.mode, service: f.service, rx_only: f.rx_only,
    tx_tone: f.tx_tone, rx_tone: f.rx_tone, tone_squelch: f.tone_squelch, bandwidth: f.bandwidth, location,
  };
}

/** ChannelEditor creates a channel (channel null) or edits one. */
export function ChannelEditor(props: { channel: Channel | null; opened: boolean; onClose: () => void; readOnly?: boolean }) {
  const lib = useLib();
  const client = useQueryClient();
  const [form, setForm] = useState<Form>(blank);
  const c = props.channel;
  const linked = c?.link != null;
  useEffect(() => setForm(c ? fromChannel(c) : blank), [c, props.opened]);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const done = (warnings: string[]) => {
    warn(warnings);
    void client.invalidateQueries();
    props.onClose();
  };
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
  const remove = useMutation({
    mutationFn: () => call(api.DELETE("/libraries/{lib}/channels/{id}", { params: { path: { lib, id: c!.id } } })),
    onSuccess: () => {
      ok("Deleted");
      done([]);
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
  const shared = linked || props.readOnly;
  return (
    <Drawer opened={props.opened} onClose={props.onClose} position="right" size="lg" title={c ? c.name : "New channel"}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Stack gap="sm">
          {linked && (
            <Alert icon={<IconLink size={18} />} title="Linked to a shared channel">
              <Text size="sm">
                It follows the shared channel's corrections. Your tags, notes, power, skip, and short names are yours; to change the rest,
                make it a private copy.
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
            <Switch label="Skip in scans" disabled={props.readOnly} checked={form.skip} onChange={(e) => set("skip", e.currentTarget.checked)} />
          </Group>
          <Group grow>
            <Select
              label="Power"
              disabled={props.readOnly}
              data={POWERS.map((p) => ({ value: p, label: p || "radio's default" }))}
              value={form.power}
              allowDeselect={false}
              onChange={(v) => v !== null && set("power", v)}
            />
            <TextInput label="Bandwidth" disabled={shared} value={form.bandwidth} onChange={(e) => set("bandwidth", e.currentTarget.value)} />
          </Group>
          <TagsInput label="Tags" disabled={props.readOnly} value={form.tags} onChange={(v) => set("tags", v)} />
          {linked && c?.link && c.link.tags.length > 0 && (
            <Text size="xs" c="dimmed">
              Shared tags: {c.link.tags.join(", ")}
            </Text>
          )}
          <TagsInput
            label="Short names"
            description="Your abbreviations for narrow radio displays"
            disabled={props.readOnly}
            value={form.short_names}
            onChange={(v) => set("short_names", v)}
          />
          <Textarea label="Notes" autosize minRows={2} disabled={props.readOnly} value={form.notes} onChange={(e) => set("notes", e.currentTarget.value)} />
          {linked && c?.link?.notes && (
            <Text size="xs" c="dimmed">
              Shared notes: {c.link.notes}
            </Text>
          )}
          <Group grow>
            <TextInput label="Latitude" disabled={shared} value={form.lat} onChange={(e) => set("lat", e.currentTarget.value)} />
            <TextInput label="Longitude" disabled={shared} value={form.lon} onChange={(e) => set("lon", e.currentTarget.value)} />
            <TextInput label="Radius km" disabled={shared} value={form.radius} onChange={(e) => set("radius", e.currentTarget.value)} />
          </Group>
          {!props.readOnly && (
            <Group justify="space-between" mt="md">
              {c ? (
                <Button color="red" variant="subtle" onClick={() => remove.mutate()} loading={remove.isPending}>
                  Delete
                </Button>
              ) : (
                <span />
              )}
              <Button type="submit" loading={save.isPending}>
                {c ? "Save" : "Add channel"}
              </Button>
            </Group>
          )}
        </Stack>
      </form>
    </Drawer>
  );
}
