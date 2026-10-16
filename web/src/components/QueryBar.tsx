import { Button, Group, Menu, TextInput } from "@mantine/core";
import { IconChevronDown, IconSearch, IconX } from "@tabler/icons-react";
import { getTerm, setTerm } from "../lib/query";

/** Option is a filter's value; short, when set, labels the button. */
export type Option = { value: string; label: string; short?: string };

/** FilterMenu is a button that picks one value for a filter, or Any. A
 * filter with a default (unset, the value no term means) shows that value
 * when no term is set, and offers it in place of Any. */
export function FilterMenu(props: { label: string; options: Option[]; value: string | null; onChange: (v: string | null) => void; unset?: string }) {
  const value = props.value ?? props.unset ?? null;
  const current = props.options.find((o) => o.value === value);
  const shown = current?.short ?? current?.label;
  return (
    <Menu position="bottom-start" withinPortal>
      <Menu.Target>
        <Button
          size="compact-sm"
          variant={props.value !== null && props.value !== props.unset ? "light" : "default"}
          rightSection={<IconChevronDown size={14} />}
          aria-label={`${props.label} filter`}
        >
          {current ? `${props.label}: ${shown}` : props.label}
        </Button>
      </Menu.Target>
      <Menu.Dropdown mah={320} style={{ overflowY: "auto" }}>
        {!props.unset && <Menu.Item onClick={() => props.onChange(null)}>Any</Menu.Item>}
        {props.options.map((o) => (
          <Menu.Item key={o.value} onClick={() => props.onChange(o.value === props.unset ? null : o.value)} fw={o.value === value ? 600 : undefined}>
            {o.label}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}

const opts = (values: string[]): Option[] => values.map((v) => ({ value: v, label: v }));

/** The guided filters: each sets one field's term in the query. */
const FILTERS: { field: string; label: string; options: Option[]; unset?: string }[] = [
  { field: "band", label: "Band", options: opts(["2m", "1.25m", "70cm", "6m", "10m", "gmrs", "frs", "murs", "cb", "noaa", "air", "marine"]) },
  {
    field: "service",
    label: "Service",
    options: [...opts(["amateur", "gmrs", "frs", "murs", "cb", "noaa", "marine", "public_safety", "other"]), { value: "none", label: "none set" }],
  },
  {
    field: "is",
    label: "Type",
    options: [
      { value: "repeater", label: "repeater" },
      { value: "simplex", label: "simplex" },
      { value: "rxonly", label: "receive only" },
      { value: "transmit", label: "can transmit" },
      { value: "skip", label: "skipped in scan" },
    ],
  },
  {
    field: "tone",
    label: "Tone",
    options: [
      { value: "none", label: "no tone" },
      { value: "ctcss", label: "CTCSS" },
      { value: "dcs", label: "DCS" },
    ],
  },
  { field: "status", label: "Status", options: opts(["active", "off-air", "retired"]) },
];

/** SOURCE chooses which channels a library's search covers (the in: term). */
const SOURCE = {
  field: "in",
  label: "Source",
  unset: "use",
  options: [
    { value: "use", label: "in use (default)", short: "in use" },
    { value: "mine", label: "mine only" },
    { value: "shared", label: "the shared list" },
    { value: "all", label: "mine and the shared list" },
  ],
};

/**
 * QueryBar is the search box with guided filters. The text is always the
 * search: each filter adds, changes, or removes its own term in it, and
 * anything typed by hand is kept. Clear starts over: the search, and any
 * extra filters the page has.
 */
export function QueryBar(props: {
  value: string;
  onChange: (v: string) => void;
  tags?: string[];
  placeholder?: string;
  source?: boolean;
  /** Filters outside the search text that Clear also resets. */
  extra?: { active: boolean; clear: () => void };
}) {
  let filters = props.source ? [SOURCE, ...FILTERS] : FILTERS;
  if (props.tags?.length) filters = [...filters, { field: "tag", label: "Tag", options: opts(props.tags) }];
  return (
    <div style={{ flex: 1 }}>
      <TextInput
        leftSection={<IconSearch size={16} />}
        placeholder={props.placeholder ?? "Search: band:2m is:repeater, tag:ham, near:34.6,-120.4,40, or words"}
        aria-label="Search"
        value={props.value}
        onChange={(e) => props.onChange(e.currentTarget.value)}
      />
      <Group gap={6} mt={6}>
        {filters.map((f) => (
          <FilterMenu
            key={f.field}
            label={f.label}
            options={f.options}
            value={getTerm(props.value, f.field)}
            unset={"unset" in f ? f.unset : undefined}
            onChange={(v) => props.onChange(setTerm(props.value, f.field, v))}
          />
        ))}
        {props.extra?.active || props.value.trim() !== "" ? (
          <Button
            size="compact-sm"
            variant="subtle"
            leftSection={<IconX size={14} />}
            onClick={() => {
              props.onChange("");
              props.extra?.clear();
            }}
          >
            Clear
          </Button>
        ) : null}
      </Group>
    </div>
  );
}
