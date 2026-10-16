import { ActionIcon, Badge, Button, Checkbox, Divider, Group, Modal, NumberInput, SegmentedControl, Select, Stack, Text, TextInput, Tooltip } from "@mantine/core";
import { modals } from "@mantine/modals";
import { IconGripVertical, IconPencil } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call, type Section, type Source } from "../api/client";
import { failed, ok, warn } from "../lib/notify";

/** useSections loads a radio's sections, as laid out. */
export function useSections(lib: string, radio: string) {
  return useQuery({
    queryKey: ["sections", lib, radio],
    queryFn: () => call<{ sections: Section[] }>(api.GET("/libraries/{lib}/radios/{radio}/sections", { params: { path: { lib, radio } } })),
    refetchOnWindowFocus: true,
  });
}

/** range describes the memories a section uses or reserves. */
export function range(s: Section): string {
  if (s.size != null) return `Memories ${s.first}–${s.first + s.size - 1}`;
  if (s.last < s.first) return `Empty; would start at ${s.first}`;
  return s.last === s.first ? `Memory ${s.first}` : `Memories ${s.first}–${s.last}`;
}

/** SectionHeader is a section's row at the top of its memories: what it
 * is, the memories it uses, and the sources that feed it. */
export function SectionHeader(props: {
  section: Section;
  sources: Source[];
  onEdit: () => void;
  /** Selects the section's memories shown, for the bulk actions. */
  onSelectAll?: () => void;
  dragHandle: React.HTMLAttributes<HTMLElement> & { draggable?: boolean };
}) {
  const s = props.section;
  return (
    <Group gap="xs" wrap="nowrap">
      <Tooltip label="Drag to move the section">
        <ActionIcon variant="subtle" size="sm" color="gray" aria-label={`Drag section ${s.name}`} style={{ cursor: "grab" }} {...props.dragHandle}>
          <IconGripVertical size={14} />
        </ActionIcon>
      </Tooltip>
      <Text fw={600} size="sm">
        {s.name}
      </Text>
      <Text size="xs" c="dimmed">
        {range(s)}
        {s.start != null && ` · starts at ${s.start}`}
        {s.size != null && ` · ${s.size} reserved`}
      </Text>
      {s.sort !== "manual" && (
        <Badge size="xs" variant="light">
          by {s.sort}
        </Badge>
      )}
      {s.default && (
        <Tooltip label="Channels added by hand go here">
          <Badge size="xs" variant="outline">
            Default
          </Badge>
        </Tooltip>
      )}
      {props.sources.map((src) => (
        <Badge key={src.id} size="xs" variant="dot" color="grape" tt="none">
          {src.plan ? `Plan: ${src.plan}` : src.query}
        </Badge>
      ))}
      <ActionIcon variant="subtle" size="sm" aria-label={`Edit section ${s.name}`} onClick={props.onEdit}>
        <IconPencil size={14} />
      </ActionIcon>
      {props.onSelectAll && (
        <Button size="compact-xs" variant="subtle" onClick={props.onSelectAll} aria-label={`Select all in ${s.name}`}>
          Select all
        </Button>
      )}
    </Group>
  );
}

const nullable = (v: number | string) => (v === "" ? null : Number(v));

