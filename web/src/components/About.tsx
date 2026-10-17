import { Anchor, Text } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";

/** About says which rflib this is, who wrote it, and where its source is,
 * which the AGPL asks a hosted copy to offer everyone who uses it. */
export function About(props: { api?: boolean; ta?: "center" }) {
  const health = useQuery({ queryKey: ["health"], queryFn: () => call(api.GET("/health")), staleTime: Infinity });
  const h = health.data;
  return (
    <Text size="xs" c="dimmed" ta={props.ta}>
      {h ? (
        <>
          rflib {h.version} by {h.author} ·{" "}
          <Anchor href={h.source} target="_blank" rel="noopener" inherit>
            Source
          </Anchor>
        </>
      ) : (
        "rflib"
      )}
      {props.api && (
        <>
          {" "}
          · API: <a href="/api/v1/openapi.yaml">/api/v1</a>
        </>
      )}
    </Text>
  );
}
