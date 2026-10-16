import { Alert, Badge, Button, Group, List, Stack, Table, Text, Title } from "@mantine/core";
import { modals } from "@mantine/modals";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call, showDraft, type Radio } from "../api/client";
import { formatMHz } from "../lib/radio";
import { failed, ok } from "../lib/notify";
import { ReasonModal } from "./Records";

const when = (at: string) => new Date(at).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });

/** RevisionsTab lists a radio's revisions, saves one, compares one with
 * the radio as it is now, and restores one into the draft. */
export function RevisionsTab(props: { lib: string; radio: string; info: Radio | undefined }) {
  const client = useQueryClient();
  const path = { lib: props.lib, radio: props.radio };
  const [saving, setSaving] = useState(false);
  const [picked, setPicked] = useState<number | null>(null);
  const revisions = useQuery({
    queryKey: ["radio-revisions", props.lib, props.radio],
    queryFn: () => call(api.GET("/libraries/{lib}/radios/{radio}/revisions", { params: { path } })),
  });
  const save = useMutation({
    mutationFn: (reason: string) => call(api.POST("/libraries/{lib}/radios/{radio}/revisions", { params: { path }, body: { reason } })),
    onSuccess: (r) => {
      ok(`Saved revision ${r.number}`);
      setSaving(false);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const revs = [...(revisions.data?.revisions ?? [])].reverse();
  const info = props.info;
  return (
    <Stack>
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          A revision is the saved radio as it was: saved by hand, or by an export when the radio changed. The radio needs programming when it
          differs from its last exported revision.
        </Text>
        <Button variant="light" onClick={() => setSaving(true)} disabled={info != null && !info.changed_since_revision}>
          Save revision
        </Button>
      </Group>
      {info?.draft && <Alert color="yellow">Revisions are of the saved radio; the open draft isn't in them until it's saved.</Alert>}
      {revs.length === 0 && <Text c="dimmed">No revisions yet. Exporting the radio saves its first.</Text>}
      {revs.length > 0 && (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Revision</Table.Th>
              <Table.Th>Saved</Table.Th>
              <Table.Th>Reason</Table.Th>
              <Table.Th>Memories</Table.Th>
              <Table.Th />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {revs.map((r) => (
              <Table.Tr
                key={r.number}
                style={{ cursor: "pointer" }}
                bg={picked === r.number ? "var(--mantine-color-blue-light)" : undefined}
                onClick={() => setPicked(r.number)}
              >
                <Table.Td>{r.number}</Table.Td>
                <Table.Td>{when(r.at)}</Table.Td>
                <Table.Td>{r.reason || "no reason given"}</Table.Td>
                <Table.Td>{r.memories}</Table.Td>
                <Table.Td>
                  {r.exported && (
                    <Badge size="sm" variant="light" color="green">
                      Exported
                    </Badge>
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {picked !== null && <RevisionCompare lib={props.lib} radio={props.radio} n={picked} draft={info?.draft != null} />}
      <ReasonModal opened={saving} title={`Save a revision of ${props.radio}`} loading={save.isPending} onClose={() => setSaving(false)} onConfirm={(r) => save.mutate(r)} />
    </Stack>
  );
}

/** RevisionCompare shows what changed since a revision, and restores it
 * into the radio's draft. */
function RevisionCompare(props: { lib: string; radio: string; n: number; draft: boolean }) {
  const client = useQueryClient();
  const path = { lib: props.lib, radio: props.radio, n: props.n };
  const cmp = useQuery({
    queryKey: ["radio-revision-compare", props.lib, props.radio, props.n],
    queryFn: () => call(api.GET("/libraries/{lib}/radios/{radio}/revisions/{n}/compare", { params: { path } })),
  });
  const restore = useMutation({
    mutationFn: () =>
      call(api.POST("/libraries/{lib}/radios/{radio}/revisions/{n}/restore", { params: { path }, body: { replace_draft: props.draft } })),
    onSuccess: (d) => {
      showDraft(props.lib, props.radio, d?.id ?? null);
      ok(`Put revision ${props.n} into ${props.radio}'s draft; review it on the Memories tab, then save or discard`);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const c = cmp.data?.changes;
  const mhz = (hz: number) => formatMHz(hz);
  const lines: string[] = [];
  if (c) {
    for (const x of c.added) lines.push(`Added ${x.name} (${mhz(x.rx_hz)})`);
    for (const x of c.removed) lines.push(`Removed ${x.name} (${mhz(x.rx_hz)})`);
    for (const m of c.moved) lines.push(`Moved ${m.channel.name} from ${m.from_section} to ${m.to_section}`);
    if (c.renumbered.length) lines.push(`${c.renumbered.length} memories renumbered`);
    for (const x of c.changed) lines.push(`${x.name}: another name, power, or scan`);
    for (const x of c.channels ?? []) for (const change of x.changes) lines.push(`${x.channel.name}: ${change}`);
    for (const s of c.sections_added) lines.push(`Section added: ${s}`);
    for (const s of c.sections_removed) lines.push(`Section removed: ${s}`);
    for (const s of c.sources_added) lines.push(`Source added: ${s}`);
    for (const s of c.sources_removed) lines.push(`Source removed: ${s}`);
  }
  return (
    <Stack gap="xs">
      <Title order={4}>Since revision {props.n}</Title>
      {cmp.error && <Alert color="red">{cmp.error.message}</Alert>}
      {c && (lines.length ? <List size="sm">{lines.map((l, i) => <List.Item key={i}>{l}</List.Item>)}</List> : <Text size="sm">Nothing changed since.</Text>)}
      <Group>
        <Button
          variant="default"
          loading={restore.isPending}
          onClick={() =>
            props.draft
              ? modals.openConfirmModal({
                  title: `Replace ${props.radio}'s draft?`,
                  children: <Text size="sm">Restoring puts revision {props.n} into the draft, replacing the changes it holds now.</Text>,
                  labels: { confirm: "Replace the draft", cancel: "Cancel" },
                  confirmProps: { color: "red" },
                  onConfirm: () => restore.mutate(),
                })
              : restore.mutate()
          }
        >
          Restore into draft
        </Button>
        <Text size="xs" c="dimmed">
          Its sections, sources, and memories; settings stay, and channels are as they are now.
        </Text>
      </Group>
    </Stack>
  );
}
