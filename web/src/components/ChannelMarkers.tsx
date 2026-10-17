import "leaflet.markercluster/dist/MarkerCluster.css";
import L from "leaflet";
import "leaflet.markercluster";
import { useEffect } from "react";
import { useMap } from "react-leaflet";
import type { Site } from "../lib/map";
import { formatMHz, toneSummary } from "../lib/radio";

/** A site's marker, with how many channels it stands for. */
type SiteMarker = L.Marker & { channels: number };

/** dot is a marker's look: a dot for one channel, a numbered disc for
 * several; clusters use the disc too, with a ring. */
function dot(count: number, cluster: boolean): L.DivIcon {
  const size = count === 1 ? 14 : count < 10 ? 24 : count < 100 ? 30 : 36;
  const ring = cluster ? "box-shadow: 0 0 0 4px rgba(34, 139, 230, 0.35);" : "";
  const html =
    `<div style="width:${size}px;height:${size}px;border-radius:50%;background:#228be6;border:2px solid #fff;${ring}` +
    `color:#fff;font:600 12px/1 sans-serif;display:flex;align-items:center;justify-content:center;box-sizing:border-box">` +
    `${count === 1 ? "" : count}</div>`;
  return L.divIcon({ html, className: cluster ? "rflib-cluster" : "rflib-site", iconSize: [size, size], iconAnchor: [size / 2, size / 2], popupAnchor: [0, -size / 2] });
}

/** popup lists a site's channels, built as elements so names stay text. */
function popup(site: Site): HTMLElement {
  const box = document.createElement("div");
  box.style.maxHeight = "260px";
  box.style.overflowY = "auto";
  if (site.channels.length > 1) {
    const head = document.createElement("div");
    head.style.fontWeight = "600";
    head.style.marginBottom = "4px";
    head.textContent = `${site.channels.length} channels here`;
    box.append(head);
  }
  for (const c of site.channels) {
    const row = document.createElement("div");
    row.dataset.channel = c.name;
    row.style.marginBottom = "4px";
    const name = document.createElement("strong");
    name.textContent = c.name;
    const reach = c.location?.radius_km != null ? `, reaches ${c.location.radius_km} km` : "";
    row.append(name, document.createElement("br"), [`${formatMHz(c.rx_hz)} MHz`, toneSummary(c)].filter(Boolean).join(" ") + reach);
    box.append(row);
  }
  return box;
}

/** ChannelMarkers puts one marker on the map for each site, numbered when
 * it holds several channels, and groups markers that would overlap into
 * clusters that open as you zoom in. */
export function ChannelMarkers({ sites }: { sites: Site[] }) {
  const map = useMap();
  useEffect(() => {
    const group = L.markerClusterGroup({
      showCoverageOnHover: false,
      maxClusterRadius: 40,
      iconCreateFunction: (cluster) => {
        const n = cluster.getAllChildMarkers().reduce((sum, m) => sum + ((m as SiteMarker).channels ?? 1), 0);
        return dot(n, true);
      },
    });
    for (const s of sites) {
      const m = L.marker([s.lat, s.lon], { icon: dot(s.channels.length, false), title: s.channels.map((c) => c.name).join(", ") }) as SiteMarker;
      m.channels = s.channels.length;
      m.bindPopup(() => popup(s));
      group.addLayer(m);
    }
    map.addLayer(group);
    return () => {
      map.removeLayer(group);
    };
  }, [map, sites]);
  return null;
}
