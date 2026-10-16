import { Button, Group, Modal, NumberInput, Select, Stack, Text, Textarea, TextInput } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, call, type Model, type Radio } from "../api/client";
import { failed, ok } from "../lib/notify";

const powers = [
  { value: "", label: "Unset" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
  { value: "max", label: "Max" },
];

const dedupes = [
  { value: "none", label: "Leave them as they are" },
  { value: "index", label: "Number them" },
  { value: "freq", label: "Add the frequency" },
];

/** EditRadio changes a radio's settings, saved at once. A new model is a
 * copy instead, since it can leave channels out. */
export function EditRadio(props: { lib: string; radio: Radio; onClose: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const r = props.radio;
  const [name, setName] = useState(r.name);
  const [power, setPower] = useState<string>(r.default_power);
  const [dedupe, setDedupe] = useState<string>(r.dedupe || "none");
  const [nameLength, setNameLength] = useState<number | "">(r.name_length || "");
  const [notes, setNotes] = useState(r.notes);
  const models = useQuery({ queryKey: ["models"], queryFn: () => call<{ models: Model[] }>(api.GET("/models")) });
  const modelLength = models.data?.models.find((m) => m.name === r.model)?.name_length;
  const save = useMutation({
    mutationFn: () =>
      call<Radio>(
        api.PATCH("/libraries/{lib}/radios/{radio}", {
          params: { path: { lib: props.lib, radio: r.name } },
          body: { name: name.trim(), default_power: power, dedupe, name_length: nameLength || 0, notes },
        }),
      ),
    onSuccess: (saved) => {
      ok(`Saved ${saved.name}`);
      void client.invalidateQueries();
      props.onClose();
      if (saved.name !== r.name) navigate(`/l/${encodeURIComponent(props.lib)}/radios/${encodeURIComponent(saved.name)}`, { replace: true });
    },
    onError: failed,
  });
  return (
    <Modal opened onClose={props.onClose} title={`Edit ${r.name}`}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Stack>
          <TextInput label="Name" required value={name} onChange={(e) => setName(e.currentTarget.value)} data-autofocus />
          <Select label="Default power" description="For channels without a power of their own" data={powers} value={power} onChange={(v) => setPower(v ?? "")} allowDeselect={false} />
          <Select label="Duplicate names" description="When two memories end up with the same name" data={dedupes} value={dedupe} onChange={(v) => setDedupe(v ?? "none")} allowDeselect={false} />
          <NumberInput
            label="Name length"
            description={modelLength ? `Leave blank to use the model's (${modelLength})` : "The longest name the radio shows; leave blank to use the model's"}
            placeholder="Model's"
            min={1}
            max={64}
            allowDecimal={false}
            value={nameLength}
            onChange={(v) => setNameLength(typeof v === "number" ? v : "")}
          />
          <Textarea label="Notes" autosize minRows={2} value={notes} onChange={(e) => setNotes(e.currentTarget.value)} />
          <Text size="xs" c="dimmed">
            The model ({r.model}) can't change here: use Copy to make this radio as another model, since a new model can leave channels out.
          </Text>
          <Button type="submit" loading={save.isPending} disabled={!name.trim()}>
            Save
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}

/** DeleteRadio deletes a radio once its name is typed; its channels stay
 * in the library. */
export function DeleteRadio(props: { lib: string; radio: Radio; onClose: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const r = props.radio;
  const [typed, setTyped] = useState("");
  const remove = useMutation({
    mutationFn: () => call(api.DELETE("/libraries/{lib}/radios/{radio}", { params: { path: { lib: props.lib, radio: r.name } } })),
    onSuccess: () => {
      ok(`Deleted ${r.name}; its channels are still in the library`);
      props.onClose();
      navigate(`/l/${encodeURIComponent(props.lib)}/radios`, { replace: true });
      void client.invalidateQueries();
    },
    onError: failed,
  });
  return (
    <Modal opened onClose={props.onClose} title={`Delete ${r.name}?`}>
      <Stack>
        <Text size="sm">
          This deletes the radio, with its {r.channels} memories, sections, and sources. It can't be undone. The channels themselves stay in the
          library.
        </Text>
        <TextInput label={`Type ${r.name} to confirm`} value={typed} onChange={(e) => setTyped(e.currentTarget.value)} data-autofocus />
        <Group justify="flex-end">
          <Button variant="default" onClick={props.onClose}>
            Cancel
          </Button>
          <Button color="red" onClick={() => remove.mutate()} loading={remove.isPending} disabled={typed !== r.name}>
            Delete radio
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
