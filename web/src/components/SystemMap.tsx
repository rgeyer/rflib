import "leaflet/dist/leaflet.css";
import { CircleMarker, MapContainer, Popup, TileLayer } from "react-leaflet";
import type { Channel, System } from "../api/client";
import { formatMHz, toneSummary } from "../lib/radio";
import { SystemLines } from "./SystemLines";

/** SystemMap shows a system's located members and the links between
 * them. It brings Leaflet, so pages load it lazily. */
export default function SystemMap(props: { system: System; channels: Channel[] }) {
  const located = props.channels.filter((c) => c.location);
  if (located.length === 0) return null;
  const byID = new Map(located.map((c) => [c.id, [c.location!.latitude, c.location!.longitude] as [number, number]]));
  const center: [number, number] = [
    located.reduce((s, c) => s + c.location!.latitude, 0) / located.length,
    located.reduce((s, c) => s + c.location!.longitude, 0) / located.length,
  ];
  return (
    <MapContainer center={center} zoom={8} style={{ height: 360 }}>
      <TileLayer attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors' url="https://tile.openstreetmap.org/{z}/{x}/{y}.png" />
      <SystemLines systems={[props.system]} where={(id) => byID.get(id) ?? null} />
      {located.map((c) => (
        <CircleMarker key={c.id} center={[c.location!.latitude, c.location!.longitude]} radius={6}>
          <Popup>
            <strong>{c.name}</strong>
            <br />
            {formatMHz(c.rx_hz)} MHz {toneSummary(c)}
          </Popup>
        </CircleMarker>
      ))}
    </MapContainer>
  );
}