/** SectionEditor adds a section, or changes or deletes one. */
export function SectionEditor(props: { lib: string; radio: string; section: Section | null; onClose: () => void }) {
  const client = useQueryClient();
  const s = props.section;
  const path = { lib: props.lib, radio: props.radio };
  const [name, setName] = useState(s?.name ?? "");
  const [start, setStart] = useState<number | string>(s?.start ?? "");
  const [size, setSize] = useState<number | string>(s?.size ?? "");
  const [sort, setSort] = useState<string>(s?.sort ?? "manual");
  const [isDefault, setDefault] = useState(s?.default ?? false);
  const done = (msg: string) => {
    ok(msg);
    void client.invalidateQueries();
    props.onClose();
  };
  const body = { name: name.trim(), start: nullable(start), size: nullable(size), sort: sort as Section["sort"], default: isDefault };
  const save = useMutation({
    mutationFn: () =>
      s
        ? call(api.PATCH("/libraries/{lib}/radios/{radio}/sections/{section}", { params: { path: { ...path, section: s.id } }, body }))
        : call(api.POST("/libraries/{lib}/radios/{radio}/sections", { params: { path }, body })),
    onSuccess: () => done(s ? "Section saved" : "Section added"),
    onError: failed,
  });
  const sections = useSections(props.lib, props.radio);
  const [into, setInto] = useState<string | null>(null);
  const merge = useMergeSection(props.lib, props.radio, props.onClose);
  const remove = useMutation({
    mutationFn: (takeOff: boolean) =>
      call(api.DELETE("/libraries/{lib}/radios/{radio}/sections/{section}", { params: { path: { ...path, section: s!.id }, query: { take_off: takeOff } } })),
    onSuccess: (r, takeOff) =>
      done(
        `Section removed${takeOff ? `, with its ${r.taken_off} channels` : "; its channels are at the end of the default section"}${
          r.sources_removed.length ? `, and ${r.sources_removed.length === 1 ? "the source" : `${r.sources_removed.length} sources`} that fed it` : ""
        }`,
      ),
    onError: failed,
  });
  const sources = useQuery({
    queryKey: ["sources", props.lib, props.radio],
    queryFn: () => call<{ sources: Source[] }>(api.GET("/libraries/{lib}/radios/{radio}/sources", { params: { path } })),
  });
  const feeding = (sources.data?.sources ?? []).filter((src) => s && src.section_id === s.id);
  const first = start === "" ? null : Number(start);
  return (
    <Modal opened onClose={props.onClose} title={s ? `Section ${s.name}` : "New section"}>
      <Stack>
        <TextInput label="Name" required value={name} onChange={(e) => setName(e.currentTarget.value)} data-autofocus />
        <Group grow align="flex-start">
          <NumberInput label="Start" description="Empty: right after the section before it" min={0} value={start} onChange={setStart} />
          <NumberInput label="Size" description="Memories it reserves" min={1} value={size} onChange={setSize} />
          <NumberInput
            label="End"
            description="Or set the end"
            disabled={first === null}
            min={first ?? 0}
            value={first !== null && size !== "" ? first + Number(size) - 1 : ""}
            onChange={(v) => first !== null && setSize(v === "" ? "" : Number(v) - first + 1)}
          />
        </Group>
        <div>
          <Text size="sm" fw={500} mb={4}>
            Order
          </Text>
          <SegmentedControl
            data={[
              { value: "manual", label: "As placed" },
              { value: "name", label: "By name" },
              { value: "freq", label: "By frequency" },
            ]}
            value={sort}
            onChange={setSort}
          />
        </div>
        <Checkbox
          label="Default section: channels added by hand go here"
          checked={isDefault}
          disabled={s?.default}
          onChange={(e) => setDefault(e.currentTarget.checked)}
        />
        {s && (
          <>
            <Divider label="Merge" labelPosition="left" />
            <Group align="end">
              <Select
                label="Merge into"
                description="Its channels go to the end of that section, and its sources feed it"
                flex={1}
                data={(sections.data?.sections ?? []).filter((x) => x.id !== s.id).map((x) => ({ value: x.id, label: x.name }))}
                value={into}
                onChange={setInto}
              />
              <Button
                variant="light"
                disabled={!into}
                loading={merge.isPending}
                onClick={() => {
                  const target = sections.data?.sections.find((x) => x.id === into);
                  if (target) confirmMerge(s, target, () => merge.mutate({ from: s.id, into: target.id }));
                }}
              >
                Merge
              </Button>
            </Group>
          </>
        )}
        <Group justify="space-between">
          {s && !s.default ? (
            <Button
              variant="subtle"
              color="red"
              onClick={() => {
                let takeOff = false;
                modals.openConfirmModal({
                  title: `Remove section ${s.name}?`,
                  children: (
                    <Stack gap="xs">
                      {feeding.length > 0 && (
                        <Text size="sm">
                          {feeding.length === 1 ? "The source" : `The ${feeding.length} sources`} that feed it go too (
                          {feeding.map((src) => (src.plan ? `plan ${src.plan}` : src.query)).join("; ")}), so nothing refills it elsewhere. To keep
                          {feeding.length === 1 ? " it" : " them"}, merge the section into another instead.
                        </Text>
                      )}
                      <Checkbox label="Also take its channels off the radio (otherwise they go to the default section)" onChange={(e) => (takeOff = e.currentTarget.checked)} />
                    </Stack>
                  ),
                  labels: { confirm: "Remove", cancel: "Cancel" },
                  confirmProps: { color: "red" },
                  onConfirm: () => remove.mutate(takeOff),
                });
              }}
            >
              Remove section
            </Button>
          ) : (
            <span />
          )}
          <Button onClick={() => save.mutate()} loading={save.isPending} disabled={name.trim() === ""}>
            {s ? "Save section" : "Add section"}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

/** useMergeSection merges one of a radio's sections into another. */
export function useMergeSection(lib: string, radio: string, onDone?: () => void) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ from, into }: { from: string; into: string }) =>
      call(api.POST("/libraries/{lib}/radios/{radio}/sections/{section}/merge", { params: { path: { lib, radio, section: from } }, body: { into } })),
    onSuccess: (r) => {
      ok(`Merged: ${r.moved} channels moved`);
      if (r.starts_cleared.length)
        warn([`Their starts no longer fit, so these now follow the section before them: ${r.starts_cleared.join(", ")}`]);
      void client.invalidateQueries();
      onDone?.();
    },
    onError: failed,
  });
}

/** confirmMerge asks before merging one section into another. */
export function confirmMerge(from: Section, into: Section, onConfirm: () => void) {
  const count = from.last >= from.first ? from.last - from.first + 1 : 0;
  modals.openConfirmModal({
    title: `Merge ${from.name} into ${into.name}?`,
    children: (
      <Text size="sm">
        {count > 0 ? `Its ${count} memories go to the end of ${into.name}` : `It holds no memories`}, its sources feed {into.name}, and {from.name}{" "}
        goes{from.default ? `; ${into.name} becomes the default section` : ""}.
      </Text>
    ),
    labels: { confirm: "Merge", cancel: "Cancel" },
    onConfirm,
  });
}

