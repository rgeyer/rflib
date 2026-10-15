import { Alert, Badge, Button, Checkbox, Group, Modal, Select, Stack, Switch, Table, TagsInput, Text, Title, Tooltip } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { modals } from "@mantine/modals";
import { IconLink, IconPlus } from "@tabler/icons-react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, call, type Channel, type Library, type Radio } from "../api/client";
import { ChannelEditor } from "../components/ChannelEditor";
import { QueryBar } from "../components/QueryBar";
import { useLib, usePrivateLibraries, useTags } from "../lib/library";
import { failed, ok, warn } from "../lib/notify";
import { duplex, formatMHz, offsetMHz, SERVICES, toneSummary } from "../lib/radio";

export function Channels() {
  const lib = useLib();
  const client = useQueryClient();
  // A link can open the page with a search, such as a system's channels.
  const [params] = useSearchParams();
  const [text, setText] = useState(params.get("q") ?? "");
  const [shared, setShared] = useState(false);
  const [q] = useDebouncedValue((shared ? "in:shared " : "") + text, 300);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [editing, setEditing] = useState<Channel | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [tagAction, setTagAction] = useState<"add" | "remove" | null>(null);
  const [radioAction, setRadioAction] = useState(false);
  const [linking, setLinking] = useState(false);
  const [serviceAction, setServiceAction] = useState(false);
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
  // The rows are shared channels, which change only as corrections.
  const readOnly = maintainer || shared;
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
          <Button
            leftSection={<IconPlus size={16} />}
            onClick={() => {
              setEditing(null);
              setEditorOpen(true);
            }}
          >
            New channel
          </Button>
        )}
      </Group>
      <Group align="start">
        <QueryBar value={text} onChange={setText} tags={tags} />
        {!library.data?.shared && (
          <Switch label="Search the shared list" checked={shared} onChange={(e) => setShared(e.currentTarget.checked)} />
        )}
      </Group>
      {channels.error && <Alert color="red">{channels.error.message}</Alert>}
      {maintainer && (
        <Alert>
          Select channels to link them into your library. Open one to correct it; every correction asks for a reason, is kept in its history,
          and reaches everyone who links it.
        </Alert>
      )}
      {selected.size > 0 && (
        <Group>
          <Text size="sm">{selected.size} selected</Text>
          {readOnly ? (
            <Button size="xs" leftSection={<IconLink size={14} />} onClick={() => setLinking(true)}>
              Link into my library
            </Button>
          ) : (
            <>
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
                    children: <Text size="sm">They come off every radio too.</Text>,
                    labels: { confirm: "Delete", cancel: "Cancel" },
                    confirmProps: { color: "red" },
                    onConfirm: () => remove.mutate(),
                  })
                }
              >
                Delete
              </Button>
            </>
          )}
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
                      <Tooltip label={c.link.changed ? "Linked; the shared channel changed" : "Linked to a shared channel"}>
                        <IconLink size={14} color={c.link.changed ? "orange" : "gray"} />
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
      <ChannelEditor channel={editing} opened={editorOpen} onClose={() => setEditorOpen(false)} readOnly={readOnly} maintainer={maintainer} />
      <LinkModal opened={linking} ids={ids} current={maintainer ? null : lib} onClose={() => setLinking(false)} onDone={after} />
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
      props.onDone(`Updated ${r.channels.length} channels`);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.action !== null} onClose={props.onClose} title={props.action === "add" ? "Add tags" : "Remove tags"}>
      <Stack>
        <TagsInput label="Tags" value={tags} onChange={setTags} data-autofocus />
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
            {props.links.length} of them are linked; their service comes from the shared list, so they're left out.
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

/** LinkModal links shared channels into a private library (current, when
 * one is open), and optionally puts the links on one of its radios. */
function LinkModal(props: { opened: boolean; ids: string[]; current: string | null; onClose: () => void; onDone: (msg: string) => void }) {
  const libraries = usePrivateLibraries();
  const [chosen, setChosen] = useState<string | null>(null);
  const [radio, setRadio] = useState<string | null>(null);
  const target = props.current ?? chosen ?? libraries[0]?.name ?? null;
  const radios = useQuery({
    queryKey: ["radios", target],
    queryFn: () => call<{ radios: Radio[] }>(api.GET("/libraries/{lib}/radios", { params: { path: { lib: target! } } })),
    enabled: props.opened && target !== null,
  });
  const link = useMutation({
    mutationFn: async () => {
      const lib = target!;
      const r = await call(api.POST("/libraries/{lib}/channels/link", { params: { path: { lib } }, body: { select: { ids: props.ids } } }));
      let msg = `Linked ${r.linked.length} channels${r.already.length ? `; ${r.already.length} already linked` : ""}`;
      if (radio) {
        const a = await call(api.POST("/libraries/{lib}/radios/{radio}/channels", { params: { path: { lib, radio } }, body: { ids: r.link_ids } }));
        const skipped = a.skipped.length + a.wrong_mode.length;
        msg += `. Added ${a.added} to ${radio}${skipped ? `; ${skipped} skipped (outside its bands or modes)` : ""}`;
      }
      return msg;
    },
    onSuccess: (msg) => {
      props.onClose();
      setRadio(null);
      props.onDone(msg);
    },
    onError: failed,
  });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Link into my library">
      <Stack>
        <Text size="sm">Links follow the shared channels' corrections. Your tags, notes, power, and skip stay your own.</Text>
        {props.current === null && libraries.length > 1 && (
          <Select
            label="Library"
            data={libraries.map((l) => ({ value: l.name, label: l.default ? `${l.name} (default)` : l.name }))}
            value={target}
            allowDeselect={false}
            onChange={(v) => {
              setChosen(v);
              setRadio(null);
            }}
          />
        )}
        <Select
          label="Also add to a radio"
          placeholder="None"
          clearable
          data={(radios.data?.radios ?? []).map((r) => r.name)}
          value={radio}
          onChange={setRadio}
        />
        <Button onClick={() => link.mutate()} loading={link.isPending} disabled={target === null}>
          Link {props.ids.length} channels
        </Button>
      </Stack>
    </Modal>
  );
}
