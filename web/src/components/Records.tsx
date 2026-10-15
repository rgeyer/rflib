import { Anchor, Button, Combobox, Group, Modal, Stack, Text, TextInput, Timeline, useCombobox } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { api, call, type Channel, type RecordRevision } from "../api/client";
import { networkLabel, networkURL } from "../lib/records";
import { formatMHz, toneSummary } from "../lib/radio";

/** NetworkLinks shows network nodes, each linking to its network's page. */
export function NetworkLinks(props: { networks: string[] }) {
  if (props.networks.length === 0) return null;
  return (
    <Group gap="xs">
      {props.networks.map((n) => {
        const url = networkURL(n);
        return url ? (
          <Anchor key={n} href={url} target="_blank" rel="noreferrer" size="sm">
            {networkLabel(n)}
          </Anchor>
        ) : (
          <Text key={n} size="sm">
            {networkLabel(n)}
          </Text>
        );
      })}
    </Group>
  );
}

/** History shows a record's revisions, newest first, with what changed. */
export function History(props: { revisions: RecordRevision[] }) {
  return (
    <Timeline active={props.revisions.length} bulletSize={14} lineWidth={2}>
      {[...props.revisions].reverse().map((r) => (
        <Timeline.Item key={r.number} title={`Revision ${r.number}: ${r.reason || "no reason given"}`}>
          <Text size="xs" c="dimmed">
            {new Date(r.at).toLocaleString()}
          </Text>
          {r.changes.map((c) => (
            <Text key={c} size="sm" style={{ wordBreak: "break-word" }}>
              {c}
            </Text>
          ))}
        </Timeline.Item>
      ))}
    </Timeline>
  );
}

/** ReasonModal asks why before a maintainer change, which the history
 * records. */
export function ReasonModal(props: { opened: boolean; title: string; children?: ReactNode; loading: boolean; onClose: () => void; onConfirm: (reason: string) => void }) {
  const [reason, setReason] = useState("");
  return (
    <Modal opened={props.opened} onClose={props.onClose} title={props.title}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          // React bubbles events through portals, and the modal may sit
          // inside another form.
          e.stopPropagation();
          props.onConfirm(reason);
        }}
      >
        <Stack>
          {props.children}
          <TextInput label="Reason" description="Recorded in the history" required value={reason} onChange={(e) => setReason(e.currentTarget.value)} data-autofocus />
          <Button type="submit" loading={props.loading} disabled={!reason.trim()}>
            Save
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}

/** SharedChannelPicker searches the shared list and picks a channel. */
export function SharedChannelPicker(props: { label: string; onPick: (c: Channel) => void }) {
  const [text, setText] = useState("");
  const [q] = useDebouncedValue(text, 300);
  const combobox = useCombobox();
  const found = useQuery({
    queryKey: ["channels", "shared", q],
    enabled: q.trim() !== "",
    queryFn: () => call<{ channels: Channel[]; total: number }>(api.GET("/libraries/{lib}/channels", { params: { path: { lib: "shared" }, query: { q, limit: 20 } } })),
  });
  return (
    <Combobox
      store={combobox}
      onOptionSubmit={(id) => {
        const c = found.data?.channels.find((x) => x.id === id);
        if (c) props.onPick(c);
        setText("");
        combobox.closeDropdown();
      }}
    >
      <Combobox.Target>
        <TextInput
          label={props.label}
          placeholder="Search the shared list, such as name:cumbre or band:2m"
          value={text}
          onChange={(e) => {
            setText(e.currentTarget.value);
            combobox.openDropdown();
          }}
          onFocus={() => combobox.openDropdown()}
          onBlur={() => combobox.closeDropdown()}
        />
      </Combobox.Target>
      <Combobox.Dropdown hidden={!q.trim()}>
        <Combobox.Options>
          {(found.data?.channels ?? []).map((c) => (
            <Combobox.Option key={c.id} value={c.id}>
              {c.name} · {formatMHz(c.rx_hz)} {toneSummary(c)}
            </Combobox.Option>
          ))}
          {found.data?.channels.length === 0 && <Combobox.Empty>No shared channels match</Combobox.Empty>}
          {found.error && <Combobox.Empty>{found.error.message}</Combobox.Empty>}
        </Combobox.Options>
      </Combobox.Dropdown>
    </Combobox>
  );
}
