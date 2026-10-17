import { Alert, Anchor, Badge, Button, Center, Menu, Modal, Paper, PasswordInput, Stack, Text, TextInput, Title, UnstyledButton } from "@mantine/core";
import { IconChevronDown, IconUserCircle } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, call, message, type User } from "../api/client";
import { useAuth } from "../lib/auth";
import { failed, ok } from "../lib/notify";

/** SignIn is the page a server with accounts shows until someone signs
 * in, with registration when the server's sign-up allows it. */
export function SignIn() {
  const auth = useAuth().data;
  const client = useQueryClient();
  const navigate = useNavigate();
  const signup = auth?.signup ?? "closed";
  const [registering, setRegistering] = useState(false);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [display, setDisplay] = useState("");
  const [email, setEmail] = useState("");
  const [invite, setInvite] = useState("");
  const [error, setError] = useState("");
  // Everything cached was the signed-out server's view.
  const done = () => {
    navigate("/", { replace: true });
    void client.resetQueries();
  };
  const signIn = useMutation({
    mutationFn: () => call(api.POST("/auth/signin", { body: { name, password } })),
    onSuccess: done,
    onError: (e) => setError(message(e)),
  });
  const register = useMutation({
    mutationFn: () => call(api.POST("/auth/register", { body: { name, password, display_name: display, email, invite } })),
    onSuccess: done,
    onError: (e) => setError(message(e)),
  });
  const mismatch = registering && again !== "" && again !== password;
  return (
    <Center mih="100vh" p="md">
      <Paper withBorder p="xl" w="100%" maw={420}>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setError("");
            if (registering) register.mutate();
            else signIn.mutate();
          }}
        >
          <Stack>
            <Title order={2}>rflib</Title>
            <Text c="dimmed">{registering ? "Make an account, with a library of your own." : "Sign in to your library."}</Text>
            {error && <Alert color="red">{error}</Alert>}
            <TextInput
              label="Callsign or username"
              description={registering ? "What you sign in with: your callsign, or a username if you have none" : undefined}
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
              autoComplete="username"
              required
              data-autofocus
            />
            <PasswordInput
              label="Password"
              description={registering ? "At least 10 characters" : undefined}
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
              autoComplete={registering ? "new-password" : "current-password"}
              required
            />
            {registering && (
              <>
                <PasswordInput
                  label="Password again"
                  value={again}
                  onChange={(e) => setAgain(e.currentTarget.value)}
                  autoComplete="new-password"
                  error={mismatch ? "The passwords don't match" : undefined}
                  required
                />
                <TextInput label="Display name" description="Optional" value={display} onChange={(e) => setDisplay(e.currentTarget.value)} />
                <TextInput label="Email" description="Optional; rflib sends no email" type="email" value={email} onChange={(e) => setEmail(e.currentTarget.value)} />
                {signup === "invite" && <TextInput label="Invitation code" description="From the server's admin" value={invite} onChange={(e) => setInvite(e.currentTarget.value)} required />}
              </>
            )}
            <Button type="submit" loading={signIn.isPending || register.isPending} disabled={mismatch}>
              {registering ? "Make the account" : "Sign in"}
            </Button>
            {signup !== "closed" && (
              <Text size="sm" ta="center">
                {registering ? "Have an account? " : "New here? "}
                <Anchor
                  component="button"
                  type="button"
                  onClick={() => {
                    setRegistering(!registering);
                    setError("");
                  }}
                >
                  {registering ? "Sign in" : "Make an account"}
                </Anchor>
              </Text>
            )}
            {signup === "closed" && !registering && (
              <Text size="sm" c="dimmed" ta="center">
                Accounts on this server are made by its admin.
              </Text>
            )}
          </Stack>
        </form>
      </Paper>
    </Center>
  );
}

/** AccountMenu is the signed-in user's menu in the header: their account,
 * and signing out. */
export function AccountMenu(props: { user: User }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const signOut = useMutation({
    mutationFn: () => call(api.POST("/auth/signout")),
    onSuccess: () => {
      navigate("/", { replace: true });
      void client.resetQueries();
    },
    onError: failed,
  });
  const u = props.user;
  return (
    <>
      <Menu position="bottom-end">
        <Menu.Target>
          <UnstyledButton aria-label="Account">
            <Badge size="lg" variant="default" leftSection={<IconUserCircle size={16} />} rightSection={<IconChevronDown size={12} />} tt="none">
              {u.display_name || u.name}
            </Badge>
          </UnstyledButton>
        </Menu.Target>
        <Menu.Dropdown>
          <Menu.Label>
            {u.name} · {u.role}
          </Menu.Label>
          <Menu.Item onClick={() => setEditing(true)}>Account</Menu.Item>
          <Menu.Item onClick={() => signOut.mutate()}>Sign out</Menu.Item>
        </Menu.Dropdown>
      </Menu>
      <AccountModal user={u} opened={editing} onClose={() => setEditing(false)} />
    </>
  );
}

/** AccountModal changes the user's display name, email, and password. */
function AccountModal(props: { user: User; opened: boolean; onClose: () => void }) {
  const client = useQueryClient();
  const [display, setDisplay] = useState(props.user.display_name);
  const [email, setEmail] = useState(props.user.email);
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const profile = useMutation({
    mutationFn: () => call(api.PATCH("/auth/me", { body: { display_name: display, email } })),
    onSuccess: () => {
      ok("Saved");
      void client.invalidateQueries({ queryKey: ["auth"] });
    },
    onError: failed,
  });
  const change = useMutation({
    mutationFn: () => call(api.POST("/auth/password", { body: { current, password } })),
    onSuccess: () => {
      ok("Changed your password; your other sessions are signed out");
      setCurrent("");
      setPassword("");
      setAgain("");
    },
    onError: failed,
  });
  const mismatch = again !== "" && again !== password;
  return (
    <Modal opened={props.opened} onClose={props.onClose} title={`Your account, ${props.user.name}`}>
      <Stack>
        <Text size="sm" c="dimmed">
          {props.user.callsign ? "Your name is a callsign (unverified)." : "Your name is a username."} Your library is {props.user.library}.
          {props.user.role === "admin" ? " As an admin, you also maintain the shared list." : ""}
        </Text>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            profile.mutate();
          }}
        >
          <Stack gap="xs">
            <TextInput label="Display name" value={display} onChange={(e) => setDisplay(e.currentTarget.value)} />
            <TextInput label="Email" type="email" value={email} onChange={(e) => setEmail(e.currentTarget.value)} />
            <Button type="submit" variant="light" loading={profile.isPending}>
              Save
            </Button>
          </Stack>
        </form>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            change.mutate();
          }}
        >
          <Stack gap="xs">
            <Title order={5}>Change your password</Title>
            <PasswordInput label="Current password" value={current} onChange={(e) => setCurrent(e.currentTarget.value)} autoComplete="current-password" required />
            <PasswordInput label="New password" description="At least 10 characters" value={password} onChange={(e) => setPassword(e.currentTarget.value)} autoComplete="new-password" required />
            <PasswordInput
              label="New password again"
              value={again}
              onChange={(e) => setAgain(e.currentTarget.value)}
              autoComplete="new-password"
              error={mismatch ? "The passwords don't match" : undefined}
              required
            />
            <Button type="submit" variant="light" loading={change.isPending} disabled={mismatch}>
              Change password
            </Button>
          </Stack>
        </form>
      </Stack>
    </Modal>
  );
}
