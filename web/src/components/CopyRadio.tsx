import { Button, Modal, Select, Stack, Text, TextInput } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, call, type Channel, type Model, type Radio } from "../api/client";
import { modelLabel } from "../lib/radio";
import { failed, ok, warn } from "../lib/notify";

/** CopyRadio makes a new radio from this one as it is now, for another
 * model, and opens it. */
export function CopyRadio(props: { lib: string; radio: string; onClose: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [model, setModel] = useState<string | null>(null);
  const models = useQuery({ queryKey: ["models"], queryFn: () => call<{ models: Model[] }>(api.GET("/models")) });
  const copy = useMutation({
    mutationFn: () =>
      call<{ radio: Radio; copied: number; skipped: Channel[]; layout_error: string | null }>(
        api.POST("/libraries/{lib}/radios/{radio}/copy", { params: { path: { lib: props.lib, radio: props.radio } }, body: { name: name.trim(), model: model! } }),
      ),
    onSuccess: (r) => {
      ok(`Copied ${props.radio} to ${r.radio.name}: ${r.copied} channels`);
      const notes = r.skipped.map((c) => `Left out ${c.name}: the new model can't use it`);
      if (r.layout_error) notes.push(`${r.radio.name} doesn't lay out yet: ${r.layout_error}`);
      if (notes.length) warn(notes);
      void client.invalidateQueries();
      props.onClose();
      navigate(`/l/${encodeURIComponent(props.lib)}/radios/${encodeURIComponent(r.radio.name)}`);
    },
    onError: failed,
  });
  return (
    <Modal opened onClose={props.onClose} title={`Copy ${props.radio}`}>
      <Stack>
        <Text size="sm" c="dimmed">
          A new radio with {props.radio}'s sections, sources, channels, and overrides as they are now. Channels the new model can't use are left
          out. From then on the two are independent; compare them on the Compare tab.
        </Text>
        <TextInput label="Name" required value={name} onChange={(e) => setName(e.currentTarget.value)} data-autofocus />
        <Select
          label="Model"
          required
          searchable
          data={(models.data?.models ?? []).map((m) => ({ value: m.name, label: modelLabel(m) }))}
          value={model}
          onChange={setModel}
        />
        <Button onClick={() => copy.mutate()} loading={copy.isPending} disabled={!name.trim() || !model}>
          Copy
        </Button>
      </Stack>
    </Modal>
  );
}
