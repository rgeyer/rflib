import { Badge, Button, Card, Group, List, Modal, ScrollArea, SimpleGrid, Stack, Text, Title } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call, type Seed } from "../api/client";
import { failed, ok } from "../lib/notify";

type Change = { kind: "add" | "update" | "drop"; name: string; fields: string[]; kept: string[] };
type Update = { installed: boolean; dry_run: boolean; changes: Change[] };

/** Lists shows the built-in channel lists that seed the shared list, and
 * whether each is up to date there, for the maintainer to install or update. */
export function Lists() {
  const client = useQueryClient();
  const [preview, setPreview] = useState<{ seed: string; result: Update } | null>(null);
  const seeds = useQuery({ queryKey: ["seeds"], queryFn: () => call<{ seeds: Seed[] }>(api.GET("/seeds")) });
  const update = useMutation({
    mutationFn: ({ seed, dryRun }: { seed: string; dryRun: boolean }) =>
      call<Update>(api.POST("/seeds/{seed}/update", { params: { path: { seed } }, body: { dry_run: dryRun } })),
    onSuccess: (r, v) => {
      if (v.dryRun) {
        setPreview({ seed: v.seed, result: r });
        return;
      }
      setPreview(null);
      ok(`${v.seed}: ${summary(r.changes)}`);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  return (
    <Stack>
      <Title order={2}>Built-in lists</Title>
      <Text size="sm" c="dimmed">
        Built-in lists fill the shared list. A new database starts with all of them; when a newer rflib changes a list, it shows here as an
        update to review. An update adds the list's new channels, applies what the list changed, and retires channels it dropped; a field
        you've corrected keeps your correction. People take the channels from the shared list into their own libraries.
      </Text>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
        {(seeds.data?.seeds ?? []).map((s) => (
          <Card key={s.id} withBorder>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text fw={600}>{s.id}</Text>
                <Status seed={s} />
              </Group>
              <Text size="sm">{s.description}</Text>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  {s.count} channels
                </Text>
                {(!s.installed || s.pending > 0) && (
                  <Button size="xs" onClick={() => update.mutate({ seed: s.id, dryRun: true })} loading={update.isPending && update.variables?.seed === s.id}>
                    {s.installed ? "Review update" : "Install"}
                  </Button>
                )}
              </Group>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <Modal opened={preview !== null} onClose={() => setPreview(null)} title={`${preview?.result.installed ? "Update" : "Install"} ${preview?.seed}`}>
        {preview && (
          <Stack>
            <Text size="sm">{summary(preview.result.changes)}</Text>
            <ScrollArea.Autosize mah={320}>
              <List size="sm" spacing={2}>
                {preview.result.changes.map((ch) => (
                  <List.Item key={`${ch.kind}-${ch.name}`}>{describe(ch)}</List.Item>
                ))}
              </List>
            </ScrollArea.Autosize>
            <Button onClick={() => update.mutate({ seed: preview.seed, dryRun: false })} loading={update.isPending}>
              {preview.result.installed ? "Apply update" : "Install"}
            </Button>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}

function Status(props: { seed: Seed }) {
  if (!props.seed.installed) return <Badge color="gray">Not installed</Badge>;
  if (props.seed.pending > 0) return <Badge color="orange">{props.seed.pending} changes</Badge>;
  return <Badge color="green">Up to date</Badge>;
}

/** summary counts an update's changes. */
function summary(changes: Change[]): string {
  if (changes.length === 0) return "Up to date";
  const n = (k: Change["kind"]) => changes.filter((c) => c.kind === k).length;
  const parts = [
    n("add") && `${n("add")} to add`,
    n("update") && `${n("update")} to update`,
    n("drop") && `${n("drop")} to retire (no longer in the list)`,
  ].filter(Boolean);
  return parts.join(", ");
}

function describe(ch: Change): string {
  if (ch.kind === "add") return `Add ${ch.name}`;
  if (ch.kind === "drop") return `Retire ${ch.name}`;
  let s = `Update ${ch.name}`;
  if (ch.fields.length) s += `: ${ch.fields.join(", ")}`;
  if (ch.kept.length) s += ` (keeps your correction to ${ch.kept.join(", ")})`;
  return s;
}
