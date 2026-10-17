import { ActionIcon, Affix, Alert, Badge, Paper, SegmentedControl, Spoiler, Button, Checkbox, Group, List, Menu, Modal, Select, Stack, Switch, Table, Tabs, Text, TextInput, Title, Tooltip } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { modals } from "@mantine/modals";
import { IconArrowDown, IconArrowUp, IconChevronDown, IconCopy, IconDots, IconDownload, IconGripVertical, IconPencil, IconPlus, IconRefresh, IconTrash } from "@tabler/icons-react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api, call, download, showDraft, type Channel, type Memory, type Radio, type Section, type Source } from "../api/client";
import { CompareRadio } from "../components/CompareRadio";
import { CompareRadios } from "../components/CompareRadios";
import { CopyRadio } from "../components/CopyRadio";
import { DraftBar, ExportChoice } from "../components/Drafts";
import { DeleteRadio, EditRadio } from "../components/RadioSettings";
import { RevisionsTab } from "../components/Revisions";
import { ReasonModal } from "../components/Records";
import { FilterMenu, QueryBar } from "../components/QueryBar";
import { confirmMerge, SectionEditor, SectionHeader, useMergeSection, useSections } from "../components/Sections";
import { SourcePreview } from "../components/SourcePreview";
import { useLib, useTags } from "../lib/library";
import { failed, ok, warn } from "../lib/notify";
import { draftMarks, type DraftMark } from "../lib/draft";
import { usePlans } from "../lib/records";
import { duplex, formatMHz, offsetMHz, toneSummary } from "../lib/radio";

type SyncResult = {
  sources: {
    source: Source;
    added: number;
    channels: { id: string }[];
    skipped: unknown[];
    wrong_mode: unknown[];
    resorted: number;
    removed: unknown[];
    note: string;
  }[];
  added: number;
  resorted: number;
  layout_error: string | null;
};

