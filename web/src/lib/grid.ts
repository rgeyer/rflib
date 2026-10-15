/** A Maidenhead grid square's center, and the distance to its corners. */
export type Grid = { latitude: number; longitude: number; radius_km: number };

/** parseGrid reads a 4, 6, or 8 character grid square, such as CM94sq, in
 * any letter case, as internal/domain.ParseGrid does; null if it isn't one. */
export function parseGrid(s: string): Grid | null {
  const g = s.trim().toUpperCase();
  if (![4, 6, 8].includes(g.length)) return null;
  let lon = -180,
    lat = -90,
    lonStep = 360,
    latStep = 180;
  for (let i = 0; i < g.length; i += 2) {
    const [base, count] = i === 0 ? ["A", 18] : i === 4 ? ["A", 24] : ["0", 10];
    const x = g.charCodeAt(i) - base.charCodeAt(0),
      y = g.charCodeAt(i + 1) - base.charCodeAt(0);
    if (x < 0 || y < 0 || x >= count || y >= count) return null;
    lonStep /= count;
    latStep /= count;
    lon += x * lonStep;
    lat += y * latStep;
  }
  const round = (v: number) => Math.round(v * 1e6) / 1e6;
  const latitude = round(lat + latStep / 2),
    longitude = round(lon + lonStep / 2);
  return { latitude, longitude, radius_km: Math.ceil(distanceKM(latitude, longitude, lat, lon) * 10) / 10 };
}

function distanceKM(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const rad = Math.PI / 180;
  const a = Math.sin(((lat2 - lat1) * rad) / 2) ** 2 + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(((lon2 - lon1) * rad) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(a));
}
