import { ActionIcon, Anchor, Badge, Button, Group, Loader, MultiSelect, Select, Stack, Table, TagsInput, Text, Textarea, TextInput, Title } from "@mantine/core";
import { modals } from "@mantine/modals";
import { IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, call, type Channel, type OrgRef, type RecordRevision, type System, type SystemMember } from "../api/client";
import { useMaintainer } from "../lib/auth";
import { History, NetworkLinks, ReasonModal, SharedChannelPicker } from "../components/Records";
import { enc, useLib } from "../lib/library";
import { failed, ok } from "../lib/notify";
import { LINK_TYPES, useOrgs, useSystems } from "../lib/records";
import { formatMHz, toneSummary } from "../lib/radio";

const SystemMap = lazy(() => import("../components/SystemMap"));

type SystemForm = { name: string; link: string; networks: string[]; operators: string[]; partners: string[]; notes: string };

function toForm(x: System | null): SystemForm {
  const ids = (role: string) => (x?.organizations ?? []).filter((r) => r.role === role).map((r) => r.org_id);
  return { name: x?.name ?? "", link: x?.link ?? "", networks: x?.networks ?? [], operators: ids("operator"), partners: ids("partner"), notes: x?.notes ?? "" };
}

function toBody(f: SystemForm) {
  return {
    name: f.name,
    link: f.link,
    networks: f.networks,
    notes: f.notes,
    organizations: [...f.operators.map((org_id) => ({ org_id, role: "operator" as const })), ...f.partners.map((org_id) => ({ org_id, role: "partner" as const }))],
  };
}

function SystemFields(props: { form: SystemForm; onChange: (f: SystemForm) => void }) {
  const orgs = useOrgs();
  const data = (orgs.data?.organizations ?? []).map((o) => ({ value: o.id, label: o.name }));
  const set = <K extends keyof SystemForm>(k: K) => (v: SystemForm[K]) => props.onChange({ ...props.form, [k]: v });
  return (
    <>
      <TextInput label="Name" required value={props.form.name} onChange={(e) => set("name")(e.currentTarget.value)} />
      <Select label="Linked by" data={LINK_TYPES.map((t) => ({ value: t, label: t || "not given" }))} value={props.form.link} allowDeselect={false} onChange={(v) => v !== null && set("link")(v)} />
      <TagsInput label="The system's own nodes" description="Such as allstar:43763 for a hub" value={props.form.networks} onChange={set("networks")} />
      <MultiSelect label="Operators" data={data} searchable value={props.form.operators} onChange={set("operators")} />
      <MultiSelect label="Partners" data={data} searchable value={props.form.partners} onChange={set("partners")} />
      <Textarea label="Notes" autosize minRows={2} value={props.form.notes} onChange={(e) => set("notes")(e.currentTarget.value)} />
    </>
  );
}

function orgNames(refs: OrgRef[], role: string) {
  return refs
    .filter((r) => r.role === role)
    .map((r) => r.name)
    .join(", ");
}

