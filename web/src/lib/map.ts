import type { Channel } from "../api/client";

/** Site is a place on the map and the channels there. */
export type Site = { key: string; lat: number; lon: number; channels: Channel[] };

/** sites groups located channels by where they are, to about a meter, so a
 * repeater site's channels share one marker. Each site's channels are in
 * frequency order, and the sites in the order their first channel came. */
export function sites(channels: Channel[]): Site[] {
  const by = new Map<string, Site>();
  for (const c of channels) {
    if (!c.location) continue;
    const { latitude: lat, longitude: lon } = c.location;
    const key = `${lat.toFixed(5)},${lon.toFixed(5)}`;
    const site = by.get(key) ?? { key, lat, lon, channels: [] };
    site.channels.push(c);
    by.set(key, site);
  }
  for (const s of by.values()) s.channels.sort((a, b) => a.rx_hz - b.rx_hz || a.name.localeCompare(b.name));
  return [...by.values()];
}
