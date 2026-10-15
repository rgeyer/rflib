import { Anchor, Badge, Button, Group, Select, Stack, Table, Text, Textarea, TextInput, Title } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, call, type Channel, type Organization, type RecordRevision, type System } from "../api/client";
import { History, ReasonModal } from "../components/Records";
import { enc, useLib } from "../lib/library";
import { failed, ok } from "../lib/notify";
import { ORG_KINDS, useOrgs } from "../lib/records";
import { formatMHz } from "../lib/radio";

type OrgForm = { name: string; kind: string; callsign: string; website: string; notes: string };

function OrgFields(props: { form: OrgForm; onChange: (f: OrgForm) => void }) {
  const set = (k: keyof OrgForm) => (v: string) => props.onChange({ ...props.form, [k]: v });
  return (
    <>
      <TextInput label="Name" required value={props.form.name} onChange={(e) => set("name")(e.currentTarget.value)} />
      <Group grow>
        <Select label="Kind" data={ORG_KINDS} value={props.form.kind} allowDeselect={false} onChange={(v) => v && set("kind")(v)} />
        <TextInput label="Callsign" value={props.form.callsign} onChange={(e) => set("callsign")(e.currentTarget.value)} />
      </Group>
      <TextInput label="Website" placeholder="https://" value={props.form.website} onChange={(e) => set("website")(e.currentTarget.value)} />
      <Textarea label="Notes" autosize minRows={2} value={props.form.notes} onChange={(e) => set("notes")(e.currentTarget.value)} />
    </>
  );
}

/** Orgs lists the organizations that run repeaters and systems. */
export function Orgs() {
  const lib = useLib();
  const navigate = useNavigate();
  const client = useQueryClient();
  const orgs = useOrgs();
  const [form, setForm] = useState<OrgForm | null>(null);
  const create = useMutation({
    mutationFn: (reason: string) => call(api.POST("/organizations", { body: { ...form!, reason } })),
    onSuccess: (o) => {
      ok(`Added ${o.name}`);
      setForm(null);
      void client.invalidateQueries();
      navigate(`/l/${enc(lib)}/orgs/${enc(o.name)}`);
    },
    onError: failed,
  });
  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Organizations</Title>
        <Button onClick={() => setForm({ name: "", kind: "club", callsign: "", website: "", notes: "" })}>New organization</Button>
      </Group>
      <Text size="sm" c="dimmed">
        Who runs or supports the shared list's repeaters and systems. Find their channels with org:NAME.
      </Text>
      <Table highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Kind</Table.Th>
            <Table.Th>Callsign</Table.Th>
            <Table.Th>Website</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(orgs.data?.organizations ?? []).map((o) => (
            <Table.Tr key={o.id}>
              <Table.Td>
                <Anchor component={Link} to={enc(o.name)}>
                  {o.name}
                </Anchor>
              </Table.Td>
              <Table.Td>{o.kind}</Table.Td>
              <Table.Td>{o.callsign}</Table.Td>
              <Table.Td>
                {o.website && (
                  <Anchor href={o.website} target="_blank" rel="noreferrer">
                    {o.website}
                  </Anchor>
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {orgs.data?.organizations.length === 0 && <Text c="dimmed">No organizations yet.</Text>}
      <ReasonModal opened={form !== null} title="New organization" loading={create.isPending} onClose={() => setForm(null)} onConfirm={(r) => create.mutate(r)}>
        {form && <OrgFields form={form} onChange={setForm} />}
      </ReasonModal>
    </Stack>
  );
}

/** OrgPage shows an organization and what it runs or supports. */
export function OrgPage() {
  const lib = useLib();
  const name = useParams().org ?? "";
  const client = useQueryClient();
  const navigate = useNavigate();
  const detail = useQuery({
    queryKey: ["organization", name],
    queryFn: () => call<{ organization: Organization; channels: Channel[]; systems: System[] }>(api.GET("/organizations/{org}", { params: { path: { org: name } } })),
  });
  const history = useQuery({
    queryKey: ["organization-history", name],
    queryFn: () => call<{ revisions: RecordRevision[] }>(api.GET("/organizations/{org}/history", { params: { path: { org: name } } })),
  });
  const [form, setForm] = useState<OrgForm | null>(null);
  const save = useMutation({
    mutationFn: (reason: string) => call(api.PATCH("/organizations/{org}", { params: { path: { org: name } }, body: { ...form!, reason } })),
    onSuccess: (o) => {
      ok("Saved");
      setForm(null);
      void client.invalidateQueries();
      if (o.name !== name) navigate(`/l/${enc(lib)}/orgs/${enc(o.name)}`, { replace: true });
    },
    onError: failed,
  });
  const o = detail.data?.organization;
  if (!o) return <Text c="dimmed">{detail.error?.message ?? "Loading"}</Text>;
  const role = (refs: { org_id: string; role: string }[]) =>
    refs
      .filter((r) => r.org_id === o.id)
      .map((r) => r.role)
      .join(", ");
  return (
    <Stack>
      <Group justify="space-between">
        <Group>
          <Title order={2}>{o.name}</Title>
          <Badge variant="light">{o.kind}</Badge>
          {o.callsign && <Text c="dimmed">{o.callsign}</Text>}
        </Group>
        <Button variant="light" onClick={() => setForm({ name: o.name, kind: o.kind, callsign: o.callsign, website: o.website, notes: o.notes })}>
          Edit
        </Button>
      </Group>
      {o.website && (
        <Anchor href={o.website} target="_blank" rel="noreferrer">
          {o.website}
        </Anchor>
      )}
      {o.notes && <Text size="sm">{o.notes}</Text>}
      <Title order={4}>Systems</Title>
      {detail.data!.systems.length === 0 && <Text size="sm" c="dimmed">None</Text>}
      {detail.data!.systems.map((x) => (
        <Text key={x.id} size="sm">
          <Anchor component={Link} to={`/l/${enc(lib)}/systems/${enc(x.name)}`}>
            {x.name}
          </Anchor>{" "}
          ({role(x.organizations)})
        </Text>
      ))}
      <Title order={4}>Shared channels</Title>
      <Table>
        <Table.Tbody>
          {detail.data!.channels.map((c) => (
            <Table.Tr key={c.id}>
              <Table.Td>{c.name}</Table.Td>
              <Table.Td>{formatMHz(c.rx_hz)}</Table.Td>
              <Table.Td>{role(c.organizations)}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {detail.data!.channels.length === 0 && <Text size="sm" c="dimmed">None</Text>}
      <Title order={4}>History</Title>
      <History revisions={history.data?.revisions ?? []} />
      <ReasonModal opened={form !== null} title={`Edit ${o.name}`} loading={save.isPending} onClose={() => setForm(null)} onConfirm={(r) => save.mutate(r)}>
        {form && <OrgFields form={form} onChange={setForm} />}
      </ReasonModal>
    </Stack>
  );
}
