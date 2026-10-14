import { AppShell, Badge, Burger, Group, NavLink, Select, Text, Title } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconAntenna, IconBroadcast, IconFileImport, IconList, IconMap, IconRefresh } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { NavLink as RouterLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { api, call, type Change, type Library } from "../api/client";
import { enc, useLib } from "../lib/library";
import { TokenPrompt } from "./TokenPrompt";

export function Shell() {
  const lib = useLib();
  const navigate = useNavigate();
  const location = useLocation();
  const [opened, { toggle, close }] = useDisclosure();
  const libraries = useQuery({
    queryKey: ["libraries"],
    queryFn: () => call<{ libraries: Library[] }>(api.GET("/libraries")),
  });
  const changes = useQuery({
    queryKey: ["changes", lib],
    queryFn: () => call<{ changes: Change[] }>(api.GET("/libraries/{lib}/shared/changes", { params: { path: { lib } } })),
  });
  const section = location.pathname.split("/")[3] ?? "channels";
  const links = [
    { to: "channels", label: "Channels", icon: IconList },
    { to: "map", label: "Map", icon: IconMap },
    { to: "radios", label: "Radios", icon: IconAntenna },
    { to: "import", label: "Import", icon: IconFileImport },
    { to: "lists", label: "Built-in lists", icon: IconBroadcast },
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
          <Select
            aria-label="Library"
            w={220}
            value={lib}
            allowDeselect={false}
            data={(libraries.data?.libraries ?? []).map((l) => ({ value: l.name, label: l.shared ? `${l.name} (shared)` : l.name }))}
            onChange={(v) => v && navigate(`/l/${enc(v)}/${section}`)}
          />
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="xs">
        {links.map((l) => (
          <NavLink key={l.to} component={RouterLink} to={l.to} label={l.label} leftSection={<l.icon size={18} />} onClick={close} />
        ))}
        <NavLink
          component={RouterLink}
          to="changes"
          label="Shared changes"
          leftSection={<IconRefresh size={18} />}
          rightSection={pending > 0 ? <Badge size="sm">{pending}</Badge> : null}
          onClick={close}
        />
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
