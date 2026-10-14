import { ActionIcon, Alert, Badge, Spoiler, Button, Checkbox, Group, List, Menu, Modal, NumberInput, Select, Stack, Switch, Table, Tabs, Text, TextInput, Title, Tooltip } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { modals } from "@mantine/modals";
import { IconArrowUp, IconChevronDown, IconDownload, IconRefresh, IconTrash } from "@tabler/icons-react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { api, call, download, type Memory, type Radio, type Source } from "../api/client";
import { FilterMenu, QueryBar } from "../components/QueryBar";
import { useLib, useTags } from "../lib/library";
import { failed, ok, warn } from "../lib/notify";
import { duplex, formatMHz, offsetMHz, toneSummary } from "../lib/radio";

type SyncResult = {
  sources: { source: Source; added: number; channels: { id: string }[]; skipped: unknown[]; wrong_mode: unknown[]; resorted: number }[];
  added: number;
  resorted: number;
  plan_error: string | null;
};

export function RadioPage() {
  const lib = useLib();
  const radio = useParams().radio ?? "";
  const client = useQueryClient();
  const path = { lib, radio };
  const info = useQuery({
    queryKey: ["radio", lib, radio],
    queryFn: () => call<Radio>(api.GET("/libraries/{lib}/radios/{radio}", { params: { path } })),
  });
  const [text, setText] = useState("");
  const [q] = useDebouncedValue(text, 300);
  const plan = useQuery({
    queryKey: ["memories", lib, radio, q],
    queryFn: () =>
      call<{ memories: Memory[]; warnings: string[] }>(api.GET("/libraries/{lib}/radios/{radio}/memories", { params: { path, query: { q } } })),
    placeholderData: keepPreviousData,
  });
  const tags = useTags(lib);
  const [facets, setFacets] = useState<Facets>({ scan: null, power: null, override: null, fit: null, export: null });
  const shown = (plan.data?.memories ?? []).filter((m) => matchFacets(m, facets));
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [anchor, setAnchor] = useState<number | null>(null);
  const ids = shown.filter((m) => selected.has(m.channel.id)).map((m) => m.channel.id);
  // A shift-click selects every shown memory between it and the last one clicked.
  const pick = (i: number, shift: boolean) => {
    const id = shown[i].channel.id;
    setSelected((cur) => {
      const n = new Set(cur);
      const on = !cur.has(id);
      const [from, to] = shift && anchor !== null ? [Math.min(anchor, i), Math.max(anchor, i)] : [i, i];
      for (const m of shown.slice(from, to + 1)) {
        if (on) n.add(m.channel.id);
        else n.delete(m.channel.id);
      }
      return n;
    });
    setAnchor(i);
  };
  const allShown = shown.length > 0 && shown.every((m) => selected.has(m.channel.id));
  const bulkSet = useMutation({
    mutationFn: (body: { power?: string | null; scan?: boolean | null }) =>
      call(api.POST("/libraries/{lib}/radios/{radio}/channels/set", { params: { path }, body: { select: { ids }, ...body } })),
    onSuccess: (r) => {
      ok(`Changed ${r.channels.length} memories`);
      refresh();
    },
    onError: failed,
  });
  const bulkRemove = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/radios/{radio}/channels/remove", { params: { path }, body: { select: { ids } } })),
    onSuccess: (r) => {
      ok(`Took ${r.channels.length} channels off ${radio}; they stay in the library`);
      setSelected(new Set());
      refresh();
    },
    onError: failed,
  });
  const refresh = () => void client.invalidateQueries();
  const [preview, setPreview] = useState<SyncResult | null>(null);
  const [resort, setResort] = useState(false);
  const sync = useMutation({
    mutationFn: (dryRun: boolean) =>
      call<SyncResult>(api.POST("/libraries/{lib}/radios/{radio}/sync", { params: { path }, body: { dry_run: dryRun, resort } })),
    onSuccess: (r, dryRun) => {
      if (dryRun) {
        setPreview(r);
        return;
      }
      setPreview(null);
      ok(`Added ${r.added} channels${r.resorted ? `, re-sorted ${r.resorted}; program the radio again` : ""}`);
      if (r.plan_error) warn([r.plan_error]);
      refresh();
    },
    onError: failed,
  });
  const exportFile = useMutation({
    mutationFn: () => download(`/libraries/${encodeURIComponent(lib)}/radios/${encodeURIComponent(radio)}/export?skip_invalid=true`),
    onSuccess: (h) => {
      ok(`Exported ${h.get("X-Rflib-Count")} memories`);
      const skipped = h.get("X-Rflib-Skipped");
      if (skipped) warn([`Left out: ${skipped}`]);
    },
    onError: failed,
  });
  const top = useMutation({
    mutationFn: (id: string) => call(api.POST("/libraries/{lib}/radios/{radio}/channels/{id}/move", { params: { path: { ...path, id } }, body: { top: true } })),
    onSuccess: refresh,
    onError: failed,
  });
  const scan = useMutation({
    mutationFn: (m: Memory) =>
      call(api.PATCH("/libraries/{lib}/radios/{radio}/channels/{id}", { params: { path: { ...path, id: m.channel.id } }, body: { scan: !m.scan } })),
    onSuccess: refresh,
    onError: failed,
  });
  const takeOff = useMutation({
    mutationFn: (id: string) => call(api.POST("/libraries/{lib}/radios/{radio}/channels/remove", { params: { path }, body: { select: { ids: [id] } } })),
    onSuccess: refresh,
    onError: failed,
  });
  const r = info.data;
  return (
    <Stack>
      <Group justify="space-between">
        <div>
          <Title order={2}>{radio}</Title>
          {r && (
            <Text size="sm" c="dimmed">
              {r.model} · {r.channels} channels · default power {r.default_power || "unset"}
            </Text>
          )}
        </div>
        <Group>
          <Switch label="Re-sort" checked={resort} onChange={(e) => setResort(e.currentTarget.checked)} />
          <Button variant="light" leftSection={<IconRefresh size={16} />} onClick={() => sync.mutate(true)} loading={sync.isPending}>
            Sync
          </Button>
          <Button leftSection={<IconDownload size={16} />} onClick={() => exportFile.mutate()} loading={exportFile.isPending}>
            Export
          </Button>
        </Group>
      </Group>
      {(info.error || plan.error) && <Alert color="red">{(info.error ?? plan.error)!.message}</Alert>}
      <Tabs defaultValue="memories">
        <Tabs.List>
          <Tabs.Tab value="memories">Memories</Tabs.Tab>
          <Tabs.Tab value="sources">Sources</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="memories" pt="sm">
          {plan.data && plan.data.warnings.length > 0 && (
            <Alert color="yellow" title={`${plan.data.warnings.length} warnings`} mb="sm">
              <Spoiler maxHeight={66} showLabel="Show all" hideLabel="Show fewer">
                <List size="sm">
                  {plan.data.warnings.map((w) => (
                    <List.Item key={w}>{w}</List.Item>
                  ))}
                </List>
              </Spoiler>
            </Alert>
          )}
          <Stack gap="xs" mb="sm">
            <QueryBar value={text} onChange={setText} tags={tags} />
            <Group gap={6}>
              <Text size="xs" c="dimmed">
                On this radio:
              </Text>
              <FacetMenus memories={plan.data?.memories ?? []} facets={facets} onChange={setFacets} />
            </Group>
          </Stack>
          <Group mb="xs" mih={30}>
            <Text size="sm" c="dimmed">
              {plan.data && r ? `${shown.length} of ${r.channels} memories` : "Loading…"}
            </Text>
            {ids.length > 0 && (
              <>
                <Text size="sm">{ids.length} selected</Text>
                <BulkMenu
                  label="Scan"
                  options={[
                    { label: "On", body: { scan: true } },
                    { label: "Off", body: { scan: false } },
                    { label: "Use the channel's setting", body: { scan: null } },
                  ]}
                  onPick={(b) => bulkSet.mutate(b)}
                />
                <BulkMenu
                  label="Power"
                  options={[
                    ...["low", "medium", "high", "max"].map((p) => ({ label: p, body: { power: p } })),
                    { label: "Use the default", body: { power: null } },
                  ]}
                  onPick={(b) => bulkSet.mutate(b)}
                />
                <Button
                  size="compact-sm"
                  color="red"
                  variant="light"
                  onClick={() =>
                    modals.openConfirmModal({
                      title: `Take ${ids.length} channels off ${radio}?`,
                      children: <Text size="sm">They stay in the library, and the memories after them move up.</Text>,
                      labels: { confirm: "Take off", cancel: "Cancel" },
                      confirmProps: { color: "red" },
                      onConfirm: () => bulkRemove.mutate(),
                    })
                  }
                >
                  Take off radio
                </Button>
                <Button size="compact-sm" variant="subtle" onClick={() => setSelected(new Set())}>
                  Clear
                </Button>
              </>
            )}
          </Group>
          <Table.ScrollContainer minWidth={800}>
            <Table striped>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th w={36}>
                    <Checkbox
                      aria-label="Select all shown"
                      checked={allShown}
                      indeterminate={ids.length > 0 && !allShown}
                      onChange={() => setSelected(allShown ? new Set() : new Set(shown.map((m) => m.channel.id)))}
                    />
                  </Table.Th>
                  <Table.Th>Memory</Table.Th>
                  <Table.Th>Name on radio</Table.Th>
                  <Table.Th>Fit</Table.Th>
                  <Table.Th>Power</Table.Th>
                  <Table.Th>Scan</Table.Th>
                  <Table.Th>RX MHz</Table.Th>
                  <Table.Th>Duplex</Table.Th>
                  <Table.Th>Tone</Table.Th>
                  <Table.Th>Channel</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {shown.map((m, i) => (
                  <Table.Tr key={m.channel.id} opacity={m.exported ? 1 : 0.5}>
                    <Table.Td>
                      <Checkbox
                        aria-label={`Select memory ${m.memory}`}
                        checked={selected.has(m.channel.id)}
                        onChange={() => undefined}
                        onClick={(e) => pick(i, e.shiftKey)}
                      />
                    </Table.Td>
                    <Table.Td>
                      {m.section_start && "§"}
                      {m.memory}
                    </Table.Td>
                    <Table.Td ff="monospace">
                      {m.name}
                      {m.overrides.name && "*"}
                    </Table.Td>
                    <Table.Td>{m.fit !== "fits" && <Badge size="xs" variant="light">{m.fit}</Badge>}</Table.Td>
                    <Table.Td>
                      {m.power ?? "-"}
                      {m.overrides.power && "*"}
                    </Table.Td>
                    <Table.Td>
                      <Switch size="xs" aria-label={`Scan ${m.channel.name}`} checked={m.scan} onChange={() => scan.mutate(m)} />
                    </Table.Td>
                    <Table.Td ff="monospace">{formatMHz(m.channel.rx_hz)}</Table.Td>
                    <Table.Td>
                      {duplex(m.channel.rx_hz, m.channel.tx_hz)} {offsetMHz(m.channel.rx_hz, m.channel.tx_hz)}
                    </Table.Td>
                    <Table.Td>{toneSummary(m.channel)}</Table.Td>
                    <Table.Td>{m.channel.name !== m.name ? m.channel.name : ""}</Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="nowrap">
                        <Tooltip label="Move to the top">
                          <ActionIcon variant="subtle" size="sm" aria-label="Move to the top" onClick={() => top.mutate(m.channel.id)}>
                            <IconArrowUp size={14} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Take off this radio">
                          <ActionIcon variant="subtle" color="red" size="sm" aria-label="Take off this radio" onClick={() => takeOff.mutate(m.channel.id)}>
                            <IconTrash size={14} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
          <Text size="xs" c="dimmed" mt="xs">
            § starts a section at that memory; * marks this radio's override. Shift-click selects a range. Add channels from the Channels page, or with sources; reorder them with sources and re-sort.
          </Text>
        </Tabs.Panel>
        <Tabs.Panel value="sources" pt="sm">
          <Sources />
        </Tabs.Panel>
      </Tabs>
      <Modal opened={preview !== null} onClose={() => setPreview(null)} title="Sync" size="lg">
        {preview && (
          <Stack>
            {preview.sources.map((s) => (
              <Text key={s.source.id} size="sm">
                Source {s.source.number} ({s.source.query}): {s.channels.length} to add
                {s.skipped.length + s.wrong_mode.length > 0 && `, ${s.skipped.length + s.wrong_mode.length} skipped`}
                {s.resorted > 0 && `, ${s.resorted} to re-sort`}
              </Text>
            ))}
            {preview.sources.length === 0 && <Text size="sm">This radio has no sources; add one on the Sources tab.</Text>}
            <Button onClick={() => sync.mutate(false)} loading={sync.isPending} disabled={preview.sources.length === 0}>
              Sync now
            </Button>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}

function Sources() {
  const lib = useLib();
  const radio = useParams().radio ?? "";
  const client = useQueryClient();
  const path = { lib, radio };
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<string>("name");
  const [section, setSection] = useState<number | string>("");
  const sources = useQuery({
    queryKey: ["sources", lib, radio],
    queryFn: () => call<{ sources: Source[] }>(api.GET("/libraries/{lib}/radios/{radio}/sources", { params: { path } })),
  });
  const add = useMutation({
    mutationFn: () =>
      call(
        api.POST("/libraries/{lib}/radios/{radio}/sources", {
          params: { path },
          body: { query, sort, section_start: section === "" ? null : Number(section) },
        }),
      ),
    onSuccess: () => {
      setQuery("");
      setSection("");
      ok("Source added; sync to add its channels");
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const remove = useMutation({
    mutationFn: (n: number) => call(api.DELETE("/libraries/{lib}/radios/{radio}/sources/{n}", { params: { path: { ...path, n } } })),
    onSuccess: () => void client.invalidateQueries(),
    onError: failed,
  });
  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Sync adds each source's new matches to its part of the list, in its sort order. Earlier sources claim a channel first.
      </Text>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>#</Table.Th>
            <Table.Th>Query</Table.Th>
            <Table.Th>Sort</Table.Th>
            <Table.Th>Section</Table.Th>
            <Table.Th>Channels</Table.Th>
            <Table.Th />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(sources.data?.sources ?? []).map((s) => (
            <Table.Tr key={s.id}>
              <Table.Td>{s.number}</Table.Td>
              <Table.Td ff="monospace">{s.query}</Table.Td>
              <Table.Td>
                {s.sort}
                {s.keep_sorted && " (kept sorted)"}
              </Table.Td>
              <Table.Td>{s.section_start ?? ""}</Table.Td>
              <Table.Td>{s.channels}</Table.Td>
              <Table.Td>
                <ActionIcon
                  variant="subtle"
                  color="red"
                  aria-label={`Remove source ${s.number}`}
                  onClick={() =>
                    modals.openConfirmModal({
                      title: `Remove source ${s.number}?`,
                      children: <Text size="sm">Its channels stay on the radio.</Text>,
                      labels: { confirm: "Remove", cancel: "Cancel" },
                      onConfirm: () => remove.mutate(s.number),
                    })
                  }
                >
                  <IconTrash size={14} />
                </ActionIcon>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Group align="end">
        <TextInput flex={1} label="New source query" placeholder="band:gmrs -is:repeater" value={query} onChange={(e) => setQuery(e.currentTarget.value)} />
        <Select label="Sort" w={100} data={["name", "freq"]} value={sort} allowDeselect={false} onChange={(v) => v && setSort(v)} />
        <NumberInput label="Section start" w={120} min={0} value={section} onChange={setSection} />
        <Button onClick={() => add.mutate()} loading={add.isPending} disabled={!query.trim()}>
          Add source
        </Button>
      </Group>
    </Stack>
  );
}

type Facets = { scan: string | null; power: string | null; override: string | null; fit: string | null; export: string | null };

/** matchFacets applies the filters on this radio's own settings, which a query can't express. */
function matchFacets(m: Memory, f: Facets): boolean {
  if (f.scan && (f.scan === "on") !== m.scan) return false;
  if (f.power && (m.power ?? "none") !== f.power) return false;
  if (f.override === "any" && !(m.overrides.name || m.overrides.power || m.overrides.scan)) return false;
  if (f.override && f.override !== "any" && !m.overrides[f.override as "name" | "power" | "scan"]) return false;
  if (f.fit === "changed" && m.fit !== "shortened" && m.fit !== "cut") return false;
  if (f.fit === "cut" && m.fit !== "cut") return false;
  if (f.export === "left-out" && m.exported) return false;
  return true;
}

function FacetMenus(props: { memories: Memory[]; facets: Facets; onChange: (f: Facets) => void }) {
  const set = (k: keyof Facets) => (v: string | null) => props.onChange({ ...props.facets, [k]: v });
  const powers = [...new Set(props.memories.map((m) => m.power ?? "none"))];
  return (
    <>
      <FilterMenu label="Scan" options={[{ value: "on", label: "on" }, { value: "off", label: "off" }]} value={props.facets.scan} onChange={set("scan")} />
      <FilterMenu label="Power" options={powers.map((p) => ({ value: p, label: p }))} value={props.facets.power} onChange={set("power")} />
      <FilterMenu
        label="Override"
        options={[
          { value: "any", label: "any override" },
          { value: "name", label: "name" },
          { value: "power", label: "power" },
          { value: "scan", label: "scan" },
        ]}
        value={props.facets.override}
        onChange={set("override")}
      />
      <FilterMenu
        label="Name fit"
        options={[
          { value: "changed", label: "shortened or cut" },
          { value: "cut", label: "cut" },
        ]}
        value={props.facets.fit}
        onChange={set("fit")}
      />
      <FilterMenu label="In export" options={[{ value: "left-out", label: "left out" }]} value={props.facets.export} onChange={set("export")} />
    </>
  );
}

type SetBody = { power?: string | null; scan?: boolean | null };

function BulkMenu(props: { label: string; options: { label: string; body: SetBody }[]; onPick: (b: SetBody) => void }) {
  return (
    <Menu position="bottom-start" withinPortal>
      <Menu.Target>
        <Button size="compact-sm" variant="light" rightSection={<IconChevronDown size={14} />}>
          {props.label}
        </Button>
      </Menu.Target>
      <Menu.Dropdown>
        {props.options.map((o) => (
          <Menu.Item key={o.label} onClick={() => props.onPick(o.body)}>
            {o.label}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}
