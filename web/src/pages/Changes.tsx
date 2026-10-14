import { Alert, Badge, Button, Card, Group, List, Stack, Text, Title } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, call, type Change } from "../api/client";
import { useLib } from "../lib/library";
import { failed, ok } from "../lib/notify";

/** Changes lists links whose shared channel changed since acknowledged. */
export function Changes() {
  const lib = useLib();
  const client = useQueryClient();
  const changes = useQuery({
    queryKey: ["changes", lib],
    queryFn: () => call<{ changes: Change[] }>(api.GET("/libraries/{lib}/shared/changes", { params: { path: { lib } } })),
  });
  const done = (msg: string) => {
    ok(msg);
    void client.invalidateQueries();
  };
  const ack = useMutation({
    mutationFn: (ids: string[]) => call(api.POST("/libraries/{lib}/shared/ack", { params: { path: { lib } }, body: { select: { ids } } })),
    onSuccess: (r) => done(`Acknowledged ${r.channels.length}`),
    onError: failed,
  });
  const relink = useMutation({
    mutationFn: (ids: string[]) => call(api.POST("/libraries/{lib}/shared/relink", { params: { path: { lib } }, body: { select: { ids } } })),
    onSuccess: (r) => done(`Moved ${r.channels.length} to their replacements; program those radios again`),
    onError: failed,
  });
  const list = changes.data?.changes ?? [];
  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Shared changes</Title>
        {list.length > 0 && (
          <Button variant="light" onClick={() => ack.mutate(list.map((c) => c.channel.id))} loading={ack.isPending}>
            Acknowledge all
          </Button>
        )}
      </Group>
      {changes.error && <Alert color="red">{changes.error.message}</Alert>}
      {changes.data && list.length === 0 && <Text c="dimmed">Your linked channels are up to date.</Text>}
      {list.map((c) => (
        <Card key={c.channel.id} withBorder>
          <Stack gap="xs">
            <Group justify="space-between">
              <Group gap="xs">
                <Text fw={600}>{c.channel.name}</Text>
                {c.channel.status !== "active" && <Badge color={c.channel.status === "retired" ? "gray" : "yellow"}>{c.channel.status}</Badge>}
              </Group>
              <Group gap="xs">
                {c.replacement && (
                  <Button size="xs" onClick={() => relink.mutate([c.channel.id])} loading={relink.isPending}>
                    Move to {c.replacement.name}
                  </Button>
                )}
                <Button size="xs" variant="light" onClick={() => ack.mutate([c.channel.id])}>
                  Acknowledge
                </Button>
              </Group>
            </Group>
            {c.changes.length > 0 && (
              <List size="sm">
                {c.changes.map((x) => (
                  <List.Item key={x}>{x}</List.Item>
                ))}
              </List>
            )}
            {c.reasons.length > 0 && <Text size="sm">Why: {c.reasons.join("; ")}</Text>}
            {c.radios.length > 0 && (
              <Text size="sm" c="dimmed">
                On {c.radios.join(", ")}; program {c.radios.length === 1 ? "it" : "them"} again.
              </Text>
            )}
          </Stack>
        </Card>
      ))}
    </Stack>
  );
}
