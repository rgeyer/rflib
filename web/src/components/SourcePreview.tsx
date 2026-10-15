import { Alert, ScrollArea, Spoiler, Stack, Table, Text } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, call, type AddResult, type Channel } from "../api/client";
import { formatMHz, toneSummary } from "../lib/radio";

/** SourcePreview shows what a new source would add to a radio, after the
 * radio's sources claim their channels, before the source is added. With
 * maxHeight, as in a dialog, the list scrolls within that height; without,
 * it takes the room it needs. */
export function SourcePreview(props: {
  lib: string;
  radio: string | null;
  query: string;
  plan?: string | null;
  sort: string;
  maxHeight?: number;
  /** An existing source being edited, previewed in its place. */
  number?: number;
}) {
  const [key] = useDebouncedValue(JSON.stringify({ query: props.query.trim(), plan: props.plan ?? null, sort: props.sort }), 300);
  const input = JSON.parse(key) as { query: string; plan: string | null; sort: string };
  const number = props.number;
  const radio = props.radio;
  const preview = useQuery({
    queryKey: ["source-preview", props.lib, radio, number ?? 0, input],
    queryFn: () =>
      call<AddResult>(
        api.POST("/libraries/{lib}/radios/{radio}/sources/preview", {
          params: { path: { lib: props.lib, radio: radio! } },
          body: { ...(number ? { number } : {}), ...(input.plan ? { plan: input.plan } : { query: input.query, sort: input.sort }) },
        }),
      ),
    enabled: radio !== null && (input.query !== "" || input.plan !== null),
    placeholderData: keepPreviousData,
    retry: false,
  });
  if (radio === null || (input.query === "" && input.plan === null)) return null;
  if (preview.error) return <Alert color="red">{preview.error.message}</Alert>;
  const p = preview.data;
  if (!p) return <Text size="sm">Looking for matches…</Text>;
  return (
    <Stack gap="xs" aria-label="Source preview">
      <Text size="sm" fw={500}>
        {p.added === 0
          ? "Would add nothing new: its matches are already on the radio, or an earlier source claims them."
          : `Sync would add ${p.added} channels to ${radio}.`}
      </Text>
      {p.channels.length > 0 && <Channels channels={p.channels} maxHeight={props.maxHeight} />}
      {p.skipped.length > 0 && <Skipped label={`${p.skipped.length} outside ${radio}'s bands`} channels={p.skipped} />}
      {p.wrong_mode.length > 0 && <Skipped label={`${p.wrong_mode.length} in modes ${radio} doesn't have`} channels={p.wrong_mode} />}
    </Stack>
  );
}

function Channels(props: { channels: Channel[]; maxHeight?: number }) {
  const table = (
    <Table>
      <Table.Tbody>
        {props.channels.map((c) => (
          <Table.Tr key={c.id}>
            <Table.Td>{c.name}</Table.Td>
            <Table.Td>
              {formatMHz(c.rx_hz)} {toneSummary(c)}
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
  return props.maxHeight ? <ScrollArea.Autosize mah={props.maxHeight}>{table}</ScrollArea.Autosize> : table;
}

function Skipped(props: { label: string; channels: Channel[] }) {
  return (
    <Spoiler maxHeight={0} showLabel={`${props.label}: show`} hideLabel="Hide">
      <Text size="sm" c="dimmed">
        {props.channels.map((c) => c.name).join(", ")}
      </Text>
    </Spoiler>
  );
}
