import { Alert, Badge, Button, FileInput, Group, SegmentedControl, Select, Stack, Switch, Table, Text } from "@mantine/core";
import { modals } from "@mantine/modals";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call, type MemoryDiff, type MemoryValues } from "../api/client";
import { failed, ok } from "../lib/notify";
import { duplex, fieldValue, formatMHz, offsetMHz, toneSummary } from "../lib/radio";

type Comparison = { memories: MemoryDiff[]; errors: string[] };
type Applied = { used: number; kept: number; taken: number; at_end: string[]; copied: string[] };

/** What to do with a memory only on the radio: "keep" it as programmed,
 * "off" (leave it off), or a channel ID to use. */
type Choice = string;

const STATUS: Record<MemoryDiff["status"], { label: string; color: string }> = {
  same: { label: "Same", color: "gray" },
  changed: { label: "Changed", color: "orange" },
  radio: { label: "Only on the radio", color: "blue" },
  rflib: { label: "Only in rflib", color: "grape" },
};

/** CompareRadio compares a radio with a file of what it holds, and brings
 * rflib in line with the choices made per memory. */
export function CompareRadio(props: { lib: string; radio: string }) {
  const client = useQueryClient();
  const path = { lib: props.lib, radio: props.radio };
  const [file, setFile] = useState<File | null>(null);
  const [text, setText] = useState("");
  const [result, setResult] = useState<Comparison | null>(null);
  const [choices, setChoices] = useState<Record<number, Choice>>({});
  const [takes, setTakes] = useState<Record<number, boolean>>({});
  const [hideSame, setHideSame] = useState(true);
  const [ignoreNumbers, setIgnoreNumbers] = useState(false);

  const compare = useMutation({
    mutationFn: ({ file, ignore }: { file: string; ignore: boolean }) =>
      call<Comparison>(api.POST("/libraries/{lib}/radios/{radio}/compare", { params: { path }, body: { file, ignore_numbers: ignore } })),
    onSuccess: (r) => {
      setResult(r);
      setTakes({});
      // A memory with an exact match uses it; any other is kept as programmed.
      const next: Record<number, Choice> = {};
      for (const m of r.memories) {
        if (m.status !== "radio" || m.memory === null) continue;
        const exact = m.candidates.find((c) => c.match === "exact");
        next[m.memory] = exact ? exact.channel.id : "keep";
      }
      setChoices(next);
    },
    onError: failed,
  });
  const choose = async (f: File | null) => {
    setFile(f);
    setResult(null);
    if (!f) return;
    const t = await f.text();
    setText(t);
    compare.mutate({ file: t, ignore: ignoreNumbers });
  };
  const setIgnore = (ignore: boolean) => {
    setIgnoreNumbers(ignore);
    if (text) compare.mutate({ file: text, ignore });
  };

  const decisions = () => {
    const use = Object.entries(choices)
      .filter(([, c]) => c !== "keep" && c !== "off")
      .map(([m, id]) => ({ memory: Number(m), channel_id: id }));
    const keep = Object.entries(choices)
      .filter(([, c]) => c === "keep")
      .map(([m]) => Number(m));
    const take = Object.entries(takes)
      .filter(([, t]) => t)
      .map(([m]) => Number(m));
    return { use, keep, take };
  };
  const apply = useMutation({
    mutationFn: () => call<Applied>(api.POST("/libraries/{lib}/radios/{radio}/compare/apply", { params: { path }, body: { file: text, ignore_numbers: ignoreNumbers, ...decisions() } })),
    onSuccess: (r) => {
      let msg = `Used ${r.used}, kept ${r.kept}, and took ${r.taken} memories`;
      if (r.at_end.length) msg += `; at the end, as rflib uses their numbers: ${r.at_end.join(", ")}`;
      if (r.copied.length) msg += `; copied from the shared list: ${r.copied.join(", ")}`;
      ok(msg);
      void client.invalidateQueries();
      compare.mutate({ file: text, ignore: ignoreNumbers });
    },
    onError: failed,
  });
  const confirm = () => {
    const d = decisions();
    const off = Object.values(choices).filter((c) => c === "off").length;
    modals.openConfirmModal({
      title: "Bring rflib in line with the radio?",
      children: (
        <Text size="sm">
          Use {d.use.length} library channels, keep {d.keep.length} memories as programmed (as your own channels), and take the radio's values for{" "}
          {d.take.length} changed memories.
          {ignoreNumbers ? " What you use or keep goes at the end of the radio's list." : ""}
          {off > 0 && ` ${off} memories left off will be gone from the radio at its next programming.`}
        </Text>
      ),
      labels: { confirm: "Apply", cancel: "Cancel" },
      onConfirm: () => apply.mutate(),
    });
  };

  const shown = (result?.memories ?? []).filter((m) => !hideSame || m.status !== "same");
  const count = (s: MemoryDiff["status"]) => (result?.memories ?? []).filter((m) => m.status === s).length;
  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Compare this radio with a file of what it holds now, as its programming software or CHIRP saved it. For a radio that's new to rflib, every memory is
        only on the radio: use a library channel that matches, or keep it as programmed. Memories keep their numbers where rflib has nothing there.
      </Text>
      <FileInput label="The radio's file" placeholder="Choose a file" value={file} onChange={(f) => void choose(f)} clearable maw={420} />
      <Switch
        label="Ignore memory numbers"
        description="For a first import into a radio rflib already lays out its own way: only check that every channel on the radio is in rflib somewhere. What you use or keep goes at the end of rflib's list, and rflib's order wins from then on."
        checked={ignoreNumbers}
        onChange={(e) => setIgnore(e.currentTarget.checked)}
        maw={640}
      />
      {result && result.errors.length > 0 && (
        <Alert color="yellow" title="Some rows couldn't be read">
          {result.errors.join("; ")}
        </Alert>
      )}
      {result && (
        <>
          <Group gap="xs">
            {(["same", "changed", "radio", "rflib"] as const).map((s) => (
              <Badge key={s} color={STATUS[s].color} variant="light">
                {count(s)} {STATUS[s].label.toLowerCase()}
              </Badge>
            ))}
            <Switch ml="auto" label="Hide same" checked={hideSame} onChange={(e) => setHideSame(e.currentTarget.checked)} />
          </Group>
          <Table.ScrollContainer minWidth={760}>
            <Table striped>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>#</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>On the radio</Table.Th>
                  <Table.Th>In rflib</Table.Th>
                  <Table.Th>What to do</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {shown.map((m, i) => (
                  <Table.Tr key={`${m.status}-${m.memory}-${i}`}>
                    <Table.Td>{m.memory ?? "–"}</Table.Td>
                    <Table.Td>
                      <Badge color={STATUS[m.status].color} variant="light">
                        {STATUS[m.status].label}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Values v={m.radio} fields={m.fields} />
                    </Table.Td>
                    <Table.Td>
                      <Values v={m.rflib} fields={m.fields} />
                      {m.unexported && (
                        <Text size="xs" c="red">
                          rflib's export leaves it out: {m.unexported}
                        </Text>
                      )}
                      {m.rflib_memory != null && (
                        <Text size="xs" c="dimmed">
                          rflib has it at {m.rflib_memory}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      {/* Only the radio's memories, which always have numbers, take decisions. */}
                      <Decision
                        m={m}
                        choice={choices[m.memory ?? -1]}
                        take={takes[m.memory ?? -1] ?? false}
                        onChoice={(c) => setChoices({ ...choices, [m.memory ?? -1]: c })}
                        onTake={(t) => setTakes({ ...takes, [m.memory ?? -1]: t })}
                      />
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
          {shown.length === 0 && <Text size="sm">The radio holds what rflib would program.</Text>}
          <Group>
            <Button onClick={confirm} loading={apply.isPending} disabled={count("radio") + count("changed") === 0}>
              Apply
            </Button>
          </Group>
        </>
      )}
    </Stack>
  );
}

function Decision(props: { m: MemoryDiff; choice?: Choice; take: boolean; onChoice: (c: Choice) => void; onTake: (t: boolean) => void }) {
  const { m } = props;
  if (m.status === "radio") {
    const data = [
      ...m.candidates.map((c) => ({ value: c.channel.id, label: `Use ${c.channel.name} (${candidateMatch(c)})` })),
      { value: "keep", label: "Keep as programmed" },
      { value: "off", label: "Leave it off" },
    ];
    const chosen = m.candidates.find((c) => c.channel.id === props.choice);
    return (
      <Stack gap={4}>
        <Select aria-label={`Memory ${m.memory}`} data={data} value={props.choice ?? "keep"} allowDeselect={false} onChange={(v) => v && props.onChoice(v)} />
        {chosen && m.radio && chosen.differs.length > 0 && (
          <Stack gap={0} data-testid="candidate-differs">
            {chosen.differs.map((f) => (
              <Text key={f} size="xs">
                {f}: <Text span inherit c="orange" fw={600}>{fieldValue(m.radio!, f)}</Text> on the radio, {fieldValue(chosen.channel, f)} in rflib
              </Text>
            ))}
          </Stack>
        )}
      </Stack>
    );
  }
  if (m.status === "changed") {
    return (
      <Stack gap={4}>
        <Text size="xs">Differs in {m.fields.join(", ")}</Text>
        <SegmentedControl
          size="xs"
          aria-label={`Memory ${m.memory}`}
          data={[
            { value: "rflib", label: "Keep rflib's" },
            { value: "radio", label: "Take the radio's" },
          ]}
          value={props.take ? "radio" : "rflib"}
          onChange={(v) => props.onTake(v === "radio")}
        />
        {props.take && m.channel?.link && m.fields.some((f) => f !== "name" && f !== "skip") && (
          <Text size="xs" c="dimmed">
            A shared channel: you'll get your own copy with the radio's values.
          </Text>
        )}
      </Stack>
    );
  }
  if (m.status === "rflib")
    return (
      <Text size="xs" c="dimmed">
        The next export programs it
      </Text>
    );
  return null;
}

/** candidateMatch says how well a channel matches a memory only on the
 * radio, and what differs. */
function candidateMatch(c: MemoryDiff["candidates"][number]): string {
  const how = c.match === "exact" ? "exact match" : c.match === "frequencies" ? "same frequencies" : "same receive";
  return c.differs.length ? `${how}; differs in ${c.differs.join(", ")}` : how;
}

/** Values shows a memory's programming, marking the parts that differ
 * from the other side (fields, as the comparison names them). */
function Values(props: { v?: MemoryValues | null; fields?: string[] }) {
  const v = props.v;
  if (!v) return <Text c="dimmed">-</Text>;
  const differs = (...fs: string[]) => fs.some((f) => props.fields?.includes(f));
  const d = duplex(v.rx_hz, v.tx_hz ?? null);
  const shift = d === "+" || d === "-" ? `${d}${offsetMHz(v.rx_hz, v.tx_hz ?? null)}` : d === "split" ? `tx ${formatMHz(v.tx_hz!)}` : d === "off" ? "rx only" : "";
  const parts: [string, boolean][] = [
    [formatMHz(v.rx_hz), differs("rx")],
    [shift, differs("tx")],
    // "no tone" only where the other side has one.
    [toneSummary(v) || (differs("tx tone", "rx tone", "tone squelch") ? "no tone" : ""), differs("tx tone", "rx tone", "tone squelch")],
    [v.mode, differs("mode")],
    [v.skip ? "skipped" : "", differs("skip")],
  ];
  return (
    <Stack gap={0}>
      <Text size="sm" c={differs("name") ? "orange" : undefined} fw={differs("name") ? 600 : undefined}>
        {v.name}
      </Text>
      <Text size="xs" ff="monospace" c="dimmed">
        {parts
          .filter(([t]) => t)
          .map(([t, diff], i) => (
            <span key={i}>
              {i > 0 && " "}
              {diff ? (
                <Text span inherit c="orange" fw={700} data-differs>
                  {t}
                </Text>
              ) : (
                t
              )}
            </span>
          ))}
      </Text>
    </Stack>
  );
}
