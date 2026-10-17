import { ActionIcon, Anchor, Badge, Button, Group, NumberInput, Select, Stack, Table, Text, Textarea, TextInput, Title } from "@mantine/core";
import { IconArrowDown, IconArrowUp, IconPencil, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, call, type Plan, type PlanEntry, type PlanRow, type RecordRevision } from "../api/client";
import { useMaintainer } from "../lib/auth";
import { History, ReasonModal, SharedChannelPicker } from "../components/Records";
import { enc, useLib } from "../lib/library";
import { failed, ok } from "../lib/notify";
import { PLAN_STATUSES, planDates, usePlans } from "../lib/records";
import { formatMHz, shortTone } from "../lib/radio";

type PlanForm = { name: string; description: string; starts: string; ends: string; status: string };

function PlanFields(props: { form: PlanForm; onChange: (f: PlanForm) => void }) {
  const set = (k: keyof PlanForm) => (v: string) => props.onChange({ ...props.form, [k]: v });
  return (
    <>
      <TextInput label="Name" required value={props.form.name} onChange={(e) => set("name")(e.currentTarget.value)} />
      <Textarea label="Description" autosize minRows={2} value={props.form.description} onChange={(e) => set("description")(e.currentTarget.value)} />
      <Group grow>
        <TextInput label="Starts" placeholder="YYYY-MM-DD" value={props.form.starts} onChange={(e) => set("starts")(e.currentTarget.value)} />
        <TextInput label="Ends" placeholder="YYYY-MM-DD" value={props.form.ends} onChange={(e) => set("ends")(e.currentTarget.value)} />
      </Group>
      <Select label="Status" data={PLAN_STATUSES} value={props.form.status} allowDeselect={false} onChange={(v) => v && set("status")(v)} />
    </>
  );
}

function toBody(f: PlanForm) {
  return { name: f.name, description: f.description, status: f.status, starts: f.starts.trim() || null, ends: f.ends.trim() || null };
}

const statusColor: Record<string, string> = { draft: "gray", published: "green", archived: "dark" };

