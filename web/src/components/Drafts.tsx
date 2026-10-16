import { Alert, Button, Group, Modal, Stack, Text } from "@mantine/core";
import { modals } from "@mantine/modals";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, call, showDraft, type Draft } from "../api/client";
import { failed, ok } from "../lib/notify";

/** since is when a draft was started, in local time. */
function since(d: Draft): string {
  return new Date(d.started_at).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

/** useDraftActions saves or discards a radio's draft. */
function useDraftActions(lib: string, radio: string) {
  const client = useQueryClient();
  const path = { lib, radio };
  const done = (msg: string) => {
    showDraft(lib, radio, null);
    ok(msg);
    void client.invalidateQueries();
  };
  const save = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/radios/{radio}/draft/save", { params: { path } })),
    onSuccess: () => done(`Saved ${radio}'s draft; program the radio again`),
    onError: failed,
  });
  const discard = useMutation({
    mutationFn: () => call(api.POST("/libraries/{lib}/radios/{radio}/draft/discard", { params: { path } })),
    onSuccess: () => done(`Discarded ${radio}'s draft; it's as it was`),
    onError: failed,
  });
  return { save, discard };
}

/** DraftBar says a radio's list is a draft, what the draft changes, and
 * saves or discards it. */
export function DraftBar(props: { lib: string; radio: string; draft: Draft }) {
  const { save, discard } = useDraftActions(props.lib, props.radio);
  const changes = useQuery({
    queryKey: ["draft", props.lib, props.radio, props.draft.id],
    queryFn: () => call(api.GET("/libraries/{lib}/radios/{radio}/draft", { params: { path: { lib: props.lib, radio: props.radio } } })),
  });
  const c = changes.data?.changes;
  const parts = c
    ? [
        [c.added.length, "added"],
        [c.removed.length, "removed"],
        [c.moved.length, "moved to another section"],
        [c.renumbered.length, "renumbered"],
        [c.changed.length, "changed"],
        [c.sections_added.length + c.sections_removed.length, "section changes"],
        [c.sources_added.length + c.sources_removed.length, "source changes"],
      ]
        .filter(([n]) => (n as number) > 0)
        .map(([n, what]) => `${n} ${what}`)
    : [];
  return (
    <Alert color="yellow" title={`Draft since ${since(props.draft)}`}>
      <Stack gap="xs">
        <Text size="sm">
          Changes to this radio go to its draft until you save it; exports use the saved radio.{" "}
          {c && (parts.length ? `So far: ${parts.join(", ")}.` : "Nothing's changed yet.")}
        </Text>
        <Group gap="xs">
          <Button size="xs" onClick={() => save.mutate()} loading={save.isPending}>
            Save draft
          </Button>
          <Button
            size="xs"
            variant="default"
            onClick={() =>
              modals.openConfirmModal({
                title: `Discard ${props.radio}'s draft?`,
                children: <Text size="sm">Its changes go, and the radio stays as it was last saved.</Text>,
                labels: { confirm: "Discard draft", cancel: "Keep it" },
                confirmProps: { color: "red" },
                onConfirm: () => discard.mutate(),
              })
            }
            loading={discard.isPending}
          >
            Discard draft
          </Button>
        </Group>
      </Stack>
    </Alert>
  );
}

/** ExportChoice asks, when a radio has a draft, whether to save it before
 * exporting, since exports use the saved radio. */
export function ExportChoice(props: { lib: string; radio: string; draft: Draft; onExport: () => void; onClose: () => void }) {
  const { save } = useDraftActions(props.lib, props.radio);
  return (
    <Modal opened onClose={props.onClose} title={`${props.radio} has an unsaved draft`}>
      <Stack>
        <Text size="sm">Exports use the saved radio. Save the draft first to export its changes.</Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => (props.onClose(), props.onExport())}>
            Export saved
          </Button>
          <Button
            onClick={() =>
              save.mutate(undefined, {
                onSuccess: () => {
                  props.onClose();
                  props.onExport();
                },
              })
            }
            loading={save.isPending}
          >
            Save draft and export
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