/** Systems lists the shared list's systems of linked channels. */
export function Systems() {
  const maintainer = useMaintainer();
  const lib = useLib();
  const navigate = useNavigate();
  const client = useQueryClient();
  const systems = useSystems();
  const [form, setForm] = useState<SystemForm | null>(null);
  const create = useMutation({
    mutationFn: (reason: string) => call(api.POST("/systems", { body: { ...toBody(form!), reason } })),
    onSuccess: (r) => {
      ok(`Added ${r.system.name}`);
      setForm(null);
      void client.invalidateQueries();
      navigate(`/l/${enc(lib)}/systems/${enc(r.system.name)}`);
    },
    onError: failed,
  });
  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Systems</Title>
        {maintainer && <Button onClick={() => setForm(toForm(null))}>New system</Button>}
      </Group>
      <Text size="sm" c="dimmed">
        Linked repeaters as their operators intend them. Find a system's channels with system:NAME.
      </Text>
      <Table highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Linked by</Table.Th>
            <Table.Th>Nodes</Table.Th>
            <Table.Th>Members</Table.Th>
            <Table.Th>Operators</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(systems.data?.systems ?? []).map((x) => (
            <Table.Tr key={x.id}>
              <Table.Td>
                <Anchor component={Link} to={enc(x.name)}>
                  {x.name}
                </Anchor>
              </Table.Td>
              <Table.Td>{x.link}</Table.Td>
              <Table.Td>
                <NetworkLinks networks={x.networks} />
              </Table.Td>
              <Table.Td>{x.members.length}</Table.Td>
              <Table.Td>{orgNames(x.organizations, "operator")}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {systems.data?.systems.length === 0 && <Text c="dimmed">No systems yet.</Text>}
      <ReasonModal opened={form !== null} title="New system" loading={create.isPending} onClose={() => setForm(null)} onConfirm={(r) => create.mutate(r)}>
        {form && <SystemFields form={form} onChange={setForm} />}
      </ReasonModal>
    </Stack>
  );
}

/** SystemPage shows a system, its members, and a map of the links. */
export function SystemPage() {
  const maintainer = useMaintainer();
  const lib = useLib();
  const name = useParams().system ?? "";
  const client = useQueryClient();
  const navigate = useNavigate();
  const detail = useQuery({
    queryKey: ["system", name],
    queryFn: () => call<{ system: System; channels: Channel[] }>(api.GET("/systems/{system}", { params: { path: { system: name } } })),
  });
  const history = useQuery({
    queryKey: ["system-history", name],
    queryFn: () => call<{ revisions: RecordRevision[] }>(api.GET("/systems/{system}/history", { params: { path: { system: name } } })),
  });
  const [form, setForm] = useState<SystemForm | null>(null);
  // A pending change to the members, saved with a reason.
  const [members, setMembers] = useState<{ title: string; members: SystemMember[] } | null>(null);
  const [adding, setAdding] = useState<{ channel: Channel; linking: SystemMember["linking"]; notes: string } | null>(null);
  const done = (msg: string) => {
    ok(msg);
    void client.invalidateQueries();
  };
  const save = useMutation({
    mutationFn: (reason: string) => call(api.PATCH("/systems/{system}", { params: { path: { system: name } }, body: { ...toBody(form!), reason } })),
    onSuccess: (r) => {
      setForm(null);
      done("Saved");
      if (r.system.name !== name) navigate(`/l/${enc(lib)}/systems/${enc(r.system.name)}`, { replace: true });
    },
    onError: failed,
  });
  const saveMembers = useMutation({
    mutationFn: ({ list, reason }: { list: SystemMember[]; reason: string }) =>
      call(api.PATCH("/systems/{system}", { params: { path: { system: name } }, body: { members: list, reason } })),
    onSuccess: () => {
      setMembers(null);
      setAdding(null);
      done("Saved");
    },
    onError: failed,
  });
  const remove = useMutation({
    mutationFn: () => call(api.DELETE("/systems/{system}", { params: { path: { system: name } } })),
    onSuccess: () => {
      done("Deleted");
      navigate(`/l/${enc(lib)}/systems`);
    },
    onError: failed,
  });
  const x = detail.data?.system;
  if (!x) return <Text c="dimmed">{detail.error?.message ?? "Loading"}</Text>;
  const channels = detail.data!.channels;
  return (
    <Stack>
      <Group justify="space-between">
        <Group>
          <Title order={2}>{x.name}</Title>
          {x.link && <Badge variant="light">{x.link}</Badge>}
        </Group>
        <Group display={maintainer ? undefined : "none"}>
          <Button variant="light" onClick={() => setForm(toForm(x))}>
            Edit
          </Button>
          <Button
            variant="subtle"
            color="red"
            onClick={() =>
              modals.openConfirmModal({
                title: `Delete ${x.name}?`,
                children: <Text size="sm">Its channels stay in the shared list.</Text>,
                labels: { confirm: "Delete", cancel: "Cancel" },
                confirmProps: { color: "red" },
                onConfirm: () => remove.mutate(),
              })
            }
          >
            Delete
          </Button>
        </Group>
      </Group>
      <NetworkLinks networks={x.networks} />
      {x.organizations.length > 0 && (
        <Text size="sm">
          {[orgNames(x.organizations, "operator") && `Operated by ${orgNames(x.organizations, "operator")}`, orgNames(x.organizations, "partner") && `with ${orgNames(x.organizations, "partner")}`]
            .filter(Boolean)
            .join(", ")}
        </Text>
      )}
      {x.notes && <Text size="sm">{x.notes}</Text>}
      <Text size="sm">
        <Anchor component={Link} to={`/l/${enc(lib)}/channels?q=${encodeURIComponent(`system:"${x.name}"`)}`}>
          Find its channels in {lib}
        </Anchor>
      </Text>
      <Title order={4}>Members</Title>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Channel</Table.Th>
            <Table.Th>MHz</Table.Th>
            <Table.Th>Tone</Table.Th>
            <Table.Th>Linked</Table.Th>
            <Table.Th>Nodes</Table.Th>
            <Table.Th>Notes</Table.Th>
            <Table.Th />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {x.members.map((m, i) => (
            <Table.Tr key={m.channel_id}>
              <Table.Td>{channels[i].name}</Table.Td>
              <Table.Td>{formatMHz(channels[i].rx_hz)}</Table.Td>
              <Table.Td>{toneSummary(channels[i])}</Table.Td>
              <Table.Td>{m.linking}</Table.Td>
              <Table.Td>
                <NetworkLinks networks={channels[i].networks} />
              </Table.Td>
              <Table.Td>{m.notes}</Table.Td>
              <Table.Td display={maintainer ? undefined : "none"}>
                <ActionIcon
                  variant="subtle"
                  color="red"
                  aria-label={`Take ${channels[i].name} out`}
                  onClick={() => setMembers({ title: `Take ${channels[i].name} out of ${x.name}`, members: x.members.filter((o) => o.channel_id !== m.channel_id) })}
                >
                  <IconTrash size={14} />
                </ActionIcon>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      {maintainer && <SharedChannelPicker label="Add a member" onPick={(c) => setAdding({ channel: c, linking: "full-time", notes: "" })} />}
      <Suspense fallback={<Loader />}>
        <SystemMap system={x} channels={channels} />
      </Suspense>
      <Title order={4}>History</Title>
      <History revisions={history.data?.revisions ?? []} />
      <ReasonModal opened={form !== null} title={`Edit ${x.name}`} loading={save.isPending} onClose={() => setForm(null)} onConfirm={(r) => save.mutate(r)}>
        {form && <SystemFields form={form} onChange={setForm} />}
      </ReasonModal>
      <ReasonModal
        opened={members !== null}
        title={members?.title ?? ""}
        loading={saveMembers.isPending}
        onClose={() => setMembers(null)}
        onConfirm={(reason) => saveMembers.mutate({ list: members!.members, reason })}
      />
      <ReasonModal
        opened={adding !== null}
        title={`Add ${adding?.channel.name ?? ""} to ${x.name}`}
        loading={saveMembers.isPending}
        onClose={() => setAdding(null)}
        onConfirm={(reason) =>
          saveMembers.mutate({ list: [...x.members, { channel_id: adding!.channel.id, linking: adding!.linking, notes: adding!.notes }], reason })
        }
      >
        {adding && (
          <>
            <Select
              label="Linked"
              data={[
                { value: "full-time", label: "Full time: usually linked" },
                { value: "on-demand", label: "On demand: may be linked" },
              ]}
              value={adding.linking}
              allowDeselect={false}
              onChange={(v) => v && setAdding({ ...adding, linking: v as SystemMember["linking"] })}
            />
            <TextInput label="How it's linked" placeholder="microwave to the hub" value={adding.notes} onChange={(e) => setAdding({ ...adding, notes: e.currentTarget.value })} />
          </>
        )}
      </ReasonModal>
    </Stack>
  );
}
