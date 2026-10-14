import { Checkbox, Group, Input, SegmentedControl, Select } from "@mantine/core";
import { CTCSS, DCS, formatTone, parseTone, type Tone } from "../lib/radio";

/** ToneInput picks none, a standard CTCSS tone, or a DCS code, and edits
 * rflib's tone syntax. */
export function ToneInput(props: { label: string; value: string; onChange: (v: string) => void; disabled?: boolean }) {
  const tone = parseTone(props.value);
  const set = (t: Tone) => props.onChange(formatTone(t));
  return (
    <Input.Wrapper label={props.label}>
      <Group gap="xs" align="end" wrap="nowrap">
        <SegmentedControl
          aria-label={`${props.label} kind`}
          disabled={props.disabled}
          value={tone.kind}
          data={[
            { value: "none", label: "None" },
            { value: "ctcss", label: "CTCSS" },
            { value: "dcs", label: "DCS" },
          ]}
          onChange={(kind) =>
            set(kind === "ctcss" ? { kind, hz: "100.0" } : kind === "dcs" ? { kind, code: "023", inverted: false } : { kind: "none" })
          }
        />
        {tone.kind === "ctcss" && (
          <Select
            aria-label={`${props.label} CTCSS tone`}
            disabled={props.disabled}
            w={110}
            searchable
            allowDeselect={false}
            data={CTCSS.includes(Number(tone.hz).toFixed(1)) ? CTCSS : [Number(tone.hz).toFixed(1), ...CTCSS]}
            value={Number(tone.hz).toFixed(1)}
            onChange={(hz) => hz && set({ kind: "ctcss", hz })}
          />
        )}
        {tone.kind === "dcs" && (
          <>
            <Select
              aria-label={`${props.label} DCS code`}
              disabled={props.disabled}
              w={100}
              searchable
              allowDeselect={false}
              data={DCS.includes(tone.code) ? DCS : [tone.code, ...DCS]}
              value={tone.code}
              onChange={(code) => code && set({ ...tone, code })}
            />
            <Checkbox
              label="Inverted"
              disabled={props.disabled}
              checked={tone.inverted}
              onChange={(e) => set({ ...tone, inverted: e.currentTarget.checked })}
            />
          </>
        )}
      </Group>
    </Input.Wrapper>
  );
}
