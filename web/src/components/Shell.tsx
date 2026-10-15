import { AppShell, Badge, Burger, Divider, Group, NavLink, Text, Title } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconAntenna, IconBroadcast, IconClipboardList, IconFileImport, IconList, IconMap, IconRefresh, IconTopologyStar, IconUsersGroup, IconWorld } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { NavLink as RouterLink, Outlet } from "react-router-dom";
import { api, call, type Change } from "../api/client";
import { enc, useLib, useLibraries } from "../lib/library";
import { TokenPrompt } from "./TokenPrompt";

export function Shell() {
  const lib = useLib();
  const [opened, { toggle, close }] = useDisclosure();
  // Everything but the Shared list item is in the user's own library.
  const { mine, shared } = useLibraries();
  const home = `/l/${enc(mine ?? lib)}`;
  const changes = useQuery({
    queryKey: ["changes", mine],
    queryFn: () => call<{ changes: Change[] }>(api.GET("/libraries/{lib}/shared/changes", { params: { path: { lib: mine! } } })),
    enabled: mine !== null,
  });
  const links = [
    { to: "channels", label: "Channels", icon: IconList },
    { to: "map", label: "Map", icon: IconMap },
    { to: "radios", label: "Radios", icon: IconAntenna },
    { to: "systems", label: "Systems", icon: IconTopologyStar },
    { to: "plans", label: "Plans", icon: IconClipboardList },
    { to: "orgs", label: "Organizations", icon: IconUsersGroup },
    { to: "import", label: "Import", icon: IconFileImport },
  ];
  const pending = changes.data?.changes.length ?? 0;
  return (
    <AppShell header={{ height: 56 }} navbar={{ width: 220, breakpoint: "sm", collapsed: { mobile: !opened } }} padding="md">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group>
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" aria-label="Menu" />
            <Title order={3}>rflib</Title>
          </Group>
          {lib === shared && (
            <Badge size="lg" variant="light" leftSection={<IconWorld size={14} />}>
              Shared list
            </Badge>
          )}
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="xs">
        {links.map((l) => (
          <NavLink key={l.to} component={RouterLink} to={`${home}/${l.to}`} label={l.label} leftSection={<l.icon size={18} />} onClick={close} />
        ))}
        <NavLink
          component={RouterLink}
          to={`${home}/changes`}
          label="Shared changes"
          leftSection={<IconRefresh size={18} />}
          rightSection={pending > 0 ? <Badge size="sm">{pending}</Badge> : null}
          onClick={close}
        />
        {shared && (
          <>
            <Divider my="xs" />
            <NavLink
              component={RouterLink}
              to={`/l/${enc(shared)}/channels`}
              label="Shared list"
              description="Correct shared channels"
              leftSection={<IconWorld size={18} />}
              onClick={close}
            />
            <NavLink
              component={RouterLink}
              to={`/l/${enc(shared)}/lists`}
              label="Built-in lists"
              description="Fill and update the shared list"
              leftSection={<IconBroadcast size={18} />}
              onClick={close}
            />
          </>
        )}
        <Text size="xs" c="dimmed" mt="auto" p="xs">
          API: <a href="/api/v1/openapi.yaml">/api/v1</a>
        </Text>
      </AppShell.Navbar>
      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
      <TokenPrompt />
    </AppShell>
  );
}
