import "leaflet/dist/leaflet.css";
import { Alert, Button, Group, Stack, Text, Title } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Circle, MapContainer, TileLayer } from "react-leaflet";
import { api, call, type Channel, type Library } from "../api/client";
import { ChannelMarkers } from "../components/ChannelMarkers";
import { QueryBar } from "../components/QueryBar";
import { SystemLines } from "../components/SystemLines";
import { useLib } from "../lib/library";
import { sites } from "../lib/map";
import { setTerm } from "../lib/query";
import { useSystems } from "../lib/records";

/** MapPage shows channels with a location, how far each reaches, and the
 * systems that link them. */
export function MapPage() {
  const lib = useLib();
  const [text, setText] = useState("");
  const [q] = useDebouncedValue(`has:location ${text}`, 300);
  const library = useQuery({
    queryKey: ["library", lib],
    queryFn: () => call<Library>(api.GET("/libraries/{lib}", { params: { path: { lib } } })),
  });
  const channels = useQuery({
    queryKey: ["channels", lib, q, "map"],
    queryFn: () =>
      call<{ channels: Channel[]; total: number; more_in_shared: number }>(
        api.GET("/libraries/{lib}/channels", { params: { path: { lib }, query: { q, more_in_shared: true } } }),
      ),
  });
  const located = useMemo(() => channels.data?.channels ?? [], [channels.data]);
  const placed = useMemo(() => sites(located), [located]);
  // Searching what's in use, the shared list may have more with a location.
  const more = channels.data?.more_in_shared ?? 0;
  const systems = useSystems();
  // System members are shared channels; a link stands in for its own.
  const where = new Map(located.map((c) => [c.link?.shared_id ?? c.id, [c.location!.latitude, c.location!.longitude] as [number, number]]));
  const center: [number, number] = located.length
    ? [located.reduce((s, c) => s + c.location!.latitude, 0) / located.length, located.reduce((s, c) => s + c.location!.longitude, 0) / located.length]
    : [39.5, -98.35];
  return (
    <Stack h="calc(100vh - 100px)">
      <Group justify="space-between">
        <Title order={2}>Map</Title>
        <Group gap="xs">
          <Text size="sm" c="dimmed">
            {located.length} channel{located.length === 1 ? "" : "s"} with a location
            {placed.length < located.length && `, at ${placed.length} place${placed.length === 1 ? "" : "s"}`}
          </Text>
          {more > 0 && (
            <Button size="compact-xs" variant="subtle" onClick={() => setText(setTerm(text, "in", "all"))}>
              {more} more in the shared list: show them too
            </Button>
          )}
        </Group>
      </Group>
      <div>
        <QueryBar value={text} onChange={setText} placeholder="Narrow with a search, such as band:2m" source={library.data ? !library.data.shared : false} />
      </div>
      {channels.error && <Alert color="red">{channels.error.message}</Alert>}
      <MapContainer key={`${lib}-${located.length > 0}`} center={center} zoom={located.length ? 8 : 4} style={{ flex: 1, minHeight: 300 }}>
        <TileLayer attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors' url="https://tile.openstreetmap.org/{z}/{x}/{y}.png" />
        {/* Radii go first and take no clicks, so the markers above them stay clickable. */}
        {/* One circle per reach at each site, so a site's channels don't stack them. */}
        {placed.flatMap((site) =>
          [...new Set(site.channels.map((c) => c.location!.radius_km).filter((r) => r != null))].map((r) => (
            <Circle key={`r-${site.key}-${r}`} center={[site.lat, site.lon]} radius={r * 1000} interactive={false} pathOptions={{ weight: 1, fillOpacity: 0.05 }} />
          )),
        )}
        <ChannelMarkers sites={placed} />
        <SystemLines systems={systems.data?.systems ?? []} where={(id) => where.get(id) ?? null} />
      </MapContainer>
    </Stack>
  );
}
