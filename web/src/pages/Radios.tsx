import { Button, Group, Modal, Select, Stack, Table, Text, TextInput, Title } from "@mantine/core";
import { IconPlus } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, call, type Model, type Radio } from "../api/client";
import { enc, useLib } from "../lib/library";
import { failed } from "../lib/notify";

export function Radios() {
  const lib = useLib();
  const [adding, setAdding] = useState(false);
  const radios = useQuery({
    queryKey: ["radios", lib],
    queryFn: () => call<{ radios: Radio[] }>(api.GET("/libraries/{lib}/radios", { params: { path: { lib } } })),
  });
  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Radios</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setAdding(true)}>
          Add radio
        </Button>
      </Group>
      {radios.data?.radios.length === 0 && <Text c="dimmed">No radios yet. Add one to plan its memories and export its programming file.</Text>}
      <Table highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Model</Table.Th>
            <Table.Th>Channels</Table.Th>
            <Table.Th>Default power</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(radios.data?.radios ?? []).map((r) => (
            <Table.Tr key={r.id}>
              <Table.Td>
                <Link to={enc(r.name)}>{r.name}</Link>
              </Table.Td>
              <Table.Td>{r.model}</Table.Td>
              <Table.Td>{r.channels}</Table.Td>
              <Table.Td>{r.default_power || "unset"}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <AddRadio opened={adding} onClose={() => setAdding(false)} />
    </Stack>
  );
}

function AddRadio(props: { opened: boolean; onClose: () => void }) {
  const lib = useLib();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [model, setModel] = useState<string | null>(null);
  const models = useQuery({
    queryKey: ["models"],
    queryFn: () => call<{ models: Model[] }>(api.GET("/models")),
    enabled: props.opened,
  });
  const add = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/radios", { params: { path: { lib } }, body: { name, model: model! } })),
    onSuccess: (r) => {
      void client.invalidateQueries({ queryKey: ["radios", lib] });
      props.onClose();
      navigate(enc(r.name));
    },
    onError: failed,
  });
  return (
    <Modal opened={props.opened} onClose={props.onClose} title="Add a radio">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          add.mutate();
        }}
      >
        <Stack>
          <TextInput label="Name" placeholder="HT" required value={name} onChange={(e) => setName(e.currentTarget.value)} data-autofocus />
          <Select
            label="Model"
            required
            searchable
            data={(models.data?.models ?? []).map((m) => ({ value: m.name, label: `${m.description} (${m.name})` }))}
            value={model}
            onChange={setModel}
          />
          <Button type="submit" loading={add.isPending} disabled={!name || !model}>
            Add
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}
