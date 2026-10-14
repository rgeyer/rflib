import "leaflet/dist/leaflet.css";
import { Alert, Group, Stack, Text, TextInput, Title } from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Circle, CircleMarker, MapContainer, Popup, TileLayer } from "react-leaflet";
import { api, call, type Channel } from "../api/client";
import { useLib } from "../lib/library";
import { formatMHz, toneSummary } from "../lib/radio";

/** MapPage shows channels with a location, and how far each reaches. */
export function MapPage() {
  const lib = useLib();
  const [text, setText] = useState("");
  const [q] = useDebouncedValue(`has:location ${text}`, 300);
  const channels = useQuery({
    queryKey: ["channels", lib, q],
    queryFn: () => call<{ channels: Channel[]; total: number }>(api.GET("/libraries/{lib}/channels", { params: { path: { lib }, query: { q } } })),
  });
  const located = channels.data?.channels ?? [];
  const center: [number, number] = located.length
    ? [located.reduce((s, c) => s + c.location!.latitude, 0) / located.length, located.reduce((s, c) => s + c.location!.longitude, 0) / located.length]
    : [39.5, -98.35];
  return (
    <Stack h="calc(100vh - 100px)">
      <Group justify="space-between">
        <Title order={2}>Map</Title>
        <Text size="sm" c="dimmed">
          {located.length} channels with a location
        </Text>
      </Group>
      <TextInput placeholder="Narrow with a search, such as band:2m" aria-label="Search" value={text} onChange={(e) => setText(e.currentTarget.value)} />
      {channels.error && <Alert color="red">{channels.error.message}</Alert>}
      <MapContainer key={`${lib}-${located.length > 0}`} center={center} zoom={located.length ? 8 : 4} style={{ flex: 1, minHeight: 300 }}>
        <TileLayer attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors' url="https://tile.openstreetmap.org/{z}/{x}/{y}.png" />
        {located.map((c) => (
          <CircleMarker key={c.id} center={[c.location!.latitude, c.location!.longitude]} radius={6}>
            <Popup>
              <strong>{c.name}</strong>
              <br />
              {formatMHz(c.rx_hz)} MHz {toneSummary(c)}
              {c.location!.radius_km != null && (
                <>
                  <br />
                  reaches {c.location!.radius_km} km
                </>
              )}
            </Popup>
          </CircleMarker>
        ))}
        {located
          .filter((c) => c.location!.radius_km != null)
          .map((c) => (
            <Circle key={`r-${c.id}`} center={[c.location!.latitude, c.location!.longitude]} radius={c.location!.radius_km! * 1000} pathOptions={{ weight: 1, fillOpacity: 0.05 }} />
          ))}
      </MapContainer>
    </Stack>
  );
}
