import { Polyline, Tooltip } from "react-leaflet";
import type { System } from "../api/client";
import { type Point, systemLines } from "../lib/records";

/** SystemLines draws systems as lines between their members. */
export function SystemLines(props: { systems: System[]; where: (sharedID: string) => Point | null }) {
  return (
    <>
      {props.systems.flatMap((x) =>
        systemLines(x, props.where).map((l, i) => (
          <Polyline key={`${x.id}-${i}`} positions={[l.from, l.to]} pathOptions={{ color: "#7048e8", weight: 2, dashArray: l.onDemand ? "6 6" : undefined }}>
            <Tooltip sticky>{x.name}</Tooltip>
          </Polyline>
        )),
      )}
    </>
  );
}
