import { Button, Modal, PasswordInput, Stack, Text } from "@mantine/core";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { setToken, UNAUTHORIZED } from "../api/client";

/** TokenPrompt asks for the API token when the server wants one. */
export function TokenPrompt() {
  const [open, setOpen] = useState(false);
  const [token, setValue] = useState("");
  const client = useQueryClient();
  useEffect(() => {
    const show = () => setOpen(true);
    window.addEventListener(UNAUTHORIZED, show);
    return () => window.removeEventListener(UNAUTHORIZED, show);
  }, []);
  return (
    <Modal opened={open} onClose={() => setOpen(false)} title="This server needs a token">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setToken(token);
          setOpen(false);
          void client.invalidateQueries();
        }}
      >
        <Stack>
          <Text size="sm">Enter the token rflib serve was started with ($RFLIB_API_TOKEN). It's kept in this browser.</Text>
          <PasswordInput label="Token" value={token} onChange={(e) => setValue(e.currentTarget.value)} data-autofocus />
          <Button type="submit">Use token</Button>
        </Stack>
      </form>
    </Modal>
  );
}
