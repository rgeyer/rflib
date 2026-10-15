import { Button, Card, Group, List, Modal, ScrollArea, SimpleGrid, Stack, Text, Title } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call, type Channel, type Conversion, type Seed } from "../api/client";
import { useLib, usePrivateLibraries } from "../lib/library";
import { failed, ok } from "../lib/notify";

type Install = { linked: Channel[]; converted: Conversion[]; already: Channel[]; shared_added: number; shared_updated: number };

/** Lists installs the built-in channel lists: they go in the shared library,
 * and the library links to them (the default library, from the shared
 * list). */
export function Lists() {
  const open = useLib();
  const libraries = usePrivateLibraries();
  const lib = libraries.some((l) => l.name === open) ? open : (libraries[0]?.name ?? open);
  const label = libraries.length === 1 ? "My library" : lib;
  const client = useQueryClient();
  const [preview, setPreview] = useState<{ seed: string; result: Install } | null>(null);
  const seeds = useQuery({ queryKey: ["seeds"], queryFn: () => call<{ seeds: Seed[] }>(api.GET("/seeds")) });
  const install = useMutation({
    mutationFn: ({ seed, dryRun }: { seed: string; dryRun: boolean }) =>
      call<Install>(api.POST("/libraries/{lib}/seeds/{seed}/install", { params: { path: { lib, seed } }, body: { dry_run: dryRun } })),
    onSuccess: (r, v) => {
      if (v.dryRun) {
        setPreview({ seed: v.seed, result: r });
        return;
      }
      setPreview(null);
      ok(`${v.seed}: linked ${r.linked.length}, converted ${r.converted.length} of your channels`);
      void client.invalidateQueries();
    },
    onError: failed,
  });
  return (
    <Stack>
      <Title order={2}>Built-in lists</Title>
      <Text size="sm" c="dimmed">
        Installing a list puts it in the shared library and links its channels into {lib}. Your matching channels become links, keeping your tags
        and notes.
      </Text>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
        {(seeds.data?.seeds ?? []).map((s) => (
          <Card key={s.id} withBorder>
            <Stack gap="xs">
              <Text fw={600}>{s.id}</Text>
              <Text size="sm">{s.description}</Text>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  {s.count} channels
                </Text>
                <Button size="xs" onClick={() => install.mutate({ seed: s.id, dryRun: true })} loading={install.isPending && install.variables?.seed === s.id}>
                  Install
                </Button>
              </Group>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <Modal opened={preview !== null} onClose={() => setPreview(null)} title={`Install ${preview?.seed}`}>
        {preview && (
          <Stack>
            <List size="sm">
              <List.Item>
                The shared list gains {preview.result.shared_added} channels
                {preview.result.shared_updated > 0 && ` and ${preview.result.shared_updated} there are updated`}.
              </List.Item>
              <List.Item>
                {label} gains {preview.result.linked.length} new links{preview.result.already.length > 0 && `; ${preview.result.already.length} are linked already`}.
              </List.Item>
              <List.Item>
                {preview.result.converted.length === 0
                  ? "None of your channels change."
                  : `${preview.result.converted.length} of your channels become links, keeping their radios, tags, and notes${
                      preview.result.converted.some((cv) => cv.channel.name !== cv.shared.name) ? "; a different name of yours stays as a short name" : ""
                    }:`}
              </List.Item>
            </List>
            {preview.result.converted.length > 0 && (
              <ScrollArea.Autosize mah={240}>
                <List size="sm" spacing={2}>
                  {preview.result.converted.map((cv) => (
                    <List.Item key={cv.channel.id}>
                      {cv.channel.name === cv.shared.name ? cv.channel.name : `${cv.channel.name} → ${cv.shared.name}`}
                    </List.Item>
                  ))}
                </List>
              </ScrollArea.Autosize>
            )}
            <Button onClick={() => install.mutate({ seed: preview.seed, dryRun: false })} loading={install.isPending}>
              Install
            </Button>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}
