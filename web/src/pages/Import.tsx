import { Alert, Button, FileInput, Group, List, Select, Stack, Stepper, Switch, TagsInput, Text, Title } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getToken, UNAUTHORIZED, ApiError, type ImportResult } from "../api/client";
import { api, call } from "../api/client";
import { useLib } from "../lib/library";
import { failed, ok } from "../lib/notify";

const FORMATS = [
  { value: "csv", label: "rflib CSV (export csv, or a spreadsheet)" },
  { value: "json", label: "rflib JSON backup" },
  { value: "chirp", label: "CHIRP CSV (File > Export)" },
  { value: "profile", label: "A programming tool's CSV (export profile)" },
];

type Options = { format: string; profile: string | null; onDuplicate: string; match: string; tags: string[]; skipInvalid: boolean };

/** post sends the file to the import endpoint; the generated client doesn't
 * cover raw file bodies. */
async function post(lib: string, file: File, o: Options, dryRun: boolean): Promise<ImportResult> {
  const q = new URLSearchParams({ on_duplicate: o.onDuplicate, match: o.match, dry_run: String(dryRun), skip_invalid: String(o.skipInvalid) });
  if (o.tags.length) q.set("tags", o.tags.join(","));
  if (o.format === "profile" && o.profile) q.set("profile", o.profile);
  const headers: Record<string, string> = { "Content-Type": o.format === "json" ? "application/json" : "text/csv" };
  if (getToken()) headers.Authorization = `Bearer ${getToken()}`;
  const res = await fetch(`/api/v1/libraries/${encodeURIComponent(lib)}/imports/${o.format}?${q}`, { method: "POST", headers, body: file });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED));
    throw new ApiError(res.status, body.error?.code ?? "internal", body.error?.message ?? res.statusText);
  }
  return body as ImportResult;
}

export function ImportPage() {
  const lib = useLib();
  const client = useQueryClient();
  const [step, setStep] = useState(0);
  const [file, setFile] = useState<File | null>(null);
  const [o, setO] = useState<Options>({ format: "csv", profile: null, onDuplicate: "skip", match: "exact", tags: [], skipInvalid: false });
  const [result, setResult] = useState<ImportResult | null>(null);
  const profiles = useQuery({
    queryKey: ["profiles"],
    queryFn: () => call<{ profiles: { name: string; description: string }[] }>(api.GET("/profiles")),
  });
  const run = useMutation({
    mutationFn: (dryRun: boolean) => post(lib, file!, o, dryRun),
    onSuccess: (r, dryRun) => {
      setResult(r);
      if (!dryRun) {
        ok(`Imported: created ${r.created}, updated ${r.updated}`);
        void client.invalidateQueries();
      }
      setStep(dryRun ? 2 : 3);
    },
    onError: failed,
  });
  const set = <K extends keyof Options>(k: K, v: Options[K]) => setO((x) => ({ ...x, [k]: v }));
  const summary = (r: ImportResult) => (
    <Stack gap="xs">
      <Text>
        Creates {r.created}, updates {r.updated}, skips {r.skipped} duplicates{r.invalid.length ? `, and ${r.invalid.length} invalid rows` : ""}.
      </Text>
      {r.invalid.length > 0 && (
        <Alert color="red" title="Invalid rows">
          <List size="sm">
            {r.invalid.map((e) => (
              <List.Item key={e.row}>
                Row {e.row}: {e.message}
              </List.Item>
            ))}
          </List>
        </Alert>
      )}
      {r.warnings.length > 0 && (
        <Alert color="yellow" title="Warnings">
          <List size="sm">
            {r.warnings.map((e, i) => (
              <List.Item key={i}>{e.message}</List.Item>
            ))}
          </List>
        </Alert>
      )}
    </Stack>
  );
  return (
    <Stack maw={720}>
      <Title order={2}>Import</Title>
      <Stepper active={step} onStepClick={(s) => s < step && setStep(s)}>
        <Stepper.Step label="File">
          <Stack mt="md">
            <Select label="Format" data={FORMATS} value={o.format} allowDeselect={false} onChange={(v) => v && set("format", v)} />
            {o.format === "profile" && (
              <Select label="Profile" data={(profiles.data?.profiles ?? []).map((p) => ({ value: p.name, label: p.description }))} value={o.profile} onChange={(v) => set("profile", v)} />
            )}
            <FileInput label="File" placeholder="Choose a file" value={file} onChange={setFile} />
            <Group justify="end">
              <Button disabled={!file || (o.format === "profile" && !o.profile)} onClick={() => setStep(1)}>
                Next
              </Button>
            </Group>
          </Stack>
        </Stepper.Step>
        <Stepper.Step label="Options">
          <Stack mt="md">
            <Select
              label="Duplicates"
              description="A duplicate has the same ID, or the same name, frequencies, and tones"
              data={[
                { value: "skip", label: "Skip them" },
                { value: "update", label: "Update the existing channel" },
                { value: "create", label: "Create them anyway" },
              ]}
              value={o.onDuplicate}
              allowDeselect={false}
              onChange={(v) => v && set("onDuplicate", v)}
            />
            <Select
              label="Match duplicates on"
              data={[
                { value: "exact", label: "Name, frequencies, and tones" },
                { value: "programmed", label: "Frequencies and tones only (for files read from a radio)" },
              ]}
              value={o.match}
              allowDeselect={false}
              onChange={(v) => v && set("match", v)}
            />
            <TagsInput label="Tags for every imported channel" value={o.tags} onChange={(v) => set("tags", v)} />
            <Switch label="Import the valid rows even if some are invalid" checked={o.skipInvalid} onChange={(e) => set("skipInvalid", e.currentTarget.checked)} />
            <Group justify="end">
              <Button onClick={() => run.mutate(true)} loading={run.isPending}>
                Preview
              </Button>
            </Group>
          </Stack>
        </Stepper.Step>
        <Stepper.Step label="Preview">
          <Stack mt="md">
            {result && summary(result)}
            <Group justify="end">
              <Button onClick={() => run.mutate(false)} loading={run.isPending}>
                Import
              </Button>
            </Group>
          </Stack>
        </Stepper.Step>
        <Stepper.Completed>
          <Stack mt="md">
            {result && summary(result)}
            <Button
              variant="light"
              onClick={() => {
                setStep(0);
                setFile(null);
                setResult(null);
              }}
            >
              Import another file
            </Button>
          </Stack>
        </Stepper.Completed>
      </Stepper>
    </Stack>
  );
}
