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
import { useLib, useTags } from "../lib/library";
import { failed, ok } from "../lib/notify";
import { duplex, formatMHz, offsetMHz, toneSummary } from "../lib/radio";

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
  const readOnly = library.data?.shared === true || shared;
  const rows = channels.data?.channels ?? [];
  const ids = [...selected];
  const select = { ids };
  const after = (msg: string) => {
    ok(msg);
    setSelected(new Set());
    void client.invalidateQueries();
  };

  const link = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/channels/link", { params: { path: { lib } }, body: { select } })),
    onSuccess: (r) => after(`Linked ${r.linked.length} channels${r.already.length ? `; ${r.already.length} already linked` : ""}`),
    onError: failed,
  });
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
      {library.data?.shared && <Alert>
          The shared list's channels change through rflib shared commands. A channel's organizations and network nodes can be corrected here, in
          its editor.
        </Alert>}
      {selected.size > 0 && (
        <Group>
          <Text size="sm">{selected.size} selected</Text>
          {shared ? (
            <Button size="xs" leftSection={<IconLink size={14} />} onClick={() => link.mutate()} loading={link.isPending}>
              Link into {lib}
            </Button>
          ) : (
            !readOnly && (
              <>
                <Button size="xs" variant="light" onClick={() => setTagAction("add")}>
                  Add tags
                </Button>
                <Button size="xs" variant="light" onClick={() => setTagAction("remove")}>
                  Remove tags
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
            )
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
      <ChannelEditor channel={editing} opened={editorOpen} onClose={() => setEditorOpen(false)} readOnly={readOnly} />
      <TagModal action={tagAction} ids={ids} onClose={() => setTagAction(null)} onDone={after} />
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