/** Plans lists communications plans, such as an event's channels. */
export function Plans() {
  const maintainer = useMaintainer();
  const lib = useLib();
  const navigate = useNavigate();
  const client = useQueryClient();
  const plans = usePlans();
  const [form, setForm] = useState<PlanForm | null>(null);
  const create = useMutation({
    mutationFn: (reason: string) => call(api.POST("/plans", { body: { ...toBody(form!), reason } })),
    onSuccess: (r) => {
      ok(`Started ${r.plan.name}`);
      setForm(null);
      void client.invalidateQueries();
      navigate(`/l/${enc(lib)}/plans/${enc(r.plan.name)}`);
    },
    onError: failed,
  });
  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Plans</Title>
        {maintainer && <Button onClick={() => setForm({ name: "", description: "", starts: "", ends: "", status: "draft" })}>New plan</Button>}
      </Group>
      <Text size="sm" c="dimmed">
        Communications plans: shared channels with their names and assignments, laid out as ICS 205 lists them. Program one onto a radio from the
        radio's Sources tab.
      </Text>
      <Table highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th>Dates</Table.Th>
            <Table.Th>Entries</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(plans.data?.plans ?? []).map((p) => (
            <Table.Tr key={p.id}>
              <Table.Td>
                <Anchor component={Link} to={enc(p.name)}>
                  {p.name}
                </Anchor>
              </Table.Td>
              <Table.Td>
                <Badge color={statusColor[p.status]} variant="light">
                  {p.status}
                </Badge>
              </Table.Td>
              <Table.Td>{planDates(p)}</Table.Td>
              <Table.Td>{p.entries.length}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {plans.data?.plans.length === 0 && <Text c="dimmed">No plans yet.</Text>}
      <ReasonModal opened={form !== null} title="New plan" loading={create.isPending} onClose={() => setForm(null)} onConfirm={(r) => create.mutate(r)}>
        {form && <PlanFields form={form} onChange={setForm} />}
      </ReasonModal>
    </Stack>
  );
}

type EntryForm = { title: string; at: number; entry: PlanEntry; isNew: boolean };

/** PlanPage shows a plan as ICS 205 does, and edits its entries. */
export function PlanPage() {
  const maintainer = useMaintainer();
  const lib = useLib();
  const name = useParams().plan ?? "";
  const client = useQueryClient();
  const navigate = useNavigate();
  const detail = useQuery({
    queryKey: ["plan", name],
    queryFn: () => call<{ plan: Plan; rows: PlanRow[] }>(api.GET("/plans/{plan}", { params: { path: { plan: name } } })),
  });
  const history = useQuery({
    queryKey: ["plan-history", name],
    queryFn: () => call<{ revisions: RecordRevision[] }>(api.GET("/plans/{plan}/history", { params: { path: { plan: name } } })),
  });
  const [form, setForm] = useState<PlanForm | null>(null);
  // A pending change to the entries, saved with a reason.
  const [pending, setPending] = useState<{ title: string; entries: PlanEntry[] } | null>(null);
  const [editing, setEditing] = useState<EntryForm | null>(null);
  const [query, setQuery] = useState("");
  const save = useMutation({
    mutationFn: (reason: string) => call(api.PATCH("/plans/{plan}", { params: { path: { plan: name } }, body: { ...toBody(form!), reason } })),
    onSuccess: (r) => {
      ok("Saved");
      setForm(null);
      void client.invalidateQueries();
      if (r.plan.name !== name) navigate(`/l/${enc(lib)}/plans/${enc(r.plan.name)}`, { replace: true });
    },
    onError: failed,
  });
  const saveEntries = useMutation({
    mutationFn: ({ entries, reason }: { entries: PlanEntry[]; reason: string }) =>
      call(api.PATCH("/plans/{plan}", { params: { path: { plan: name } }, body: { entries, reason } })),
    onSuccess: () => {
      ok("Saved");
      setPending(null);
      setEditing(null);
      setQuery("");
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const p = detail.data?.plan;
  if (!p) return <Text c="dimmed">{detail.error?.message ?? "Loading"}</Text>;
  const rows = detail.data!.rows;
  const entries = p.entries;
  const describe = (i: number) => {
    const e = entries[i];
    if (e.query) return `Query ${e.query}`;
    return rows.find((r) => r.entry === i + 1)?.channel.name ?? e.channel_id ?? "";
  };
  const move = (i: number, by: number) => {
    const next = [...entries];
    const [e] = next.splice(i, 1);
    next.splice(i + by, 0, e);
    setPending({ title: `Move entry ${i + 1} ${by < 0 ? "up" : "down"}`, entries: next });
  };
  const blank: PlanEntry = { channel_id: null, query: "", sort: "", name: "", assignment: "", notes: "", section_start: null };
  return (
    <Stack>
      <Group justify="space-between">
        <Group>
          <Title order={2}>{p.name}</Title>
          <Badge color={statusColor[p.status]} variant="light">
            {p.status}
          </Badge>
          <Text c="dimmed">{planDates(p)}</Text>
        </Group>
        {maintainer && (
          <Button variant="light" onClick={() => setForm({ name: p.name, description: p.description, starts: p.starts ?? "", ends: p.ends ?? "", status: p.status })}>
            Edit
          </Button>
        )}
      </Group>
      {p.description && <Text size="sm">{p.description}</Text>}
      <Title order={4}>Channels (ICS 205)</Title>
      <Table striped>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Ch #</Table.Th>
            <Table.Th>Name</Table.Th>
            <Table.Th>Assignment</Table.Th>
            <Table.Th>RX MHz</Table.Th>
            <Table.Th>RX tone</Table.Th>
            <Table.Th>TX MHz</Table.Th>
            <Table.Th>TX tone</Table.Th>
            <Table.Th>Mode</Table.Th>
            <Table.Th>Remarks</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r) => (
            <Table.Tr key={r.channel.id}>
              <Table.Td>{r.number}</Table.Td>
              <Table.Td>{r.name}</Table.Td>
              <Table.Td>{r.assignment}</Table.Td>
              <Table.Td>{formatMHz(r.channel.rx_hz)}</Table.Td>
              <Table.Td>{r.channel.tone_squelch ? shortTone(r.channel.rx_tone) : ""}</Table.Td>
              <Table.Td>{r.channel.tx_hz === null ? "" : formatMHz(r.channel.tx_hz)}</Table.Td>
              <Table.Td>{shortTone(r.channel.tx_tone)}</Table.Td>
              <Table.Td>{r.channel.mode}</Table.Td>
              <Table.Td>{r.notes}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {rows.length === 0 && <Text size="sm" c="dimmed">No channels yet; add entries below.</Text>}
      <Title order={4}>Entries</Title>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>#</Table.Th>
            <Table.Th>Channel or query</Table.Th>
            <Table.Th>Name in the plan</Table.Th>
            <Table.Th>Assignment</Table.Th>
            <Table.Th>Starts at Ch #</Table.Th>
            <Table.Th />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {entries.map((e, i) => (
            <Table.Tr key={i}>
              <Table.Td>{i + 1}</Table.Td>
              <Table.Td ff={e.query ? "monospace" : undefined}>{describe(i)}</Table.Td>
              <Table.Td>{e.name}</Table.Td>
              <Table.Td>{e.assignment}</Table.Td>
              <Table.Td>{e.section_start ?? ""}</Table.Td>
              <Table.Td display={maintainer ? undefined : "none"}>
                <Group gap={4} wrap="nowrap">
                  <ActionIcon variant="subtle" aria-label={`Move entry ${i + 1} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                    <IconArrowUp size={14} />
                  </ActionIcon>
                  <ActionIcon variant="subtle" aria-label={`Move entry ${i + 1} down`} disabled={i === entries.length - 1} onClick={() => move(i, 1)}>
                    <IconArrowDown size={14} />
                  </ActionIcon>
                  <ActionIcon variant="subtle" aria-label={`Edit entry ${i + 1}`} onClick={() => setEditing({ title: `Entry ${i + 1}: ${describe(i)}`, at: i, entry: e, isNew: false })}>
                    <IconPencil size={14} />
                  </ActionIcon>
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    aria-label={`Remove entry ${i + 1}`}
                    onClick={() => setPending({ title: `Remove entry ${i + 1}, ${describe(i)}`, entries: entries.filter((_, j) => j !== i) })}
                  >
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {maintainer && (
        <SharedChannelPicker
          label="Add a shared channel"
          onPick={(c) => setEditing({ title: `Add ${c.name}`, at: entries.length, entry: { ...blank, channel_id: c.id }, isNew: true })}
        />
      )}
      <Group align="end" display={maintainer ? undefined : "none"}>
        <TextInput flex={1} label="Add a query section" placeholder='system:"SBARC Hub"' value={query} onChange={(e) => setQuery(e.currentTarget.value)} />
        <Button
          variant="light"
          disabled={!query.trim()}
          onClick={() => setEditing({ title: `Add the section ${query}`, at: entries.length, entry: { ...blank, query, sort: "name" }, isNew: true })}
        >
          Add section
        </Button>
      </Group>
      <Title order={4}>History</Title>
      <History revisions={history.data?.revisions ?? []} />
      <ReasonModal opened={form !== null} title={`Edit ${p.name}`} loading={save.isPending} onClose={() => setForm(null)} onConfirm={(r) => save.mutate(r)}>
        {form && <PlanFields form={form} onChange={setForm} />}
      </ReasonModal>
      <ReasonModal
        opened={pending !== null}
        title={pending?.title ?? ""}
        loading={saveEntries.isPending}
        onClose={() => setPending(null)}
        onConfirm={(reason) => saveEntries.mutate({ entries: pending!.entries, reason })}
      />
      <ReasonModal
        opened={editing !== null}
        title={editing?.title ?? ""}
        loading={saveEntries.isPending}
        onClose={() => setEditing(null)}
        onConfirm={(reason) => {
          const next = [...entries];
          if (editing!.isNew) next.splice(editing!.at, 0, editing!.entry);
          else next[editing!.at] = editing!.entry;
          saveEntries.mutate({ entries: next, reason });
        }}
      >
        {editing && <EntryFields form={editing} onChange={setEditing} />}
      </ReasonModal>
    </Stack>
  );
}

function EntryFields(props: { form: EntryForm; onChange: (f: EntryForm) => void }) {
  const e = props.form.entry;
  const set = (patch: Partial<PlanEntry>) => props.onChange({ ...props.form, entry: { ...e, ...patch } });
  return (
    <>
      {e.query ? (
        <>
          <TextInput label="Query" value={e.query} onChange={(ev) => set({ query: ev.currentTarget.value })} />
          <Select label="Sort" data={["name", "freq"]} value={e.sort || "name"} allowDeselect={false} onChange={(v) => v && set({ sort: v })} />
        </>
      ) : (
        <TextInput label="Name in the plan" description="Such as FAIR TAC1; empty keeps the channel's own" value={e.name ?? ""} onChange={(ev) => set({ name: ev.currentTarget.value })} />
      )}
      <TextInput label="Assignment" placeholder="Net control" value={e.assignment ?? ""} onChange={(ev) => set({ assignment: ev.currentTarget.value })} />
      <TextInput label="Remarks" value={e.notes ?? ""} onChange={(ev) => set({ notes: ev.currentTarget.value })} />
      <NumberInput
        label="Starts at Ch #"
        description="Empty continues from the entry before"
        min={1}
        value={e.section_start ?? ""}
        onChange={(v) => set({ section_start: v === "" ? null : Number(v) })}
      />
    </>
  );
}
