import { Alert, Badge, Button, Checkbox, Group, Modal, Select, Stack, Table, TagsInput, Text, Title, Tooltip } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { modals } from "@mantine/modals";
import { IconLink, IconNote, IconPlus, IconSearch, IconWorld } from "@tabler/icons-react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, call, type Channel, type Conversion, type Library, type Radio } from "../api/client";
import { ChannelEditor } from "../components/ChannelEditor";
import { QueryBar } from "../components/QueryBar";
import { SourcePreview } from "../components/SourcePreview";
import { useLib, useTags } from "../lib/library";
import { failed, ok, warn } from "../lib/notify";
import { duplex, formatMHz, offsetMHz, SERVICES, toneSummary } from "../lib/radio";

export function Channels() {
  const lib = useLib();
  const client = useQueryClient();
  // A link can open the page with a search, such as a system's channels.
  const [params] = useSearchParams();
  const [text, setText] = useState(params.get("q") ?? "");
  const [q] = useDebouncedValue(text, 300);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [editing, setEditing] = useState<Channel | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [tagAction, setTagAction] = useState<"add" | "remove" | null>(null);
  const [radioAction, setRadioAction] = useState(false);
  const [serviceAction, setServiceAction] = useState(false);
  const [matching, setMatching] = useState(false);
  const [sourcing, setSourcing] = useState(false);
  const tags = useTags(lib);

  const library = useQuery({
    queryKey: ["library", lib],
    queryFn: () => call<Library>(api.GET("/libraries/{lib}", { params: { path: { lib } } })),
  });
  const channels = useQuery({
    queryKey: ["channels", lib, q],
    queryFn: () => call<{ channels: Channel[]; total: number }>(api.GET("/libraries/{lib}/channels", { params: { path: { lib }, query: { q } } })),
    placeholderData: keepPreviousData,
  });
  const maintainer = library.data?.shared === true;
  // In the shared list itself, channels change only as corrections.
  const readOnly = maintainer;
  const rows = channels.data?.channels ?? [];
  const ids = [...selected];
  const select = { ids };
  const after = (msg: string) => {
    ok(msg);
    setSelected(new Set());
    void client.invalidateQueries();
  };

  const remove = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/channels/bulk-delete", { params: { path: { lib } }, body: { select } })),
    onSuccess: (r) => after(`Deleted ${r.channels.length} channels`),
    onError: failed,
  });
  const toggle = (id: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const all = rows.length > 0 && rows.every((c) => selected.has(c.id));

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Channels</Title>
        {!readOnly && (
          <Group>
            <Button variant="light" leftSection={<IconLink size={16} />} onClick={() => setMatching(true)}>
              Link to shared list
            </Button>
            <Button
              leftSection={<IconPlus size={16} />}
              onClick={() => {
                setEditing(null);
                setEditorOpen(true);
              }}
            >
              New channel
            </Button>
          </Group>
        )}
      </Group>
      <Group align="start">
        <QueryBar value={text} onChange={setText} tags={tags} source={!maintainer} />
        {!readOnly && text.trim() !== "" && (
          <Button variant="light" leftSection={<IconSearch size={16} />} onClick={() => setSourcing(true)}>
            Add search as source
          </Button>
        )}
      </Group>
      {channels.error && <Alert color="red">{channels.error.message}</Alert>}
      {maintainer && (
        <Alert>
          Open a channel to correct it; every correction asks for a reason, is kept in its history, and reaches everyone who uses it.
        </Alert>
      )}
      {!maintainer && (
        <Text size="xs" c="dimmed">
          Showing the channels you use: your own, and shared channels you've tagged or put on a radio. Source widens it to the whole shared list.
          <IconWorld size={12} style={{ margin: "0 2px 0 6px", verticalAlign: "middle" }} />
          marks a shared channel, and
          <IconNote size={12} style={{ margin: "0 2px 0 4px", verticalAlign: "middle" }} />
          your notes or tags on one.
        </Text>
      )}
      {selected.size > 0 && !maintainer && (
        <Group>
          <Text size="sm">{selected.size} selected</Text>
          <Button size="xs" variant="light" onClick={() => setTagAction("add")}>
            Add tags
          </Button>
          <Button size="xs" variant="light" onClick={() => setTagAction("remove")}>
            Remove tags
          </Button>
          <Button size="xs" variant="light" onClick={() => setServiceAction(true)}>
            Set service
          </Button>
          <Button size="xs" variant="light" onClick={() => setRadioAction(true)}>
            Add to radio
          </Button>
          <Button
            size="xs"
            color="red"
            variant="light"
            onClick={() =>
              modals.openConfirmModal({
                title: `Delete ${selected.size} channels?`,
                children: (
                  <Text size="sm">Your own channels come off every radio too. For shared channels, only your tags and notes go.</Text>
                ),
                labels: { confirm: "Delete", cancel: "Cancel" },
                confirmProps: { color: "red" },
                onConfirm: () => remove.mutate(),
              })
            }
          >
            Delete
          </Button>
          <Button size="xs" variant="subtle" onClick={() => setSelected(new Set())}>
            Clear
          </Button>
        </Group>
      )}
      <Text size="sm" c="dimmed">
        {channels.data ? `${channels.data.total} channels` : "Loading…"}
      </Text>
      <Table.ScrollContainer minWidth={800}>
        <Table striped highlightOnHover stickyHeader>
          <Table.Thead>
            <Table.Tr>
              <Table.Th w={36}>
                <Checkbox
                  aria-label="Select all"
                  checked={all}
                  indeterminate={selected.size > 0 && !all}
                  onChange={() => setSelected(all ? new Set() : new Set(rows.map((c) => c.id)))}
                />
              </Table.Th>
              <Table.Th>Name</Table.Th>
              <Table.Th>RX MHz</Table.Th>
              <Table.Th>Duplex</Table.Th>
              <Table.Th>Offset</Table.Th>
              <Table.Th>Tone</Table.Th>
              <Table.Th>Mode</Table.Th>
              <Table.Th>Service</Table.Th>
              <Table.Th>Tags</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((c) => (
              <Table.Tr
                key={c.id}
                style={{ cursor: "pointer" }}
                onClick={() => {
                  setEditing(c);
                  setEditorOpen(true);
                }}
              >
                <Table.Td onClick={(e) => e.stopPropagation()}>
                  <Checkbox aria-label={`Select ${c.name}`} checked={selected.has(c.id)} onChange={() => toggle(c.id)} />
                </Table.Td>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    {c.name}
                    {c.link && (
                      <Tooltip label={c.link.changed ? "Shared; changed since you last looked" : "Shared channel"}>
                        <IconWorld size={14} color={c.link.changed ? "orange" : "gray"} aria-label="Shared channel" />
                      </Tooltip>
                    )}
                    {c.link && (c.tags.length > 0 || c.notes !== "" || c.short_names.length > 0) && (
                      <Tooltip label="Your notes or tags">
                        <IconNote size={14} color="gray" aria-label="Your notes or tags" />
                      </Tooltip>
                    )}
                    {c.status !== "active" && (
                      <Badge size="xs" color={c.status === "retired" ? "gray" : "yellow"}>
                        {c.status}
                      </Badge>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td ff="monospace">{formatMHz(c.rx_hz)}</Table.Td>
                <Table.Td>{duplex(c.rx_hz, c.tx_hz)}</Table.Td>
                <Table.Td ff="monospace">{offsetMHz(c.rx_hz, c.tx_hz)}</Table.Td>
                <Table.Td>{toneSummary(c)}</Table.Td>
                <Table.Td>{c.mode}</Table.Td>
                <Table.Td>{c.service}</Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    {[...(c.link?.tags ?? []), ...c.tags].map((t) => (
                      <Badge key={t} size="xs" variant="light" tt="none">
                        {t}
                      </Badge>
                    ))}
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      <ChannelEditor channel={editing} opened={editorOpen} onClose={() => setEditorOpen(false)} readOnly={maintainer} maintainer={maintainer} />
      <MatchModal opened={matching} onClose={() => setMatching(false)} onDone={after} />
      <SourceModal opened={sourcing} query={text} onClose={() => setSourcing(false)} onDone={after} />
      <TagModal action={tagAction} ids={ids} onClose={() => setTagAction(null)} onDone={after} />
      <ServiceModal
        opened={serviceAction}
        ids={ids}
        links={rows.filter((c) => selected.has(c.id) && c.link)}
        onClose={() => setServiceAction(false)}
        onDone={after}
      />
      <AddToRadioModal opened={radioAction} ids={ids} onClose={() => setRadioAction(false)} onDone={after} />
    </Stack>
  );
}

function TagModal(props: { action: "add" | "remove" | null; ids: string[]; onClose: () => void; onDone: (msg: string) => void }) {
  const lib = useLib();
  const [tags, setTags] = useState<string[]>([]);
  const known = useTags(lib);
  const save = useMutation({
    mutationFn: () =>
      call(
        api.POST("/libraries/{lib}/channels/bulk-update", {
          params: { path: { lib } },
          body: { select: { ids: props.ids }, edit: { tags: props.action === "add" ? { add: tags } : { remove: tags } } },
        }),
      ),
    onSuccess: (r) => {
      props.onClose();
      setTags([]);
      warn(r.warnings);
      props.onDone(`Updated ${r.channels.length} channels`);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.action !== null} onClose={props.onClose} title={props.action === "add" ? "Add tags" : "Remove tags"}>
      <Stack>
        <TagsInput
          label="Tags"
          description={props.action === "add" ? "Pick a tag in use, or type a new one" : undefined}
          data={known}
          value={tags}
          onChange={setTags}
          data-autofocus
        />
        <Button onClick={() => save.mutate()} loading={save.isPending} disabled={tags.length === 0}>
          {props.action === "add" ? "Add" : "Remove"} on {props.ids.length} channels
        </Button>
      </Stack>
    </Modal>
  );
}

/** ServiceModal sets the service on the selected channels. A link's
 * service comes from the shared list, so links are left out. */
function ServiceModal(props: { opened: boolean; ids: string[]; links: Channel[]; onClose: () => void; onDone: (msg: string) => void }) {
  const lib = useLib();
  const [service, setService] = useState<string | null>(null);
  const linked = new Set(props.links.map((c) => c.id));
  const ids = props.ids.filter((id) => !linked.has(id));
  const save = useMutation({
    mutationFn: () =>
      call(api.POST("/libraries/{lib}/channels/bulk-update", { params: { path: { lib } }, body: { select: { ids }, edit: { service: service! } } })),
    onSuccess: (r) => {
      props.onClose();
      setService(null);
      warn(r.warnings);
      props.onDone(`Set the service on ${r.channels.length} channels`);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Set service">
      <Stack>
        <Select
          label="Service"
          description="Weather and public-safety services also make them receive-only"
          data={SERVICES.map((s) => ({ value: s, label: s || "unspecified" }))}
          value={service}
          onChange={setService}
          data-autofocus
        />
        {props.links.length > 0 && (
          <Text size="sm" c="dimmed">
            {props.links.length} of them are shared channels; their service comes from the shared list, so they're left out.
          </Text>
        )}
        <Button onClick={() => save.mutate()} loading={save.isPending} disabled={service === null || ids.length === 0}>
          Set on {ids.length} channels
        </Button>
      </Stack>
    </Modal>
  );
}

function AddToRadioModal(props: { opened: boolean; ids: string[]; onClose: () => void; onDone: (msg: string) => void }) {
  const lib = useLib();
  const [radio, setRadio] = useState<string | null>(null);
  const radios = useQuery({
    queryKey: ["radios", lib],
    queryFn: () => call<{ radios: Radio[] }>(api.GET("/libraries/{lib}/radios", { params: { path: { lib } } })),
    enabled: props.opened,
  });
  const add = useMutation({
    mutationFn: () =>
      call(api.POST("/libraries/{lib}/radios/{radio}/channels", { params: { path: { lib, radio: radio! } }, body: { ids: props.ids } })),
    onSuccess: (r) => {
      props.onClose();
      const skipped = r.skipped.length + r.wrong_mode.length;
      props.onDone(`Added ${r.added} channels to ${radio}${skipped ? `; ${skipped} skipped (outside its bands or modes)` : ""}`);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Add to radio">
      <Stack>
        <Select label="Radio" data={(radios.data?.radios ?? []).map((r) => r.name)} value={radio} onChange={setRadio} />
        <Button onClick={() => add.mutate()} loading={add.isPending} disabled={!radio}>
          Add {props.ids.length} channels
        </Button>
      </Stack>
    </Modal>
  );
}

/** MatchModal finds the library's private channels that match a shared
 * channel on the air, and turns the ones picked into links to it. */
function MatchModal(props: { opened: boolean; onClose: () => void; onDone: (msg: string) => void }) {
  const lib = useLib();
  const matches = useQuery({
    queryKey: ["matches", lib],
    queryFn: () =>
      call<{ conversions: Conversion[] }>(api.POST("/libraries/{lib}/shared/convert", { params: { path: { lib } }, body: { dry_run: true } })),
    enabled: props.opened,
    gcTime: 0,
  });
  const found = matches.data?.conversions ?? [];
  const [picked, setPicked] = useState<Set<string>>(new Set());
  useEffect(() => setPicked(new Set(found.map((m) => m.channel.id))), [matches.data]);
  const link = useMutation({
    mutationFn: () =>
      call(api.POST("/libraries/{lib}/shared/convert", { params: { path: { lib } }, body: { ids: [...picked] } })),
    onSuccess: (r) => {
      props.onClose();
      props.onDone(`Linked ${r.conversions.length} channels to the shared list`);
    },
    onError: failed,
  });
  const toggle = (id: string) =>
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Link to shared list" size="xl">
      <Stack>
        <Text size="sm">
          These channels match a shared channel on the air: the same frequencies, tones, and tone squelch, and the same service or none. A
          link follows the shared channel's corrections. It stays on your radios, keeps your tags and notes, and keeps a different name as
          a short name.
        </Text>
        {matches.error && <Alert color="red">{matches.error.message}</Alert>}
        {matches.isLoading ? (
          <Text size="sm">Looking for matches…</Text>
        ) : found.length === 0 ? (
          <Text size="sm">None of your channels match a shared channel.</Text>
        ) : (
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>
                  <Checkbox
                    aria-label="Pick all"
                    checked={picked.size === found.length}
                    indeterminate={picked.size > 0 && picked.size < found.length}
                    onChange={() => setPicked(picked.size === found.length ? new Set() : new Set(found.map((m) => m.channel.id)))}
                  />
                </Table.Th>
                <Table.Th>Your channel</Table.Th>
                <Table.Th>Shared channel</Table.Th>
                <Table.Th>Frequency</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {found.map((m) => (
                <Table.Tr key={m.channel.id}>
                  <Table.Td>
                    <Checkbox aria-label={`Link ${m.channel.name}`} checked={picked.has(m.channel.id)} onChange={() => toggle(m.channel.id)} />
                  </Table.Td>
                  <Table.Td>{m.channel.name}</Table.Td>
                  <Table.Td>
                    {m.shared.name}
                    {m.shared.status && m.shared.status !== "active" && (
                      <Badge ml="xs" size="sm" color="orange" variant="light">
                        {m.shared.status}
                      </Badge>
                    )}
                  </Table.Td>
                  <Table.Td>
                    {formatMHz(m.channel.rx_hz)} {toneSummary(m.channel)}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
        <Button onClick={() => link.mutate()} loading={link.isPending} disabled={picked.size === 0}>
          Link {picked.size} channels
        </Button>
      </Stack>
    </Modal>
  );
}

/** SourceModal adds the current search to a radio as a source, showing
 * first what it would add, and optionally syncs it. */
function SourceModal(props: { opened: boolean; query: string; onClose: () => void; onDone: (msg: string) => void }) {
  const lib = useLib();
  const [radio, setRadio] = useState<string | null>(null);
  const [sort, setSort] = useState("name");
  const radios = useQuery({
    queryKey: ["radios", lib],
    queryFn: () => call<{ radios: Radio[] }>(api.GET("/libraries/{lib}/radios", { params: { path: { lib } } })),
    enabled: props.opened,
  });
  const add = useMutation({
    mutationFn: async (andSync: boolean) => {
      const path = { lib, radio: radio! };
      await call(api.POST("/libraries/{lib}/radios/{radio}/sources", { params: { path }, body: { query: props.query.trim(), sort } }));
      if (!andSync) return `Added the search to ${radio} as a source; sync to add its channels`;
      const r = await call(api.POST("/libraries/{lib}/radios/{radio}/sync", { params: { path }, body: {} }));
      return `Added the search to ${radio} as a source and synced: ${r.added} channels added`;
    },
    onSuccess: (msg) => {
      props.onClose();
      props.onDone(msg);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Add search as source" size="lg">
      <Stack>
        <Text size="sm">
          A source is a saved search on a radio: each sync adds its new matches to the radio, in its own part of the list. Search:{" "}
          <Text span ff="monospace">
            {props.query.trim()}
          </Text>
        </Text>
        <Group grow>
          <Select label="Radio" placeholder="Pick a radio" data={(radios.data?.radios ?? []).map((r) => r.name)} value={radio} onChange={setRadio} />
          <Select label="Sort" data={["name", "freq"]} value={sort} allowDeselect={false} onChange={(v) => v && setSort(v)} />
        </Group>
        <SourcePreview lib={lib} radio={radio} query={props.query} sort={sort} maxHeight={320} />
        <Group>
          <Button onClick={() => add.mutate(true)} loading={add.isPending} disabled={radio === null}>
            Add source and sync
          </Button>
          <Button variant="light" onClick={() => add.mutate(false)} loading={add.isPending} disabled={radio === null}>
            Add source only
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
