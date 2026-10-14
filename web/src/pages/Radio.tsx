import { ActionIcon, Alert, Badge, Spoiler, Button, Group, List, Modal, NumberInput, Select, Stack, Switch, Table, Tabs, Text, TextInput, Title, Tooltip } from "@mantine/core";
import { modals } from "@mantine/modals";
import { IconArrowUp, IconDownload, IconRefresh, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { api, call, download, type Memory, type Radio, type Source } from "../api/client";
import { useLib } from "../lib/library";
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
  const plan = useQuery({
    queryKey: ["memories", lib, radio],
    queryFn: () => call<{ memories: Memory[]; warnings: string[] }>(api.GET("/libraries/{lib}/radios/{radio}/memories", { params: { path } })),
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
          <Table.ScrollContainer minWidth={800}>
            <Table striped>
              <Table.Thead>
                <Table.Tr>
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
                {(plan.data?.memories ?? []).map((m) => (
                  <Table.Tr key={m.channel.id} opacity={m.exported ? 1 : 0.5}>
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
            § starts a section at that memory; * marks this radio's override. Add channels from the Channels page, or with sources.
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
