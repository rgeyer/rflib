import { Alert, Badge, Button, Checkbox, Group, Select, Stack, Table, Text } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, call, type Channel, type Radio } from "../api/client";
import { failed, ok, warn } from "../lib/notify";
import { formatMHz } from "../lib/radio";

type Comparison = {
  other: string;
  same: number;
  missing: { channel: Channel; section: string; name: string; unusable: string | null }[];
  extra: Channel[];
};

/** CompareRadios compares this radio with another of the user's, by
 * channel, and adds what the other has to this one. */
export function CompareRadios(props: { lib: string; radio: string }) {
  const client = useQueryClient();
  const path = { lib: props.lib, radio: props.radio };
  const [other, setOther] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const radios = useQuery({
    queryKey: ["radios", props.lib],
    queryFn: () => call<{ radios: Radio[] }>(api.GET("/libraries/{lib}/radios", { params: { path: { lib: props.lib } } })),
  });
  const result = useQuery({
    queryKey: ["compare-radio", props.lib, props.radio, other],
    enabled: other !== null,
    queryFn: () => call<Comparison>(api.POST("/libraries/{lib}/radios/{radio}/compare-radio", { params: { path }, body: { radio: other! } })),
  });
  // Everything this radio can use starts picked.
  useEffect(() => {
    setPicked(new Set((result.data?.missing ?? []).filter((m) => !m.unusable).map((m) => m.channel.id)));
  }, [result.data]);
  const add = useMutation({
    mutationFn: () =>
      call<{ added: Channel[]; new_sections: string[]; unstarted: string[]; overflowed: string[] }>(
        api.POST("/libraries/{lib}/radios/{radio}/compare-radio/apply", { params: { path }, body: { radio: other!, add: [...picked] } }),
      ),
    onSuccess: (r) => {
      ok(`Added ${r.added.length} channels from ${other}${r.new_sections.length ? `, in new sections ${r.new_sections.join(", ")}` : ""}`);
      const notes = [];
      if (r.overflowed.length)
        notes.push(`New channels for ${r.overflowed.join(", ")} would have run into a later section's start, so they're in sections of their own at the end`);
      if (r.unstarted.length) notes.push(`Their starts didn't fit, so these follow the section before them: ${r.unstarted.join(", ")}`);
      if (notes.length) warn(notes);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  const c = result.data;
  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Compare this radio with another of yours, by channel, and add what the other has: in its order, each into this radio's section of the same
        name, with the other radio's name, power, and scan for it. Sections this radio lacks are made as the other has them, at the end, where you can
        drag them into place; nothing already here moves.
      </Text>
      <Select
        label="Other radio"
        maw={320}
        data={(radios.data?.radios ?? []).filter((r) => r.name !== props.radio).map((r) => r.name)}
        value={other}
        onChange={setOther}
      />
      {result.error && <Alert color="red">{result.error.message}</Alert>}
      {c && (
        <>
          <Group gap="xs">
            <Badge variant="light" color="gray">
              {c.same} on both
            </Badge>
            <Badge variant="light" color="blue">
              {c.missing.length} only on {c.other}
            </Badge>
            <Badge variant="light" color="grape">
              {c.extra.length} only here
            </Badge>
          </Group>
          {c.missing.length > 0 && (
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th w={36} />
                  <Table.Th>Channel</Table.Th>
                  <Table.Th>RX MHz</Table.Th>
                  <Table.Th>Section on {c.other}</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {c.missing.map((m) => (
                  <Table.Tr key={m.channel.id} opacity={m.unusable ? 0.55 : 1}>
                    <Table.Td>
                      <Checkbox
                        aria-label={`Add ${m.channel.name}`}
                        disabled={m.unusable !== null}
                        checked={picked.has(m.channel.id)}
                        onChange={(e) => {
                          const n = new Set(picked);
                          if (e.currentTarget.checked) n.add(m.channel.id);
                          else n.delete(m.channel.id);
                          setPicked(n);
                        }}
                      />
                    </Table.Td>
                    <Table.Td>
                      {m.channel.name}
                      {m.name && (
                        <Text span size="xs" c="dimmed" ml={6}>
                          as {m.name}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td ff="monospace">{formatMHz(m.channel.rx_hz)}</Table.Td>
                    <Table.Td>{m.section}</Table.Td>
                    <Table.Td>
                      {m.unusable && (
                        <Text size="xs" c="dimmed">
                          Can't use: {m.unusable}
                        </Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
          {c.extra.length > 0 && (
            <Text size="sm" c="dimmed">
              Only here: {c.extra.map((ch) => ch.name).join(", ")}
            </Text>
          )}
          <Group>
            <Button onClick={() => add.mutate()} loading={add.isPending} disabled={picked.size === 0}>
              Add {picked.size} channels
            </Button>
          </Group>
        </>
      )}
    </Stack>
  );
}