export function RadioPage() {
  const lib = useLib();
  const radio = useParams().radio ?? "";
  const client = useQueryClient();
  const path = { lib, radio };
  // The radio's list may change elsewhere (another window, or the CLI),
  // so the page reloads when it's looked at again.
  const info = useQuery({
    queryKey: ["radio", lib, radio],
    queryFn: () => call<Radio>(api.GET("/libraries/{lib}/radios/{radio}", { params: { path } })),
    refetchOnWindowFocus: true,
  });
  // Changes from this page are made against the draft it shows.
  useEffect(() => {
    if (info.data) showDraft(lib, radio, info.data.draft?.id ?? null);
  }, [lib, radio, info.data]);
  const [text, setText] = useState("");
  const [q] = useDebouncedValue(text, 300);
  const plan = useQuery({
    queryKey: ["memories", lib, radio, q],
    queryFn: () =>
      call<{ memories: Memory[]; sections: Section[]; warnings: string[] }>(
        api.GET("/libraries/{lib}/radios/{radio}/memories", { params: { path, query: { q } } }),
      ),
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: true,
  });
  const sources = useQuery({
    queryKey: ["sources", lib, radio],
    queryFn: () => call<{ sources: Source[] }>(api.GET("/libraries/{lib}/radios/{radio}/sources", { params: { path } })),
    refetchOnWindowFocus: true,
  });
  // A draft's changes from the saved radio, to mark them in the list.
  const draftID = info.data?.draft?.id;
  const draftChanges = useQuery({
    queryKey: ["draft", lib, radio, draftID],
    queryFn: () => call(api.GET("/libraries/{lib}/radios/{radio}/draft", { params: { path } })),
    enabled: !!draftID,
  });
  const changes = draftID ? draftChanges.data?.changes : undefined;
  const marks = useMemo(() => draftMarks(changes), [changes]);
  const sections = plan.data?.sections ?? [];
  const [editingSection, setEditingSection] = useState<Section | "new" | null>(null);
  // Drag and drop: a memory onto a memory (before or after it), onto a
  // section's header (its top), or into an empty section; a section's
  // header onto another's (its place).
  const [drag, setDrag] = useState<{ kind: "memory" | "section"; id: string } | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const moveMemory = useMutation({
    mutationFn: ({ id, body }: { id: string; body: { section?: string; before?: string; after?: string; top?: boolean } }) =>
      call(api.POST("/libraries/{lib}/radios/{radio}/channels/{id}/move", { params: { path: { ...path, id } }, body })),
    onSuccess: (r) => {
      clearedStarts(r.starts_cleared);
      refreshAll();
    },
    onError: failed,
  });
  const moveSection = useMutation({
    mutationFn: ({ id, to }: { id: string; to: number }) =>
      call(api.POST("/libraries/{lib}/radios/{radio}/sections/{section}/move", { params: { path: { ...path, section: id } }, body: { to } })),
    onSuccess: (r) => {
      clearedStarts(r.starts_cleared);
      refreshAll();
    },
    onError: failed,
  });
  function refreshAll() {
    void client.invalidateQueries();
  }
  const dropOnMemory = (target: Memory, e: React.DragEvent) => {
    e.preventDefault();
    setOver(null);
    if (drag?.kind !== "memory" || drag.id === target.channel.id) return;
    const box = e.currentTarget.getBoundingClientRect();
    const after = e.clientY > box.top + box.height / 2;
    moveMemory.mutate({ id: drag.id, body: after ? { after: target.channel.id } : { before: target.channel.id } });
    setDrag(null);
  };
  const dropOnSection = (target: Section, e: React.DragEvent) => {
    e.preventDefault();
    setOver(null);
    if (drag?.kind === "memory") moveMemory.mutate({ id: drag.id, body: { section: target.id, top: true } });
    if (drag?.kind === "section" && drag.id !== target.id) moveSection.mutate({ id: drag.id, to: target.number });
    setDrag(null);
  };
  const dragOver = (key: string) => (e: React.DragEvent) => {
    if (!drag) return;
    e.preventDefault();
    setOver(key);
  };
  useAutoScroll(drag !== null);
  const merge = useMergeSection(lib, radio);
  const keep = useMutation({
    mutationFn: (keepIDs: string[]) => call(api.POST("/libraries/{lib}/radios/{radio}/channels/keep", { params: { path }, body: { ids: keepIDs } })),
    onSuccess: (_, keepIDs) => {
      ok(`Kept ${keepIDs.length} memories: no source will take them off`);
      refreshAll();
    },
    onError: failed,
  });
  const moveMany = useMutation({
    mutationFn: (section: string) => call(api.POST("/libraries/{lib}/radios/{radio}/channels/move", { params: { path }, body: { ids, section } })),
    onSuccess: (r) => {
      ok(`Moved ${ids.length} memories`);
      clearedStarts(r.starts_cleared);
      setSelected(new Set());
      refreshAll();
    },
    onError: failed,
  });
  const tags = useTags(lib);
  const [facets, setFacets] = useState<Facets>(NO_FACETS);
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
  const [copying, setCopying] = useState(false);
  const [managing, setManaging] = useState<"edit" | "delete" | null>(null);
  const [choosingExport, setChoosingExport] = useState(false);
  const [compareWith, setCompareWith] = useState<string>("file");
  const sync = useMutation({
    mutationFn: (dryRun: boolean) =>
      call<SyncResult>(api.POST("/libraries/{lib}/radios/{radio}/sync", { params: { path }, body: { dry_run: dryRun } })),
    onSuccess: (r, dryRun) => {
      if (dryRun) {
        setPreview(r);
        return;
      }
      setPreview(null);
      ok(`Added ${r.added} channels${r.resorted ? `, moved ${r.resorted} into their plans' order; program the radio again` : ""}`);
      if (r.layout_error) warn([r.layout_error]);
      refresh();
    },
    onError: failed,
  });
  const exportFile = useMutation({
    mutationFn: (reason: string) =>
      download(
        `/libraries/${encodeURIComponent(lib)}/radios/${encodeURIComponent(radio)}/export?skip_invalid=true${reason ? `&reason=${encodeURIComponent(reason)}` : ""}`,
      ),
    onSuccess: (h) => {
      const saved = h.get("X-Rflib-Revision-Saved") === "true";
      ok(`Exported ${h.get("X-Rflib-Count")} memories${saved ? `; saved revision ${h.get("X-Rflib-Revision")}` : ""}`);
      const skipped = h.get("X-Rflib-Skipped");
      if (skipped) warn([`Left out: ${skipped}`]);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  // An export that saves a revision (the radio changed since its last)
  // asks why first.
  const [askingReason, setAskingReason] = useState(false);
  const startExport = async () => {
    const fresh = await info.refetch();
    if (fresh.data?.changed_since_revision) setAskingReason(true);
    else exportFile.mutate("");
  };
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
          <Group gap="xs">
            <Title order={2}>{radio}</Title>
            {info.data?.needs_programming && (
              <Tooltip label="The saved radio differs from what it was last exported with, so program it again">
                <Badge color="orange" variant="light">
                  Needs programming
                </Badge>
              </Tooltip>
            )}
            {info.data?.sync_changes != null && info.data.sync_changes > 0 && (
              <Tooltip label="A sync of the saved radio would add or take off this many channels">
                <Badge variant="light">Sync would change {info.data.sync_changes}</Badge>
              </Tooltip>
            )}
          </Group>
          {r && (
            <Text size="sm" c="dimmed">
              {r.model} · {r.channels} channels · default power {r.default_power || "unset"}
              {r.name_length > 0 && ` · names up to ${r.name_length}`}
            </Text>
          )}
        </div>
        <Group>
          <Button variant="light" leftSection={<IconRefresh size={16} />} onClick={() => sync.mutate(true)} loading={sync.isPending}>
            Sync
          </Button>
          <Button variant="default" leftSection={<IconCopy size={16} />} onClick={() => setCopying(true)}>
            Copy
          </Button>
          <Button
            leftSection={<IconDownload size={16} />}
            onClick={() => (info.data?.draft ? setChoosingExport(true) : void startExport())}
            loading={exportFile.isPending}
          >
            Export
          </Button>
          <Menu position="bottom-end" withinPortal>
            <Menu.Target>
              <ActionIcon variant="default" size="lg" aria-label="Radio settings" disabled={!r}>
                <IconDots size={16} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item leftSection={<IconPencil size={14} />} onClick={() => setManaging("edit")}>
                Edit radio…
              </Menu.Item>
              <Menu.Item color="red" leftSection={<IconTrash size={14} />} onClick={() => setManaging("delete")}>
                Delete radio…
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Group>
      {(info.error || plan.error) && <Alert color="red">{(info.error ?? plan.error)!.message}</Alert>}
      {info.data?.draft && <DraftBar lib={lib} radio={radio} draft={info.data.draft} />}
      <Tabs defaultValue="memories">
        <Tabs.List>
          <Tabs.Tab value="memories">Memories</Tabs.Tab>
          <Tabs.Tab value="sources">Sources</Tabs.Tab>
          <Tabs.Tab value="compare">Compare</Tabs.Tab>
          <Tabs.Tab value="revisions">Revisions</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="memories" pt="sm">
          {drag?.kind === "section" && sections.length > 1 && (
            // Merging is its own drop target, so a drop on a header always
            // means reorder.
            <Affix position={{ top: 8, left: "50%" }} style={{ transform: "translateX(-50%)" }} zIndex={300}>
              <Paper shadow="md" p="xs" withBorder>
                <Group gap={6} wrap="wrap" maw="90vw">
                  <Text size="xs" c="dimmed">
                    Merge into:
                  </Text>
                  {sections
                    .filter((sec) => sec.id !== drag.id)
                    .map((sec) => (
                      <Badge
                        key={sec.id}
                        size="lg"
                        color="grape"
                        variant={over === `merge-${sec.id}` ? "filled" : "light"}
                        tt="none"
                        style={{ cursor: "copy" }}
                        onDragOver={dragOver(`merge-${sec.id}`)}
                        onDragLeave={() => setOver(null)}
                        onDrop={(e) => {
                          e.preventDefault();
                          setOver(null);
                          const from = sections.find((x) => x.id === drag.id);
                          setDrag(null);
                          if (from) confirmMerge(from, sec, () => merge.mutate({ from: from.id, into: sec.id }));
                        }}
                        data-merge-section={sec.name}
                      >
                        {sec.name}
                      </Badge>
                    ))}
                </Group>
              </Paper>
            </Affix>
          )}
          {drag?.kind === "memory" && (
            // Every section, in reach wherever the memory is on the page.
            <Affix position={{ top: 8, left: "50%" }} style={{ transform: "translateX(-50%)" }} zIndex={300}>
              <Paper shadow="md" p="xs" withBorder>
                <Group gap={6} wrap="wrap" maw="90vw">
                  <Text size="xs" c="dimmed">
                    Drop into section:
                  </Text>
                  {sections.map((sec) => (
                    <Badge
                      key={sec.id}
                      size="lg"
                      variant={over === `bar-${sec.id}` ? "filled" : "light"}
                      tt="none"
                      style={{ cursor: "copy" }}
                      onDragOver={dragOver(`bar-${sec.id}`)}
                      onDragLeave={() => setOver(null)}
                      onDrop={(e) => {
                        e.preventDefault();
                        setOver(null);
                        moveMemory.mutate({ id: drag.id, body: { section: sec.id } });
                        setDrag(null);
                      }}
                      data-drop-section={sec.name}
                    >
                      {sec.name}
                    </Badge>
                  ))}
                </Group>
              </Paper>
            </Affix>
          )}
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
            <QueryBar
              value={text}
              onChange={setText}
              tags={tags}
              extra={{ active: Object.values(facets).some((v) => v !== null), clear: () => setFacets(NO_FACETS) }}
            />
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
              {plan.data && <ProgrammedSummary memories={plan.data.memories} />}
            </Text>
            <Button size="compact-sm" variant="light" leftSection={<IconPlus size={14} />} onClick={() => setEditingSection("new")}>
              Add section
            </Button>
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
                <Tooltip label="Keep them even if their sources stop matching: they count as added by hand">
                  <Button size="compact-sm" variant="light" onClick={() => keep.mutate(ids)}>
                    Keep
                  </Button>
                </Tooltip>
                <Menu position="bottom-start" withinPortal>
                  <Menu.Target>
                    <Button size="compact-sm" variant="light" rightSection={<IconChevronDown size={14} />}>
                      Move to section
                    </Button>
                  </Menu.Target>
                  <Menu.Dropdown>
                    {sections.map((sec) => (
                      <Menu.Item key={sec.id} onClick={() => moveMany.mutate(sec.id)}>
                        {sec.name}
                      </Menu.Item>
                    ))}
                  </Menu.Dropdown>
                </Menu>
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
                  <Table.Th w={28} />
                  <Table.Th>Memory</Table.Th>
                  {draftID && <Table.Th>Draft</Table.Th>}
                  <Table.Th>Name on radio</Table.Th>
                  <Table.Th>Fit</Table.Th>
                  <Table.Th>Power</Table.Th>
                  <Table.Th>Scan</Table.Th>
                  <Table.Th>RX MHz</Table.Th>
                  <Table.Th>Duplex</Table.Th>
                  <Table.Th>Tone</Table.Th>
                  <Table.Th>Channel</Table.Th>
                  <Table.Th>Programmed</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {sections.map((sec) => {
                  const rows = shown.filter((m) => m.section === sec.id);
                  const headerKey = `section-${sec.id}`;
                  return [
                    <Table.Tr
                      key={headerKey}
                      bg={over === headerKey ? "var(--mantine-color-blue-light)" : "var(--mantine-color-default-hover)"}
                      onDragOver={dragOver(headerKey)}
                      onDragLeave={() => setOver(null)}
                      onDrop={(e) => dropOnSection(sec, e)}
                      data-section={sec.name}
                    >
                      <Table.Td colSpan={14}>
                        <SectionHeader
                          section={sec}
                          sources={(sources.data?.sources ?? []).filter((src) => src.section_id === sec.id)}
                          onEdit={() => setEditingSection(sec)}
                          onSelectAll={rows.length ? () => setSelected(new Set([...selected, ...rows.map((m) => m.channel.id)])) : undefined}
                          dragHandle={{
                            draggable: true,
                            onDragStart: () => setDrag({ kind: "section", id: sec.id }),
                            onDragEnd: () => setDrag(null),
                          }}
                        />
                      </Table.Td>
                    </Table.Tr>,
                    rows.length === 0 && (
                      <Table.Tr key={`empty-${sec.id}`} onDragOver={dragOver(`empty-${sec.id}`)} onDrop={(e) => dropOnSection(sec, e)}>
                        <Table.Td colSpan={14}>
                          <Text size="xs" c="dimmed" ta="center" bg={over === `empty-${sec.id}` ? "var(--mantine-color-blue-light)" : undefined}>
                            {q || Object.values(facets).some((v) => v !== null) ? "Nothing shown here" : "No channels yet: drag memories here"}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    ),
                    ...rows.map((m) => {
                      const i = shown.indexOf(m);
                      const mark = marks.get(m.channel.id);
                      const edge = mark && markColor(mark);
                      const shadows = [
                        over === m.channel.id && "inset 0 2px 0 var(--mantine-color-blue-filled)",
                        edge && `inset 4px 0 0 var(--mantine-color-${edge}-filled)`,
                      ].filter(Boolean);
                      return (
                        <Table.Tr
                          key={m.channel.id}
                          opacity={m.exported ? (drag?.id === m.channel.id ? 0.4 : 1) : 0.55}
                          bg={mark?.kind === "added" ? "var(--mantine-color-green-light)" : undefined}
                          onDragOver={dragOver(m.channel.id)}
                          onDragLeave={() => setOver(null)}
                          onDrop={(e) => dropOnMemory(m, e)}
                          style={shadows.length ? { boxShadow: shadows.join(", ") } : undefined}
                          data-draft={mark?.kind}
                        >
                          <Table.Td>
                            <Checkbox
                              aria-label={`Select memory ${m.memory ?? m.channel.name}`}
                              checked={selected.has(m.channel.id)}
                              onChange={() => undefined}
                              onClick={(e) => pick(i, e.shiftKey)}
                            />
                          </Table.Td>
                          <Table.Td
                            draggable
                            onDragStart={() => setDrag({ kind: "memory", id: m.channel.id })}
                            onDragEnd={() => {
                              setDrag(null);
                              setOver(null);
                            }}
                            style={{ cursor: "grab" }}
                            aria-label={`Drag ${m.channel.name}`}
                          >
                            <IconGripVertical size={14} color="var(--mantine-color-dimmed)" />
                          </Table.Td>
                          <Table.Td>
                            {m.exported ? (
                              m.memory
                            ) : (
                              <Text span c="dimmed" aria-label="No memory number">
                                –
                              </Text>
                            )}
                          </Table.Td>
                          {draftID && <Table.Td>{mark && <DraftBadge mark={mark} />}</Table.Td>}
                          <Table.Td ff="monospace" td={m.exported ? undefined : "line-through"}>
                            {m.name}
                            {m.overrides.name && "*"}
                            {m.plan_number != null && (
                              <Text span size="xs" c="dimmed" ff="text" ml={6}>
                                Ch {m.plan_number}
                              </Text>
                            )}
                            {m.source != null && <SourceMark n={m.source} sources={sources.data?.sources ?? []} />}
                          </Table.Td>
                          <Table.Td>{m.fit !== "fits" && <Badge size="xs" variant="light">{m.fit}</Badge>}</Table.Td>
                          <Table.Td>
                            {m.power ?? "-"}
                            {m.overrides.power && "*"}
                          </Table.Td>
                          <Table.Td>
                            <ScanSwitch m={m} onToggle={() => scan.mutate(m)} />
                          </Table.Td>
                          <Table.Td ff="monospace">{formatMHz(m.channel.rx_hz)}</Table.Td>
                          <Table.Td>
                            {duplex(m.channel.rx_hz, m.channel.tx_hz)} {offsetMHz(m.channel.rx_hz, m.channel.tx_hz)}
                          </Table.Td>
                          <Table.Td>{toneSummary(m.channel)}</Table.Td>
                          <Table.Td>{m.channel.name !== m.name ? m.channel.name : ""}</Table.Td>
                          <Table.Td>
                            <Programmed m={m} />
                          </Table.Td>
                          <Table.Td>
                            <Group gap={4} wrap="nowrap">
                              <Menu position="bottom-end" withinPortal>
                                <Menu.Target>
                                  <ActionIcon variant="subtle" size="sm" aria-label={`Move ${m.channel.name}`}>
                                    <IconDots size={14} />
                                  </ActionIcon>
                                </Menu.Target>
                                <Menu.Dropdown>
                                  <Menu.Item leftSection={<IconArrowUp size={14} />} onClick={() => moveMemory.mutate({ id: m.channel.id, body: { top: true } })}>
                                    Top of its section
                                  </Menu.Item>
                                  {m.source != null && (
                                    <Menu.Item onClick={() => keep.mutate([m.channel.id])}>Keep, even if source {m.source} stops matching</Menu.Item>
                                  )}
                                  <Menu.Label>Move to section</Menu.Label>
                                  {sections
                                    .filter((x) => x.id !== m.section)
                                    .map((x) => (
                                      <Menu.Item key={x.id} onClick={() => moveMemory.mutate({ id: m.channel.id, body: { section: x.id } })}>
                                        {x.name}
                                      </Menu.Item>
                                    ))}
                                </Menu.Dropdown>
                              </Menu>
                              <Tooltip label="Take off this radio">
                                <ActionIcon variant="subtle" color="red" size="sm" aria-label="Take off this radio" onClick={() => takeOff.mutate(m.channel.id)}>
                                  <IconTrash size={14} />
                                </ActionIcon>
                              </Tooltip>
                            </Group>
                          </Table.Td>
                        </Table.Tr>
                      );
                    }),
                  ];
                })}
                {changes && changes.removed.length > 0 && <TakenOff channels={changes.removed} />}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
          <Text size="xs" c="dimmed" mt="xs">
            Drag memories within and between sections, and drag a section by its handle to reorder sections; while dragging, the bar at the top takes a
            memory to any section, and the page scrolls near the window's edges. Placing a memory in a section sorted by name or frequency makes it
            manual. * marks this radio's override; Ch is a plan's channel number. Shift-click selects a range. Add channels from the
            Channels page, or with sources.
            {draftID &&
              " In a draft, green marks memories it adds, orange ones it changes, and blue ones it moves to another section, against the saved radio; memories it takes off are listed last."}
          </Text>
          {editingSection && (
            <SectionEditor lib={lib} radio={radio} section={editingSection === "new" ? null : editingSection} onClose={() => setEditingSection(null)} />
          )}
        </Tabs.Panel>
        <Tabs.Panel value="sources" pt="sm">
          <Sources />
        </Tabs.Panel>
        <Tabs.Panel value="compare" pt="sm">
          <SegmentedControl
            mb="sm"
            data={[
              { value: "file", label: "With the radio's file" },
              { value: "radio", label: "With another radio" },
            ]}
            value={compareWith}
            onChange={setCompareWith}
          />
          {compareWith === "file" ? <CompareRadio lib={lib} radio={radio} /> : <CompareRadios lib={lib} radio={radio} />}
        </Tabs.Panel>
        <Tabs.Panel value="revisions" pt="sm">
          <RevisionsTab lib={lib} radio={radio} info={info.data} />
        </Tabs.Panel>
      </Tabs>
      {copying && <CopyRadio lib={lib} radio={radio} onClose={() => setCopying(false)} />}
      {managing === "edit" && r && <EditRadio lib={lib} radio={r} onClose={() => setManaging(null)} />}
      {choosingExport && info.data?.draft && (
        <ExportChoice lib={lib} radio={radio} draft={info.data.draft} onExport={() => void startExport()} onClose={() => setChoosingExport(false)} />
      )}
      <ReasonModal
        opened={askingReason}
        title={`Export ${radio}`}
        loading={exportFile.isPending}
        onClose={() => setAskingReason(false)}
        onConfirm={(reason) => {
          setAskingReason(false);
          exportFile.mutate(reason);
        }}
      >
        <Text size="sm">{radio} changed since its last revision, so this export saves a new one. Why are you programming it?</Text>
      </ReasonModal>
      {managing === "delete" && r && <DeleteRadio lib={lib} radio={r} onClose={() => setManaging(null)} />}
      <Modal opened={preview !== null} onClose={() => setPreview(null)} title="Sync" size="lg">
        {preview && (
          <Stack>
            {preview.sources.map((s) => (
              <Text key={s.source.id} size="sm">
                Source {s.source.number} ({s.source.plan ? `plan ${s.source.plan}` : s.source.query}):{" "}
                {s.note || `${s.channels.length} to add`}
                {s.removed.length > 0 && `, ${s.removed.length} to take off`}
                {s.skipped.length + s.wrong_mode.length > 0 && `, ${s.skipped.length + s.wrong_mode.length} skipped`}
                {s.resorted > 0 && `, ${s.resorted} to put in the plan's order`}
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
  const [plan, setPlan] = useState<string | null>(null);
  const plans = usePlans();
  const [sort, setSort] = useState<string>("name");
  const [section, setSection] = useState<string>("");
  const [planNumbers, setPlanNumbers] = useState<string>("compact");
  const sections = useSections(lib, radio);
  const sectionChoices = [
    { value: "", label: "A new section of its own" },
    ...(sections.data?.sections ?? []).map((x) => ({ value: x.id, label: x.name })),
  ];
  const sources = useQuery({
    queryKey: ["sources", lib, radio],
    queryFn: () => call<{ sources: Source[] }>(api.GET("/libraries/{lib}/radios/{radio}/sources", { params: { path } })),
  });
  const add = useMutation({
    mutationFn: () =>
      call(
        api.POST("/libraries/{lib}/radios/{radio}/sources", {
          params: { path },
          body: { ...(plan ? { plan, plan_numbers: planNumbers as Source["plan_numbers"] } : { query, sort }), ...(section ? { section } : {}) },
        }),
      ),
    onSuccess: () => {
      setQuery("");
      setPlan(null);
      setSection("");
      setPlanNumbers("compact");
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
  const move = useMutation({
    mutationFn: ({ n, to }: { n: number; to: number }) =>
      call(api.POST("/libraries/{lib}/radios/{radio}/sources/{n}/move", { params: { path: { ...path, n } }, body: { to } })),
    onSuccess: () => {
      ok("Source moved; sync to apply its new order of claims");
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const [editing, setEditing] = useState<Source | null>(null);
  const count = sources.data?.sources.length ?? 0;
  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Sync adds each query source's new matches to its section, in its sort order (a sorted section keeps its own); earlier sources claim a
        channel first. A plan source follows its plan: its channels, order, and names, numbered without gaps or matching the plan's numbers.
      </Text>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>#</Table.Th>
            <Table.Th>Query or plan</Table.Th>
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
              <Table.Td ff={s.plan ? undefined : "monospace"}>{s.plan ? `Plan: ${s.plan}` : s.query}</Table.Td>
              <Table.Td>{s.plan ? (s.plan_numbers === "match" ? "plan numbers" : "plan order") : s.sort}</Table.Td>
              <Table.Td>{s.section}</Table.Td>
              <Table.Td>{s.channels}</Table.Td>
              <Table.Td>
                <Group gap={2} wrap="nowrap">
                  <ActionIcon variant="subtle" aria-label={`Edit source ${s.number}`} onClick={() => setEditing(s)}>
                    <IconPencil size={14} />
                  </ActionIcon>
                  <ActionIcon
                    variant="subtle"
                    aria-label={`Move source ${s.number} up`}
                    disabled={s.number === 1}
                    onClick={() => move.mutate({ n: s.number, to: s.number - 1 })}
                  >
                    <IconArrowUp size={14} />
                  </ActionIcon>
                  <ActionIcon
                    variant="subtle"
                    aria-label={`Move source ${s.number} down`}
                    disabled={s.number === count}
                    onClick={() => move.mutate({ n: s.number, to: s.number + 1 })}
                  >
                    <IconArrowDown size={14} />
                  </ActionIcon>
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    aria-label={`Remove source ${s.number}`}
                    onClick={() =>
                      modals.openConfirmModal({
                        title: `Remove source ${s.number}?`,
                        children: <Text size="sm">{s.plan ? "Its channels come off the radio." : "Its channels stay on the radio."}</Text>,
                        labels: { confirm: "Remove", cancel: "Cancel" },
                        onConfirm: () => remove.mutate(s.number),
                      })
                    }
                  >
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Group align="end">
        <TextInput
          flex={1}
          label="New source query"
          placeholder="band:gmrs -is:repeater"
          disabled={plan !== null}
          value={query}
          onChange={(e) => setQuery(e.currentTarget.value)}
        />
        <Select
          label="or a plan"
          w={180}
          clearable
          data={(plans.data?.plans ?? []).filter((p) => p.status !== "archived").map((p) => p.name)}
          value={plan}
          onChange={setPlan}
        />
        <Select label="Sort" w={100} data={["name", "freq"]} value={sort} allowDeselect={false} disabled={plan !== null} onChange={(v) => v && setSort(v)} />
        {plan !== null && (
          <Select
            label="Plan numbers"
            w={170}
            data={[
              { value: "compact", label: "In order, no gaps" },
              { value: "match", label: "Match the plan's" },
            ]}
            value={planNumbers}
            allowDeselect={false}
            onChange={(v) => v && setPlanNumbers(v)}
          />
        )}
        <Select label="Section" w={200} data={sectionChoices} value={section} allowDeselect={false} onChange={(v) => setSection(v ?? "")} />
        <Button onClick={() => add.mutate()} loading={add.isPending} disabled={!query.trim() && !plan}>
          Add source
        </Button>
      </Group>
      <SourcePreview lib={lib} radio={radio} query={query} plan={plan} sort={sort} />
      {editing && <EditSource source={editing} onClose={() => setEditing(null)} />}
    </Stack>
  );
}

type Facets = { scan: string | null; power: string | null; override: string | null; fit: string | null; export: string | null };

const NO_FACETS: Facets = { scan: null, power: null, override: null, fit: null, export: null };

/** matchFacets applies the filters on this radio's own settings, which a query can't express. */
function matchFacets(m: Memory, f: Facets): boolean {
  if (f.scan && (f.scan === "on") !== m.scan) return false;
  if (f.power && (m.power ?? "none") !== f.power) return false;
  if (f.override === "any" && !(m.overrides.name || m.overrides.power || m.overrides.scan)) return false;
  if (f.override && f.override !== "any" && !m.overrides[f.override as "name" | "power" | "scan"]) return false;
  if (f.fit === "changed" && m.fit !== "shortened" && m.fit !== "cut") return false;
  if (f.fit === "cut" && m.fit !== "cut") return false;
  if (f.export === "left-out" && m.exported) return false;
  if (f.export === "rx-only" && !(m.exported && m.receive_only)) return false;
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
      <FilterMenu
        label="Programmed"
        options={[
          { value: "left-out", label: "not programmed" },
          { value: "rx-only", label: "receive only" },
        ]}
        value={props.facets.export}
        onChange={set("export")}
      />
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

/** EditSource changes a radio's source, previewing in its place what the
 * changed source would add before saving. */
function EditSource(props: { source: Source; onClose: () => void }) {
  const lib = useLib();
  const radio = useParams().radio ?? "";
  const client = useQueryClient();
  const s = props.source;
  const [query, setQuery] = useState(s.query);
  const [sort, setSort] = useState(s.sort);
  const [section, setSection] = useState(s.section_id);
  const [planNumbers, setPlanNumbers] = useState<string>(s.plan_numbers);
  const sections = useSections(lib, radio);
  const save = useMutation({
    mutationFn: () =>
      call(
        api.PATCH("/libraries/{lib}/radios/{radio}/sources/{n}", {
          params: { path: { lib, radio, n: s.number } },
          body: { ...(s.plan ? { plan_numbers: planNumbers as Source["plan_numbers"] } : { query, sort }), section },
        }),
      ),
    onSuccess: () => {
      ok("Source saved; sync to add its new matches");
      void client.invalidateQueries();
      props.onClose();
    },
    onError: failed,
  });
  return (
    <Modal opened onClose={props.onClose} title={`Edit source ${s.number}`} size="lg">
      <Stack>
        {s.plan ? (
          <>
            <Text size="sm">Plan: {s.plan}. A plan source follows its plan: its channels, order, and names.</Text>
            <Select
              label="Plan numbers"
              data={[
                { value: "compact", label: "In the plan's order, without gaps" },
                { value: "match", label: "Match the plan's: the section's first memory + Ch # − 1" },
              ]}
              value={planNumbers}
              allowDeselect={false}
              onChange={(v) => v && setPlanNumbers(v)}
            />
          </>
        ) : (
          <>
            <TextInput label="Query" ff="monospace" value={query} onChange={(e) => setQuery(e.currentTarget.value)} data-autofocus />
            <Select
              label="Sort"
              description="The order a sync adds its channels in; a sorted section keeps its own order"
              data={["name", "freq"]}
              value={sort}
              allowDeselect={false}
              onChange={(v) => v && setSort(v)}
            />
          </>
        )}
        <Select
          label="Section"
          description="Where a sync adds its channels; those already on the radio stay where they are"
          data={(sections.data?.sections ?? []).map((x) => ({ value: x.id, label: x.name }))}
          value={section}
          allowDeselect={false}
          onChange={(v) => v && setSection(v)}
        />
        {!s.plan && <SourcePreview lib={lib} radio={radio} query={query} sort={sort} number={s.number} maxHeight={320} />}
        <Text size="xs" c="dimmed">
          Channels the source already put on the radio stay; a sync adds the changed source's new matches.
        </Text>
        <Button onClick={() => save.mutate()} loading={save.isPending} disabled={!s.plan && query.trim() === ""}>
          Save source
        </Button>
      </Stack>
    </Modal>
  );
}

/** Programmed says how a memory is programmed when it isn't an ordinary
 * one: not at all (it takes no number), receive-only, with the transmit
 * tone alone, or off the air and so skipped in scans. */
function Programmed(props: { m: Memory }) {
  const { m } = props;
  if (!m.exported)
    return (
      <Tooltip label={`It takes no memory number, so the memories after it move up. ${m.not_programmed ?? ""}`} multiline w={280}>
        <Badge size="xs" color="red" variant="light">
          Not programmed: {shortReason(m.not_programmed ?? "")}
        </Badge>
      </Tooltip>
    );
  return (
    <Group gap={4} wrap="nowrap">
      {m.receive_only && (
        <Badge size="xs" color="gray" variant="light">
          RX only
        </Badge>
      )}
      {m.transmit_tone_only && (
        <Tooltip label="The radio can't hold this channel's receive tone, so it's programmed with the transmit tone alone">
          <Badge size="xs" color="yellow" variant="light">
            TX tone only
          </Badge>
        </Tooltip>
      )}
      {m.overflow && (
        <Tooltip label="Past the end of its section's size, so it spills into the memories after the section">
          <Badge size="xs" color="orange" variant="light">
            Past section end
          </Badge>
        </Tooltip>
      )}
      {m.channel.status === "off-air" && (
        <Badge size="xs" color="orange" variant="light">
          Off the air
        </Badge>
      )}
    </Group>
  );
}

/** shortReason is a not-programmed reason short enough for a badge. */
function shortReason(why: string): string {
  if (why.startsWith("retired")) return "retired";
  if (why.startsWith("the radio doesn't have")) return why.replace("the radio doesn't have", "no");
  if (why.startsWith("the radio's file can't hold it")) return "can't hold it";
  return why;
}

/** ProgrammedSummary counts what an export leaves out or programs
 * receive-only. */
function ProgrammedSummary(props: { memories: Memory[] }) {
  const out = props.memories.filter((m) => !m.exported).length;
  const rx = props.memories.filter((m) => m.exported && m.receive_only).length;
  const parts = [out > 0 && `${out} not programmed`, rx > 0 && `${rx} receive only`].filter(Boolean);
  return parts.length ? <>; {parts.join(", ")}</> : null;
}

/** useAutoScroll scrolls the page while something is dragged near the top
 * or bottom of the window, faster nearer the edge, since a browser's own
 * drag doesn't reliably scroll it. */
function useAutoScroll(active: boolean) {
  useEffect(() => {
    if (!active) return;
    const edge = 80;
    let speed = 0;
    let frame = 0;
    const onDragOver = (e: DragEvent) => {
      const h = window.innerHeight;
      if (e.clientY < edge) speed = -Math.ceil(((edge - e.clientY) / edge) * 24);
      else if (e.clientY > h - edge) speed = Math.ceil(((e.clientY - (h - edge)) / edge) * 24);
      else speed = 0;
    };
    const tick = () => {
      if (speed !== 0) window.scrollBy(0, speed);
      frame = requestAnimationFrame(tick);
    };
    document.addEventListener("dragover", onDragOver);
    frame = requestAnimationFrame(tick);
    return () => {
      document.removeEventListener("dragover", onDragOver);
      cancelAnimationFrame(frame);
    };
  }, [active]);
}

/** clearedStarts says which sections a move took starts from. */
function clearedStarts(names: string[] | undefined) {
  if (names?.length) warn([`Their starts no longer fit, so these now follow the section before them: ${names.join(", ")}. Set new starts in each section's settings.`]);
}

/** SourceMark marks a memory a source added: a sync takes it off when the
 * source no longer matches it. */
function SourceMark(props: { n: number; sources: Source[] }) {
  const src = props.sources.find((x) => x.number === props.n);
  const what = src ? (src.plan ? `plan ${src.plan}` : src.query) : "";
  return (
    <Tooltip label={`Added by source ${props.n}${what ? ` (${what})` : ""}; a sync takes it off if that source stops matching it`}>
      <Badge size="xs" variant="dot" color="grape" ml={6} tt="none" ff="text">
        S{props.n}
      </Badge>
    </Tooltip>
  );
}


/** markColor is the color a draft's change to a memory is marked in:
 * none for a memory only renumbered, since adding one renumbers those
 * after it. */
function markColor(mark: DraftMark): string | null {
  switch (mark.kind) {
    case "added":
      return "green";
    case "changed":
      return "orange";
    case "moved":
      return "blue";
  }
  return null;
}

/** DraftBadge says how a draft changes a memory from the saved radio. */
function DraftBadge({ mark }: { mark: DraftMark }) {
  switch (mark.kind) {
    case "added":
      return (
        <Badge size="xs" color="green" variant="light">
          new
        </Badge>
      );
    case "moved":
      return (
        <Badge size="xs" color="blue" variant="light">
          from {mark.from}
        </Badge>
      );
    case "changed":
      return (
        <Tooltip label={mark.changes.join("; ")} multiline maw={320}>
          <Badge size="xs" color="orange" variant="light" aria-label={`Changed: ${mark.changes.join("; ")}`}>
            changed
          </Badge>
        </Tooltip>
      );
    case "renumbered":
      return (
        <Text span size="xs" c="dimmed">
          was {mark.from ?? "–"}
        </Text>
      );
  }
}

/** TakenOff lists, after a radio's memories, those its draft takes off. */
function TakenOff({ channels }: { channels: Channel[] }) {
  return (
    <>
      <Table.Tr bg="var(--mantine-color-default-hover)">
        <Table.Td colSpan={14}>
          <Text size="sm" fw={500}>
            Taken off by this draft ({channels.length})
          </Text>
        </Table.Td>
      </Table.Tr>
      {channels.map((c) => (
        <Table.Tr
          key={`removed-${c.id}`}
          bg="var(--mantine-color-red-light)"
          style={{ boxShadow: "inset 4px 0 0 var(--mantine-color-red-filled)" }}
          data-draft="removed"
        >
          <Table.Td />
          <Table.Td />
          <Table.Td />
          <Table.Td>
            <Badge size="xs" color="red" variant="light">
              removed
            </Badge>
          </Table.Td>
          <Table.Td ff="monospace" td="line-through">
            {c.name}
          </Table.Td>
          <Table.Td />
          <Table.Td />
          <Table.Td />
          <Table.Td ff="monospace" td="line-through">
            {formatMHz(c.rx_hz)}
          </Table.Td>
          <Table.Td>
            {duplex(c.rx_hz, c.tx_hz)} {offsetMHz(c.rx_hz, c.tx_hz)}
          </Table.Td>
          <Table.Td>{toneSummary(c)}</Table.Td>
          <Table.Td colSpan={3} />
        </Table.Tr>
      ))}
    </>
  );
}

/** ScanSwitch turns scanning a memory on or off for this radio. An off-air
 * channel isn't scanned unless the radio says to, which the switch says. */
function ScanSwitch({ m, onToggle }: { m: Memory; onToggle: () => void }) {
  const sw = (
    <Group gap={4} wrap="nowrap">
      <Switch size="xs" aria-label={`Scan ${m.channel.name}`} checked={m.scan} onChange={onToggle} />
      {m.overrides.scan && "*"}
    </Group>
  );
  if (m.channel.status !== "off-air") return sw;
  return (
    <Tooltip
      multiline
      maw={260}
      label={m.scan ? "Off the air, but scanned on this radio as you set it" : "Off the air, so not scanned; turn it on to scan it on this radio anyway"}
    >
      <Group gap={4} wrap="nowrap">
        {sw}
        <Text span size="xs" c="dimmed">
          off air
        </Text>
      </Group>
    </Tooltip>
  );
}
